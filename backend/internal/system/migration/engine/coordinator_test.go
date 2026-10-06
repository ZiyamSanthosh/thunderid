// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

type coordinatorFailingOperation struct {
	testOperation
}

func (o coordinatorFailingOperation) Execute(context.Context, OperationExecution) error {
	return errors.New("operation failed")
}

type coordinatorReadinessOperation struct {
	testOperation
	checks int
}

type testMigrationExecutionLocker struct{}

func (testMigrationExecutionLocker) WithLock(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	return fn(ctx)
}

type countingMigrationExecutionLocker struct {
	calls int
}

func (l *countingMigrationExecutionLocker) WithLock(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	l.calls++
	return fn(ctx)
}

type unavailableMigrationExecutionLocker struct{}

func (unavailableMigrationExecutionLocker) WithLock(
	context.Context,
	func(context.Context) error,
) error {
	return ErrMigrationExecutionLocked
}

func TestMigrationCoordinatorRequiresExecutionLockBeforeDatabaseInspection(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	coordinator, err := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: &testTransactioner{}, Locker: unavailableMigrationExecutionLocker{},
	}})
	require.NoError(t, err)

	applyResult, applyErr := coordinator.Apply(context.Background(), MigrationSelection{})
	resumeResult, resumeErr := coordinator.Resume(context.Background())

	require.ErrorIs(t, applyErr, ErrMigrationExecutionLocked)
	assert.Empty(t, applyResult.RunID)
	assert.Equal(t, OperationPhaseExpand, applyResult.Phase)
	require.ErrorIs(t, resumeErr, ErrMigrationExecutionLocked)
	assert.Empty(t, resumeResult.RunID)
}

func TestMigrationCoordinatorRejectsInvalidMigrationSelection(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	result, err := coordinator.Apply(context.Background(), MigrationSelection{
		DeploymentIDs: []string{"deployment-a", "deployment-a"},
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "repeats deployment ID")
	assert.Empty(t, result.RunID)
}

func TestMigrationCoordinatorSafeWorkflowRunsExpandAndMigrateUnderOneLock(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, true, updatedAt)
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, true, updatedAt)
	locker := &countingMigrationExecutionLocker{}
	coordinator, err := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: &testTransactioner{}, Locker: locker,
	}})
	require.NoError(t, err)

	result, err := coordinator.ApplySafe(context.Background(), MigrationSelection{})

	require.NoError(t, err)
	require.Len(t, result.Phases, 2)
	assert.Equal(t, OperationPhaseExpand, result.Phases[0].Phase)
	assert.True(t, result.Phases[0].AlreadyAtTarget)
	assert.Equal(t, OperationPhaseMigrate, result.Phases[1].Phase)
	assert.True(t, result.Phases[1].AlreadyAtTarget)
	assert.Equal(t, 1, locker.calls)
}

func TestMigrationCoordinatorSafeWorkflowRejectsContractOnlyStore(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Contract:      []Operation{validPlanOperation(StoreIDConfigDB)},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	result, err := coordinator.ApplySafe(context.Background(), MigrationSelection{})

	require.ErrorIs(t, err, ErrSafeWorkflowUnsupported)
	assert.Empty(t, result.Phases)
}

func TestMigrationCoordinatorRequiresDedicatedContractExecution(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Contract:      []Operation{validPlanOperation(StoreIDConfigDB)},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	locker := &countingMigrationExecutionLocker{}
	coordinator, err := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: &testTransactioner{}, Locker: locker,
	}})
	require.NoError(t, err)

	phaseResult, phaseErr := coordinator.ApplyPhase(
		context.Background(), OperationPhaseContract, MigrationSelection{})

	require.ErrorIs(t, phaseErr, ErrContractRequiresDedicatedExecution)
	assert.Equal(t, OperationPhaseContract, phaseResult.Phase)
	assert.Equal(t, 0, locker.calls)
}

func TestMigrationCoordinatorAppliesContractThroughDedicatedPath(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Contract:      []Operation{validPlanOperation(StoreIDConfigDB)},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, true, time.Now().UTC())
	locker := &countingMigrationExecutionLocker{}
	coordinator, err := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: &testTransactioner{}, Locker: locker,
	}})
	require.NoError(t, err)

	result, err := coordinator.ApplyContract(context.Background(), MigrationSelection{})

	require.NoError(t, err)
	assert.Equal(t, OperationPhaseContract, result.Phase)
	assert.True(t, result.AlreadyAtTarget)
	assert.Equal(t, 1, locker.calls)
}

func (o *coordinatorReadinessOperation) CheckReadiness(
	context.Context,
	ReadAccess,
	PlanContext,
) (ReadinessResult, error) {
	o.checks++
	if o.checks == 1 {
		return ReadinessResult{State: ReadinessReady, Summary: "Initial readiness passed"}, nil
	}
	return ReadinessResult{
		State: ReadinessBlocked, Summary: "Reserved-state readiness failed",
		Findings: []Finding{{Code: "source_changed", Message: "The source changed after preflight"}},
	}, nil
}

func TestMigrationCoordinatorAppliesBasicMigration(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, false, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{coordinatorStateRow(
			StoreIDConfigDB, "1.0.0", StatusClean, "", "", updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryReserveMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID,
		mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		string(MigrationStageComplete), string(StatusClean)).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		mock.Anything, testStateID, testRunID, release.Manifest.ReleaseVersion,
		string(release.Plan.Expand[0].Definition().ID), 0, string(StatusInProgress), nil, mock.Anything).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, mock.Anything, mock.Anything, testStateID, testRunID).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()

	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)
	coordinator.newID = sequenceIDs(testRunID)

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.NoError(t, err)
	assert.Equal(t, testRunID, result.RunID)
	assert.False(t, result.AlreadyAtTarget)
	assert.True(t, result.Preflight.Ready())
	require.Len(t, result.Reservations, 1)
	assert.True(t, result.Reservations[0].Reserved)
	require.Len(t, result.Operations, 1)
	assert.Equal(t, StatusCompleted, result.Operations[0].Progress.Status)
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, 3, transactioner.calls)
}

func TestMigrationCoordinatorAppliesSelectedMigratePhase(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	for _, requirement := range release.Manifest.Stores {
		expectReadySchema(dbClient)
		row := coordinatorStateRow(
			requirement.ID, requirement.RequiredLevel, StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = phaseStateRow(
				MigrationStageExpanded, StatusCompleted, release.Manifest.ReleaseVersion, "", updatedAt)
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageExpanded, StatusCompleted, release.Manifest.ReleaseVersion, "", updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageMigrating), string(StatusInProgress),
		testRunID, nil, mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageExpanded), string(StatusCompleted),
		nil, nil, updatedAt).
		Return(int64(1), nil).Once()
	expectCoordinatorActiveState(
		dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, MigrationStageMigrating, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		mock.Anything, testStateID, testRunID, release.Manifest.ReleaseVersion,
		string(release.Plan.Migrate[0].Definition().ID), 1, string(StatusInProgress), nil, mock.Anything).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, mock.Anything, mock.Anything, testStateID, testRunID).
		Return(int64(1), nil).Once()
	expectCoordinatorActiveState(
		dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, MigrationStageMigrating, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Migrate[0].Definition().ID, 1, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageContractReady), string(StatusCompleted),
		nil, nil, mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageMigrating), string(StatusInProgress),
		testRunID, nil, updatedAt).
		Return(int64(1), nil).Once()

	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)
	coordinator.newID = sequenceIDs(testRunID)

	result, err := coordinator.ApplyPhase(context.Background(), OperationPhaseMigrate, MigrationSelection{})

	require.NoError(t, err)
	assert.Equal(t, OperationPhaseMigrate, result.Phase)
	assert.Equal(t, OperationPhaseMigrate, result.Preflight.Phase)
	assert.Equal(t, testRunID, result.RunID)
	require.Len(t, result.Operations, 1)
	assert.Equal(t, string(release.Plan.Migrate[0].Definition().ID), result.Operations[0].Progress.OperationID)
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, MigrationStageContractReady, result.Finalizations[0].State.Stage)
	assert.Equal(t, 3, transactioner.calls)
}

func TestMigrationCoordinatorReturnsWithoutRunWhenAlreadyAtTarget(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, true, time.Now().UTC())
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})
	coordinator.newID = func() (string, error) {
		t.Fatal("run ID must not be generated when every store is at target")
		return "", nil
	}

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.NoError(t, err)
	assert.True(t, result.AlreadyAtTarget)
	assert.Empty(t, result.RunID)
	assert.Empty(t, result.Reservations)
}

func TestMigrationCoordinatorDoesNotReserveBlockedPreflight(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	for _, requirement := range release.Manifest.Stores {
		expectReadySchema(dbClient)
		row := coordinatorStateRow(requirement.ID, requirement.AcceptedSourceLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = coordinatorStateRow(requirement.ID, requirement.AcceptedSourceLevel,
				StatusFailed, requirement.RequiredLevel, testRunID, updatedAt)
			row["failure_summary"] = "previous migration failed"
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMigrationPreflightBlocked)
	assert.False(t, result.Preflight.Ready())
	assert.Empty(t, result.RunID)
	assert.Empty(t, result.Reservations)
}

func TestMigrationCoordinatorCancelsReservationsWhenRevalidationBlocks(t *testing.T) {
	release := coordinatorTestRelease()
	baseOperation := validPlanOperation(StoreIDConfigDB)
	release.Plan.Expand = []Operation{&coordinatorReadinessOperation{testOperation: baseOperation}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, false, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{coordinatorStateRow(
			StoreIDConfigDB, "1.0.0", StatusClean, "", "", updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryReserveMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID,
		mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		string(MigrationStageComplete), string(StatusClean)).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryCancelMigrationState,
		string(MigrationStageComplete), string(StatusClean), mock.Anything, testStateID,
		string(StoreIDConfigDB), 1, "1.0.0", release.Manifest.ReleaseVersion,
		string(MigrationStageExpanding), string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)
	coordinator.newID = sequenceIDs(testRunID)

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMigrationPreflightBlocked)
	assert.Equal(t, testRunID, result.RunID)
	require.Len(t, result.Reservations, 1)
	assert.False(t, result.Preflight.Ready())
	assert.Empty(t, result.Operations)
	assert.Equal(t, 2, transactioner.calls)
}

func TestMigrationCoordinatorMarksStoreFailedAfterExecutionError(t *testing.T) {
	release := coordinatorTestRelease()
	baseOperation := validPlanOperation(StoreIDConfigDB)
	release.Plan.Expand = []Operation{coordinatorFailingOperation{testOperation: baseOperation}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, false, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{coordinatorStateRow(
			StoreIDConfigDB, "1.0.0", StatusClean, "", "", updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryReserveMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID,
		mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		string(MigrationStageComplete), string(StatusClean)).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		mock.Anything, testStateID, testRunID, release.Manifest.ReleaseVersion,
		string(baseOperation.Definition().ID), 0, string(StatusInProgress), nil, mock.Anything).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryFailMigrationState,
		string(StatusFailed), mock.MatchedBy(func(summary string) bool {
			return strings.Contains(summary, "operation failed")
		}), mock.Anything, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)
	coordinator.newID = sequenceIDs(testRunID)

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "operation failed")
	assert.Equal(t, testRunID, result.RunID)
	assert.Empty(t, result.Operations)
	assert.Empty(t, result.Finalizations)
	assert.Equal(t, 3, transactioner.calls)
}

func TestMigrationCoordinatorResumesFailedRunWithoutRepeatingCompletedOperation(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Date(2026, time.October, 2, 8, 0, 0, 0, time.UTC)
	failedRow := coordinatorStateRow(StoreIDConfigDB, "1.0.0", StatusFailed,
		release.Manifest.ReleaseVersion, testRunID, updatedAt)
	failedRow["failure_summary"] = "finalization failed"
	for _, requirement := range release.Manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = failedRow
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{failedRow}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryResumeMigrationState,
		string(StatusInProgress), mock.Anything, testStateID, string(StoreIDConfigDB), 1,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusFailed), testRunID,
		"finalization failed", updatedAt).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()

	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)

	result, err := coordinator.Resume(context.Background())

	require.NoError(t, err)
	assert.True(t, result.Resumed)
	assert.Equal(t, testRunID, result.RunID)
	require.Len(t, result.Resumptions, 1)
	assert.True(t, result.Resumptions[0].Resumed)
	assert.Empty(t, result.Operations)
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, 2, transactioner.calls)
}

func TestMigrationCoordinatorResumesPendingOperation(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Date(2026, time.October, 2, 8, 30, 0, 0, time.UTC)
	failedRow := coordinatorStateRow(StoreIDConfigDB, "1.0.0", StatusFailed,
		release.Manifest.ReleaseVersion, testRunID, updatedAt)
	failedRow["failure_summary"] = "operation failed"
	for _, requirement := range release.Manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = failedRow
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{failedRow}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryResumeMigrationState,
		string(StatusInProgress), mock.Anything, testStateID, string(StoreIDConfigDB), 1,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusFailed), testRunID,
		"operation failed", updatedAt).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		mock.Anything, testStateID, testRunID, release.Manifest.ReleaseVersion,
		string(release.Plan.Expand[0].Definition().ID), 0, string(StatusInProgress), nil, mock.Anything).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, mock.Anything, mock.Anything, testStateID, testRunID).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()

	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)

	result, err := coordinator.Resume(context.Background())

	require.NoError(t, err)
	assert.True(t, result.Resumed)
	require.Len(t, result.Operations, 1)
	assert.Equal(t, StatusCompleted, result.Operations[0].Progress.Status)
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, 3, transactioner.calls)
}

func TestMigrationCoordinatorResumesAbandonedInProgressRun(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Date(2026, time.October, 2, 8, 45, 0, 0, time.UTC)
	inProgressRow := coordinatorStateRow(StoreIDConfigDB, "1.0.0", StatusInProgress,
		release.Manifest.ReleaseVersion, testRunID, updatedAt)
	for _, requirement := range release.Manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = inProgressRow
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{}, nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		mock.Anything, testStateID, testRunID, release.Manifest.ReleaseVersion,
		string(release.Plan.Expand[0].Definition().ID), 0, string(StatusInProgress), nil, mock.Anything).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, mock.Anything, mock.Anything, testStateID, testRunID).
		Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()

	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)

	result, err := coordinator.Resume(context.Background())

	require.NoError(t, err)
	assert.True(t, result.Resumed)
	assert.Equal(t, testRunID, result.RunID)
	assert.Empty(t, result.Resumptions)
	require.Len(t, result.Operations, 1)
	assert.Equal(t, StatusCompleted, result.Operations[0].Progress.Status)
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, 2, transactioner.calls)
}

func TestMigrationCoordinatorRecognizesResumableOperationCheckpoint(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	operation := validPlanOperation(StoreIDConfigDB)
	operation.definition.InterruptionMode = InterruptionModeResumable
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{resumableTestOperation{testOperation: operation}},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Date(2026, time.October, 6, 13, 0, 0, 0, time.UTC)
	for _, requirement := range manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = coordinatorStateRow(requirement.ID, requirement.AcceptedSourceLevel,
				StatusFailed, requirement.RequiredLevel, testRunID, updatedAt)
			row["failure_summary"] = "migration interrupted"
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	progressRow := completedProgressRow(operation.Definition().ID, 0, manifest.ReleaseVersion, updatedAt)
	progressRow["status"] = string(StatusInProgress)
	progressRow["checkpoint_data"] = `{"lastID":"500"}`
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{progressRow}, nil).Once()
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	resume, err := coordinator.inspectResumableRun(context.Background())

	require.NoError(t, err)
	progress, exists := resume.resumableProgress[operation.Definition().ID]
	require.True(t, exists)
	assert.Equal(t, StatusInProgress, progress.Status)
	assert.JSONEq(t, `{"lastID":"500"}`, string(progress.CheckpointData))
	assert.Empty(t, resume.completedOperations)
}

func TestMigrationCoordinatorResumesOperationFromCheckpoint(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	baseOperation := validPlanOperation(StoreIDConfigDB)
	baseOperation.definition.InterruptionMode = InterruptionModeResumable
	operation := &scriptedResumableOperation{
		definition: baseOperation.definition,
		batches: []ResumableBatchResult{{
			CheckpointData: json.RawMessage(`{"lastID":"750"}`), Completed: true,
		}},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{operation},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Date(2026, time.October, 6, 14, 0, 0, 0, time.UTC)
	failedRow := coordinatorStateRow(StoreIDConfigDB, "1.0.0", StatusFailed,
		manifest.ReleaseVersion, testRunID, updatedAt)
	failedRow["failure_summary"] = "migration interrupted"
	for _, requirement := range manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		if requirement.ID == StoreIDConfigDB {
			row = failedRow
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	inProgress := completedProgressRow(operation.Definition().ID, 0, manifest.ReleaseVersion, updatedAt)
	inProgress["status"] = string(StatusInProgress)
	inProgress["checkpoint_data"] = `{"lastID":"500"}`
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{inProgress}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{failedRow}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryResumeMigrationState,
		string(StatusInProgress), mock.Anything, testStateID, string(StoreIDConfigDB), 1,
		"1.0.0", manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusFailed), testRunID,
		"migration interrupted", updatedAt).Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), `{"lastID":"750"}`, updatedAt,
		testProgressID, testStateID, testRunID).Return(int64(1), nil).Once()
	expectReservedState(dbClient, StoreIDConfigDB, manifest.ReleaseVersion, testRunID, updatedAt)
	completed := completedProgressRow(operation.Definition().ID, 0, manifest.ReleaseVersion, updatedAt)
	completed["checkpoint_data"] = `{"lastID":"750"}`
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completed}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, transactioner)
	coordinator.namespaces[0].resumableExecutor.now = func() time.Time { return updatedAt }

	result, err := coordinator.Resume(context.Background())

	require.NoError(t, err)
	require.Len(t, result.Operations, 1)
	assert.Equal(t, 1, result.Operations[0].CommittedBatches)
	assert.JSONEq(t, `{"lastID":"750"}`, string(result.Operations[0].Progress.CheckpointData))
	require.Len(t, operation.checkpoints, 1)
	assert.JSONEq(t, `{"lastID":"500"}`, string(operation.checkpoints[0]))
	require.Len(t, result.Finalizations, 1)
	assert.True(t, result.Finalizations[0].Finalized)
	assert.Equal(t, 3, transactioner.calls)
}

func TestMigrationCoordinatorResumeAcceptsStoreThatCompletedTheActivePhase(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	for index := range manifest.Stores {
		if manifest.Stores[index].ID == StoreIDEntityDB {
			manifest.Stores[index].Changed = true
			manifest.Stores[index].RequiredLevel = manifest.ReleaseVersion
		}
	}
	expandConfig := validPlanOperation(StoreIDConfigDB)
	expandConfig.definition.ID = "1.1.0/configdb/expand"
	expandEntity := validPlanOperation(StoreIDEntityDB)
	expandEntity.definition.ID = "1.1.0/entitydb/expand"
	migrateConfig := validPlanOperation(StoreIDConfigDB)
	migrateConfig.definition.ID = "1.1.0/configdb/migrate"
	migrateEntity := validPlanOperation(StoreIDEntityDB)
	migrateEntity.definition.ID = "1.1.0/entitydb/migrate"
	contractConfig := validPlanOperation(StoreIDConfigDB)
	contractConfig.definition.ID = "1.1.0/configdb/contract"
	contractEntity := validPlanOperation(StoreIDEntityDB)
	contractEntity.definition.ID = "1.1.0/entitydb/contract"
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{expandConfig, expandEntity},
		Migrate:       []Operation{migrateConfig, migrateEntity},
		Contract:      []Operation{contractConfig, contractEntity},
	}}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	for _, requirement := range manifest.Stores {
		row := coordinatorStateRow(
			requirement.ID, requirement.RequiredLevel, StatusClean, "", "", updatedAt)
		switch requirement.ID {
		case StoreIDConfigDB:
			row = phaseStateRow(
				MigrationStageContractReady, StatusCompleted, manifest.ReleaseVersion, "", updatedAt)
		case StoreIDEntityDB:
			row = phaseStateRow(
				MigrationStageMigrating, StatusFailed, manifest.ReleaseVersion, testRunID, updatedAt)
			row["store_id"] = string(StoreIDEntityDB)
			row["failure_summary"] = "migration interrupted"
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{}, nil).Once()
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	resume, err := coordinator.inspectResumableRun(context.Background())

	require.NoError(t, err)
	assert.Equal(t, OperationPhaseMigrate, resume.phase)
	assert.Equal(t, testRunID, resume.runID)
	assert.Equal(t, map[StoreID]struct{}{StoreIDEntityDB: {}}, resume.activeStores)
	configAssessment := resume.storeAssessments[0]
	assert.Equal(t, StoreIDConfigDB, configAssessment.StoreID)
	assert.Equal(t, StoreAssessmentAtTarget, configAssessment.State)
}

func TestMigrationCoordinatorRefusesResumeWithoutResumableRun(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	for _, requirement := range release.Manifest.Stores {
		row := coordinatorStateRow(requirement.ID, requirement.RequiredLevel,
			StatusClean, "", "", updatedAt)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})

	result, err := coordinator.Resume(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoMigrationToResume)
	assert.True(t, result.Resumed)
	assert.Empty(t, result.RunID)
}

func TestNewMigrationCoordinatorValidatesNamespaceCoverageAndOperationExecutors(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	transactioner := &testTransactioner{}

	_, missingNamespaceErr := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: []StoreID{StoreIDConfigDB}, DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: transactioner, Locker: testMigrationExecutionLocker{},
	}})
	_, missingLockerErr := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: transactioner,
	}})

	resumableRelease := release
	resumableOperation := validPlanOperation(StoreIDConfigDB)
	resumableOperation.definition.InterruptionMode = InterruptionModeResumable
	resumableRelease.Plan.Expand = []Operation{resumableTestOperation{testOperation: resumableOperation}}
	resumableCoordinator, resumableErr := NewMigrationCoordinator(resumableRelease, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: transactioner, Locker: testMigrationExecutionLocker{},
	}})
	phaseRelease := release
	phaseOperation := validPlanOperation(StoreIDConfigDB)
	phaseRelease.Plan.Expand = nil
	phaseRelease.Plan.Migrate = []Operation{phaseOperation}
	phaseCoordinator, phaseErr := NewMigrationCoordinator(phaseRelease, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: transactioner, Locker: testMigrationExecutionLocker{},
	}})

	require.Error(t, missingNamespaceErr)
	assert.ErrorContains(t, missingNamespaceErr, "has no database namespace")
	require.Error(t, missingLockerErr)
	assert.ErrorContains(t, missingLockerErr, "execution locker is required")
	require.NoError(t, resumableErr)
	assert.NotNil(t, resumableCoordinator)
	require.NoError(t, phaseErr)
	assert.NotNil(t, phaseCoordinator)
}

func TestBoundedFailureSummaryPreservesRuneBoundary(t *testing.T) {
	summary := strings.Repeat("a", maxFailureSummaryLength-1) + "éé"

	bounded := boundedFailureSummary(summary)

	assert.Equal(t, maxFailureSummaryLength, len([]rune(bounded)))
	assert.True(t, strings.HasSuffix(bounded, "é"))
}

func coordinatorTestRelease() ReleaseDefinition {
	manifest := changedStoreManifest(StoreIDConfigDB)
	return ReleaseDefinition{
		Manifest: manifest,
		Plan: MigrationPlan{
			TargetVersion: manifest.ReleaseVersion,
			Expand:        []Operation{validPlanOperation(StoreIDConfigDB)},
		},
	}
}

func newTestMigrationCoordinator(
	t *testing.T,
	release ReleaseDefinition,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *MigrationCoordinator {
	t.Helper()
	coordinator, err := NewMigrationCoordinator(release, []MigrationNamespace{{
		StoreIDs: append([]StoreID(nil), supportedStoreIDs...), DatabaseType: DatabaseTypePostgres,
		Client: dbClient, Transactioner: transactioner, Locker: testMigrationExecutionLocker{},
	}})
	require.NoError(t, err)
	return coordinator
}

func expectCoordinatorPreflightStates(
	dbClient *providermock.DBClientInterfaceMock,
	manifest ReleaseManifest,
	changedStore StoreID,
	changedStoreAtTarget bool,
	updatedAt time.Time,
) {
	for _, requirement := range manifest.Stores {
		expectReadySchema(dbClient)
		completedLevel := requirement.RequiredLevel
		if requirement.ID == changedStore && !changedStoreAtTarget {
			completedLevel = requirement.AcceptedSourceLevel
		}
		row := coordinatorStateRow(requirement.ID, completedLevel, StatusClean, "", "", updatedAt)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}
}

func coordinatorStateRow(
	storeID StoreID,
	completedLevel string,
	status Status,
	targetLevel string,
	runID string,
	updatedAt time.Time,
) map[string]interface{} {
	row := validStateRow(updatedAt)
	row["store_id"] = string(storeID)
	row["completed_level"] = completedLevel
	row["status"] = string(status)
	if status == StatusClean {
		row["stage"] = string(MigrationStageComplete)
	} else {
		row["stage"] = string(MigrationStageExpanding)
	}
	if targetLevel != "" {
		row["target_level"] = targetLevel
	}
	if runID != "" {
		row["active_run_id"] = runID
	}
	return row
}

func expectCoordinatorActiveState(
	dbClient *providermock.DBClientInterfaceMock,
	storeID StoreID,
	targetLevel string,
	stage MigrationStage,
	runID string,
	updatedAt time.Time,
) {
	row := validStateRow(updatedAt)
	row["store_id"] = string(storeID)
	row["target_level"] = targetLevel
	row["stage"] = string(stage)
	row["status"] = string(StatusInProgress)
	row["active_run_id"] = runID
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(storeID)).
		Return([]map[string]interface{}{row}, nil).Once()
}

func TestMigrationCoordinatorReportsRunIDGenerationFailure(t *testing.T) {
	release := coordinatorTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	expectCoordinatorPreflightStates(dbClient, release.Manifest, StoreIDConfigDB, false, time.Now().UTC())
	coordinator := newTestMigrationCoordinator(t, release, dbClient, &testTransactioner{})
	coordinator.newID = func() (string, error) { return "", errors.New("random source failed") }

	result, err := coordinator.Apply(context.Background(), MigrationSelection{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "random source failed")
	assert.Empty(t, result.RunID)
}
