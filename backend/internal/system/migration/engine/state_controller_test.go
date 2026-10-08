// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestMigrationStateControllerReservesAcceptedSource(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	reservedAt := time.Date(2026, time.September, 30, 14, 0, 0, 0, time.UTC)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{validStateRow(reservedAt.Add(-time.Hour))}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryReserveMigrationState,
		manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID, reservedAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageComplete), string(StatusClean)).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(manifest), newTestMetadataStore(t, dbClient), transactioner)
	require.NoError(t, err)
	controller.now = func() time.Time { return reservedAt }

	results, err := controller.Reserve(context.Background(), []StoreID{StoreIDConfigDB}, testRunID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Reserved)
	assert.False(t, results[0].AlreadyAtTarget)
	assert.Equal(t, StatusInProgress, results[0].State.Status)
	assert.Equal(t, manifest.ReleaseVersion, results[0].State.TargetLevel)
	assert.Equal(t, testRunID, results[0].State.ActiveRunID)
	assert.Equal(t, 1, transactioner.calls)
}

func TestMigrationStateControllerPreservesStoreAlreadyAtTarget(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	row := validStateRow(time.Now().UTC())
	row["completed_level"] = manifest.ReleaseVersion
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{row}, nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(manifest), newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)

	results, err := controller.Reserve(context.Background(), []StoreID{StoreIDConfigDB}, testRunID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Reserved)
	assert.True(t, results[0].AlreadyAtTarget)
}

func TestMigrationStateControllerReportsReservationConflict(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{validStateRow(updatedAt)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryReserveMigrationState,
		manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress), testRunID, mock.Anything,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageComplete), string(StatusClean)).
		Return(int64(0), nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(manifest), newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)

	results, err := controller.Reserve(context.Background(), []StoreID{StoreIDConfigDB}, testRunID)

	require.Error(t, err)
	assert.Nil(t, results)
	assert.True(t, errors.Is(err, ErrReservationConflict))
}

func TestMigrationStateControllerRejectsInvalidReservationRequests(t *testing.T) {
	manifest := changedStoreManifest(StoreIDConfigDB)
	controller, err := NewMigrationStateController(stateControllerTestRelease(manifest),
		newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t)), &testTransactioner{})
	require.NoError(t, err)
	tests := []struct {
		name       string
		storeIDs   []StoreID
		runID      string
		errorMatch string
	}{
		{name: "invalid run ID", storeIDs: []StoreID{StoreIDConfigDB}, runID: "invalid", errorMatch: "run ID"},
		{name: "empty stores", runID: testRunID, errorMatch: "at least one"},
		{name: "duplicate store", storeIDs: []StoreID{StoreIDConfigDB, StoreIDConfigDB},
			runID: testRunID, errorMatch: "duplicate"},
		{name: "unchanged store", storeIDs: []StoreID{StoreIDEntityDB},
			runID: testRunID, errorMatch: "does not require migration"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := controller.Reserve(context.Background(), test.storeIDs, test.runID)

			require.Error(t, err)
			assert.Nil(t, results)
			assert.ErrorContains(t, err, test.errorMatch)
		})
	}
}

func TestNewMigrationStateControllerValidatesDependencies(t *testing.T) {
	manifest := validTestManifest()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	metadataStore := newTestMetadataStore(t, dbClient)

	release := stateControllerTestRelease(manifest)
	invalidRelease := release
	invalidRelease.Manifest.ManifestVersion++
	_, manifestErr := NewMigrationStateController(invalidRelease, metadataStore, &testTransactioner{})
	_, metadataStoreErr := NewMigrationStateController(release, nil, &testTransactioner{})
	_, transactionerErr := NewMigrationStateController(release, metadataStore, nil)

	require.Error(t, manifestErr)
	require.Error(t, metadataStoreErr)
	require.Error(t, transactionerErr)
}

func TestMigrationStateControllerCancelsUnusedReservation(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	reserved := reservedTestState(StoreIDConfigDB)
	cancelledAt := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	dbClient.On("ExecuteContext", mock.Anything, queryCancelMigrationState,
		string(MigrationStageComplete), string(StatusClean), cancelledAt, reserved.ID,
		string(reserved.StoreID), reserved.MetadataVersion, reserved.CompletedLevel, reserved.TargetLevel,
		string(reserved.Stage), string(StatusInProgress), reserved.ActiveRunID).
		Return(int64(1), nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB)),
		newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)
	controller.now = func() time.Time { return cancelledAt }

	states, err := controller.CancelReservations(context.Background(), []State{reserved}, testRunID)

	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, StatusClean, states[0].Status)
	assert.Empty(t, states[0].TargetLevel)
	assert.Empty(t, states[0].ActiveRunID)
}

func TestMigrationStateControllerCancelsMigrateBackToExpandedBoundary(t *testing.T) {
	startedAt := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	cancelledAt := startedAt.Add(time.Minute)
	reserved := State{
		ID: testStateID, StoreID: StoreIDConfigDB, MetadataVersion: 1,
		CompletedLevel: "1.0.0", TargetLevel: "1.1.0", Stage: MigrationStageMigrating,
		Status: StatusInProgress, ActiveRunID: testRunID, UpdatedAt: startedAt,
	}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("ExecuteContext", mock.Anything, queryCancelMigrationPhase,
		"1.1.0", string(MigrationStageExpanded), string(StatusCompleted), cancelledAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", "1.1.0",
		string(MigrationStageMigrating), string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	controller, err := NewMigrationStateController(
		phaseAwareStateControllerTestRelease(), newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)
	controller.now = func() time.Time { return cancelledAt }

	states, err := controller.CancelReservations(context.Background(), []State{reserved}, testRunID)

	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, MigrationStageExpanded, states[0].Stage)
	assert.Equal(t, StatusCompleted, states[0].Status)
	assert.Equal(t, "1.1.0", states[0].TargetLevel)
}

func TestMigrationStateControllerReportsCancellationConflict(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	reserved := reservedTestState(StoreIDConfigDB)
	dbClient.On("ExecuteContext", mock.Anything, queryCancelMigrationState,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).
		Return(int64(0), nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB)),
		newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)

	_, err = controller.CancelReservations(context.Background(), []State{reserved}, testRunID)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunStateConflict))
}

func TestMigrationStateControllerMarksReservedStoreFailed(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	failedAt := time.Date(2026, time.October, 1, 13, 0, 0, 0, time.UTC)
	expectReservedState(dbClient, StoreIDConfigDB, "1.1.0", testRunID, failedAt.Add(-time.Minute))
	dbClient.On("ExecuteContext", mock.Anything, queryFailMigrationState,
		string(StatusFailed), "migration operation failed", failedAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", "1.1.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB)),
		newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)
	controller.now = func() time.Time { return failedAt }

	states, err := controller.MarkFailed(context.Background(), []StoreID{StoreIDConfigDB}, testRunID,
		"migration operation failed")

	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, StatusFailed, states[0].Status)
	assert.Equal(t, "migration operation failed", states[0].FailureSummary)
}

func TestMigrationStateControllerResumesFailedStore(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	failed := reservedTestState(StoreIDConfigDB)
	failed.Status = StatusFailed
	failed.FailureSummary = "temporary database failure"
	resumedAt := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
	row := validStateRow(failed.UpdatedAt)
	row["status"] = string(StatusFailed)
	row["stage"] = string(MigrationStageExpanding)
	row["target_level"] = failed.TargetLevel
	row["active_run_id"] = failed.ActiveRunID
	row["failure_summary"] = failed.FailureSummary
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{row}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryResumeMigrationState,
		string(StatusInProgress), resumedAt, failed.ID, string(failed.StoreID), failed.MetadataVersion,
		failed.CompletedLevel, failed.TargetLevel, string(failed.Stage), string(StatusFailed), failed.ActiveRunID,
		failed.FailureSummary, failed.UpdatedAt).
		Return(int64(1), nil).Once()
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB)),
		newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)
	controller.now = func() time.Time { return resumedAt }

	results, err := controller.Resume(context.Background(), []State{failed}, testRunID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Resumed)
	assert.Equal(t, StatusInProgress, results[0].State.Status)
	assert.Empty(t, results[0].State.FailureSummary)
}

func TestMigrationStateControllerValidatesTransitionRequests(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	controller, err := NewMigrationStateController(
		stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB)),
		newTestMetadataStore(t, dbClient), &testTransactioner{})
	require.NoError(t, err)
	reserved := reservedTestState(StoreIDConfigDB)
	wrongRun := reserved
	wrongRun.ActiveRunID = "0199a123-4571-7abc-8def-0123456789ab"

	_, cancelRunErr := controller.CancelReservations(context.Background(), []State{reserved}, "invalid")
	_, cancelEmptyErr := controller.CancelReservations(context.Background(), nil, testRunID)
	_, cancelOwnerErr := controller.CancelReservations(context.Background(), []State{wrongRun}, testRunID)
	_, failRunErr := controller.MarkFailed(
		context.Background(), []StoreID{StoreIDConfigDB}, "invalid", "failed")
	_, failEmptyErr := controller.MarkFailed(context.Background(), nil, testRunID, "failed")

	require.Error(t, cancelRunErr)
	require.Error(t, cancelEmptyErr)
	require.Error(t, cancelOwnerErr)
	require.Error(t, failRunErr)
	require.Error(t, failEmptyErr)
}

func reservedTestState(storeID StoreID) State {
	return State{
		ID: testStateID, StoreID: storeID, MetadataVersion: 1,
		CompletedLevel: "1.0.0", TargetLevel: "1.1.0", Stage: MigrationStageExpanding,
		Status:      StatusInProgress,
		ActiveRunID: testRunID, UpdatedAt: time.Now().UTC(),
	}
}

func stateControllerTestRelease(manifest ReleaseManifest) ReleaseDefinition {
	operations := make([]Operation, 0)
	for _, requirement := range manifest.Stores {
		if requirement.Changed {
			operations = append(operations, validPlanOperation(requirement.ID))
		}
	}
	return ReleaseDefinition{
		Manifest: manifest,
		Plan:     MigrationPlan{TargetVersion: manifest.ReleaseVersion, Expand: operations},
	}
}

func TestMigrationStateControllerFinalizesReservedStore(t *testing.T) {
	release := stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB))
	operation := release.Plan.Expand[0].Definition()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	startedAt := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	finalizedAt := startedAt.Add(time.Minute)
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, startedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			operation.ID, 0, release.Manifest.ReleaseVersion, startedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), finalizedAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageExpanding),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	controller := newTestFinalizationController(t, release, dbClient, transactioner)
	controller.now = func() time.Time { return finalizedAt }

	result, err := controller.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.NoError(t, err)
	assert.True(t, result.Finalized)
	assert.False(t, result.AlreadyAtTarget)
	assert.Equal(t, StatusClean, result.State.Status)
	assert.Equal(t, release.Manifest.ReleaseVersion, result.State.CompletedLevel)
	assert.Empty(t, result.State.ActiveRunID)
	assert.Equal(t, finalizedAt, result.State.UpdatedAt)
	assert.Equal(t, 1, transactioner.calls)
}

func TestMigrationStateControllerFinalizationPreservesStoreAlreadyAtTarget(t *testing.T) {
	release := stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB))
	dbClient := providermock.NewDBClientInterfaceMock(t)
	row := validStateRow(time.Now().UTC())
	row["completed_level"] = release.Manifest.ReleaseVersion
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{row}, nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})

	result, err := controller.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.NoError(t, err)
	assert.False(t, result.Finalized)
	assert.True(t, result.AlreadyAtTarget)
}

func TestMigrationStateControllerFinalizationRequiresCompleteProgress(t *testing.T) {
	release := stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB))
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{}, nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})

	result, err := controller.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.Error(t, err)
	assert.False(t, result.Finalized)
	assert.ErrorContains(t, err, "expected at least 1")
}

func TestMigrationStateControllerFinalizationReportsConditionalUpdateConflict(t *testing.T) {
	release := stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB))
	operation := release.Plan.Expand[0].Definition()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			operation.ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).
		Return(int64(0), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})

	_, err := controller.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFinalizationConflict))
}

func TestMigrationStateControllerFinalizationRejectsInvalidRequests(t *testing.T) {
	release := stateControllerTestRelease(changedStoreManifest(StoreIDConfigDB))
	dbClient := providermock.NewDBClientInterfaceMock(t)
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})

	_, runIDErr := controller.Finalize(context.Background(), StoreIDConfigDB, "invalid")
	_, unchangedStoreErr := controller.Finalize(context.Background(), StoreIDEntityDB, testRunID)
	_, unknownStoreErr := controller.Finalize(context.Background(), "unknown", testRunID)

	require.Error(t, runIDErr)
	assert.ErrorContains(t, unchangedStoreErr, "does not require migration")
	assert.ErrorContains(t, unknownStoreErr, "is not declared")
}

func TestMigrationStateControllerCompletesExpandAtExpandedBoundary(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	startedAt := time.Date(2026, time.October, 3, 8, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageExpanding, StatusInProgress, release.Manifest.ReleaseVersion, testRunID, startedAt,
		)}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Expand[0].Definition().ID, 0, release.Manifest.ReleaseVersion, startedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageExpanded), string(StatusCompleted),
		nil, nil, completedAt, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageExpanding), string(StatusInProgress),
		testRunID, nil, startedAt).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return completedAt }

	result, err := controller.CompletePhase(
		context.Background(), StoreIDConfigDB, OperationPhaseExpand, testRunID)

	require.NoError(t, err)
	assert.True(t, result.Finalized)
	assert.Equal(t, MigrationStageExpanded, result.State.Stage)
	assert.Equal(t, StatusCompleted, result.State.Status)
	assert.Equal(t, "1.0.0", result.State.CompletedLevel)
	assert.Equal(t, release.Manifest.ReleaseVersion, result.State.TargetLevel)
}

func TestMigrationStateControllerReservesMigrateFromExpandedBoundary(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	expandedAt := time.Date(2026, time.October, 3, 8, 0, 0, 0, time.UTC)
	reservedAt := expandedAt.Add(time.Minute)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageExpanded, StatusCompleted, release.Manifest.ReleaseVersion, "", expandedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageMigrating), string(StatusInProgress),
		testRunID, nil, reservedAt, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageExpanded), string(StatusCompleted),
		nil, nil, expandedAt).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return reservedAt }

	results, err := controller.ReservePhase(
		context.Background(), []StoreID{StoreIDConfigDB}, OperationPhaseMigrate, testRunID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Reserved)
	assert.Equal(t, MigrationStageMigrating, results[0].State.Stage)
	assert.Equal(t, StatusInProgress, results[0].State.Status)
}

func TestMigrationStateControllerReservesContractWhenMigrateIsUnused(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	release.Plan.Migrate = nil
	contractReadyAt := time.Date(2026, time.October, 3, 8, 0, 0, 0, time.UTC)
	reservedAt := contractReadyAt.Add(time.Minute)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageContractReady, StatusCompleted, release.Manifest.ReleaseVersion, "", contractReadyAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageContracting), string(StatusInProgress),
		testRunID, nil, reservedAt, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageContractReady), string(StatusCompleted),
		nil, nil, contractReadyAt).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return reservedAt }

	results, err := controller.ReservePhase(
		context.Background(), []StoreID{StoreIDConfigDB}, OperationPhaseContract, testRunID)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Reserved)
	assert.Equal(t, MigrationStageContracting, results[0].State.Stage)
}

func TestMigrationStateControllerCompletesMigrateAtContractReadyBoundary(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	startedAt := time.Date(2026, time.October, 3, 9, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageMigrating, StatusInProgress, release.Manifest.ReleaseVersion, testRunID, startedAt,
		)}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Migrate[0].Definition().ID, 1, release.Manifest.ReleaseVersion, startedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageContractReady), string(StatusCompleted),
		nil, nil, completedAt, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageMigrating), string(StatusInProgress),
		testRunID, nil, startedAt).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return completedAt }

	result, err := controller.CompletePhase(
		context.Background(), StoreIDConfigDB, OperationPhaseMigrate, testRunID)

	require.NoError(t, err)
	assert.Equal(t, MigrationStageContractReady, result.State.Stage)
	assert.Equal(t, StatusCompleted, result.State.Status)
}

func TestMigrationStateControllerCompletesSelectedDeploymentWithoutAdvancingGlobalBoundary(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	startedAt := time.Date(2026, time.October, 3, 9, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	progressRow := completedProgressRow(
		release.Plan.Migrate[0].Definition().ID, 1, release.Manifest.ReleaseVersion, startedAt)
	progressRow["execution_target_id"] = "deployment-a"
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageMigrating, StatusInProgress, release.Manifest.ReleaseVersion, testRunID, startedAt,
		)}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{progressRow}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpsertDeploymentMigrationState,
		testProgressID, testStateID, "deployment-a", release.Manifest.ReleaseVersion, completedAt).
		Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryTransitionMigrationState,
		"1.0.0", release.Manifest.ReleaseVersion, string(MigrationStageExpanded), string(StatusCompleted),
		nil, nil, completedAt, testStateID, string(StoreIDConfigDB), 1, "1.0.0",
		release.Manifest.ReleaseVersion, string(MigrationStageMigrating), string(StatusInProgress),
		testRunID, nil, startedAt).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return completedAt }
	controller.newID = sequenceIDs(testProgressID)

	result, err := controller.CompletePhaseSelection(
		context.Background(), StoreIDConfigDB, OperationPhaseMigrate, testRunID,
		MigrationSelection{DeploymentIDs: []string{"deployment-a"}})

	require.NoError(t, err)
	assert.True(t, result.Finalized)
	assert.Equal(t, MigrationStageExpanded, result.State.Stage)
	assert.Equal(t, "1.0.0", result.State.CompletedLevel)
	assert.Equal(t, release.Manifest.ReleaseVersion, result.State.TargetLevel)
}

func TestMigrationStateControllerCompletesContractAtTarget(t *testing.T) {
	release := phaseAwareStateControllerTestRelease()
	startedAt := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{phaseStateRow(
			MigrationStageContracting, StatusInProgress, release.Manifest.ReleaseVersion, testRunID, startedAt,
		)}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			release.Plan.Contract[0].Definition().ID, 2, release.Manifest.ReleaseVersion, startedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(MigrationStageComplete), string(StatusClean), completedAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(MigrationStageContracting),
		string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	controller := newTestFinalizationController(t, release, dbClient, &testTransactioner{})
	controller.now = func() time.Time { return completedAt }

	result, err := controller.CompletePhase(
		context.Background(), StoreIDConfigDB, OperationPhaseContract, testRunID)

	require.NoError(t, err)
	assert.Equal(t, MigrationStageComplete, result.State.Stage)
	assert.Equal(t, StatusClean, result.State.Status)
	assert.Equal(t, release.Manifest.ReleaseVersion, result.State.CompletedLevel)
	assert.Empty(t, result.State.TargetLevel)
}

func phaseAwareStateControllerTestRelease() ReleaseDefinition {
	manifest := changedStoreManifest(StoreIDConfigDB)
	expand := validPlanOperation(StoreIDConfigDB)
	expand.definition.ID = "2.0.0/configdb/expand"
	migrate := validPlanOperation(StoreIDConfigDB)
	migrate.definition.ID = "2.0.0/configdb/migrate"
	contract := validPlanOperation(StoreIDConfigDB)
	contract.definition.ID = "2.0.0/configdb/contract"
	return ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{expand},
		Migrate:       []Operation{migrate},
		Contract:      []Operation{contract},
	}}
}

func phaseStateRow(
	stage MigrationStage,
	status Status,
	targetLevel string,
	activeRunID string,
	updatedAt time.Time,
) map[string]interface{} {
	return map[string]interface{}{
		"id": testStateID, "store_id": string(StoreIDConfigDB), "metadata_version": int32(1),
		"completed_level": "1.0.0", "target_level": nullableString(targetLevel),
		"stage": string(stage), "status": string(status), "active_run_id": nullableString(activeRunID),
		"failure_summary": nil, "updated_at": updatedAt,
	}
}

func newTestFinalizationController(
	t *testing.T,
	release ReleaseDefinition,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *MigrationStateController {
	t.Helper()
	controller, err := NewMigrationStateController(
		release, newTestMetadataStore(t, dbClient), transactioner)
	require.NoError(t, err)
	return controller
}

func completedProgressRow(
	operationID OperationID,
	operationOrder int,
	targetLevel string,
	updatedAt time.Time,
) map[string]interface{} {
	return map[string]interface{}{
		"id": testProgressID, "migration_state_id": testStateID, "run_id": testRunID,
		"target_level": targetLevel, "operation_id": string(operationID),
		"operation_order": int32(operationOrder), "execution_target_id": "", "status": string(StatusCompleted),
		"checkpoint_data": nil, "updated_at": updatedAt,
	}
}
