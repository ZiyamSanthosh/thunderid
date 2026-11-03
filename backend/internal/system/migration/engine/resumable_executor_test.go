// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

type scriptedResumableOperation struct {
	definition  OperationDefinition
	target      ExecutionTarget
	batches     []ResumableBatchResult
	checkpoints []json.RawMessage
}

func (o *scriptedResumableOperation) Definition() OperationDefinition {
	return o.definition
}

func (o *scriptedResumableOperation) ResolveTargets(
	context.Context,
	ReadAccess,
	MigrationSelection,
) ([]ExecutionTarget, error) {
	return []ExecutionTarget{o.target}, nil
}

func (o *scriptedResumableOperation) PreVerify(
	context.Context,
	ReadAccess,
	ExecutionTarget,
) (VerificationResult, error) {
	return VerificationResult{State: VerificationSource, Summary: "Source is ready"}, nil
}

func (o *scriptedResumableOperation) ExecuteBatch(
	_ context.Context,
	_ OperationExecution,
	checkpoint json.RawMessage,
) (ResumableBatchResult, error) {
	o.checkpoints = append(o.checkpoints, append(json.RawMessage(nil), checkpoint...))
	result := o.batches[0]
	o.batches = o.batches[1:]
	return result, nil
}

func (o *scriptedResumableOperation) PostVerify(
	context.Context,
	ReadAccess,
	ExecutionTarget,
) (VerificationResult, error) {
	return VerificationResult{State: VerificationTarget, Summary: "Target is ready"}, nil
}

func TestResumableOperationExecutorCommitsBatchesWithCheckpoints(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := newScriptedResumableOperation(
		ResumableBatchResult{CheckpointData: json.RawMessage(`{"lastID":"500"}`)},
		ResumableBatchResult{CheckpointData: json.RawMessage(`{"lastID":"750"}`), Completed: true},
	)
	executedAt := time.Date(2026, time.October, 6, 10, 0, 0, 0, time.UTC)
	expectResumableReservedState(dbClient, 3, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 2,
		"", string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusInProgress), `{"lastID":"500"}`, executedAt,
		testProgressID, testStateID, testRunID).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), `{"lastID":"750"}`, executedAt,
		testProgressID, testStateID, testRunID).Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	executor := newTestResumableExecutor(t, dbClient, transactioner)
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation, MigrationSelection{}, testRunID, 2)

	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
	assert.JSONEq(t, `{"lastID":"750"}`, string(result.Progress.CheckpointData))
	assert.Equal(t, 2, result.CommittedBatches)
	assert.Equal(t, []json.RawMessage{nil, json.RawMessage(`{"lastID":"500"}`)}, operation.checkpoints)
	assert.Equal(t, 3, transactioner.calls)
}

func TestResumableOperationExecutorResumesFromCommittedCheckpoint(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := newScriptedResumableOperation(
		ResumableBatchResult{CheckpointData: json.RawMessage(`{"lastID":"750"}`), Completed: true},
	)
	executedAt := time.Date(2026, time.October, 6, 11, 0, 0, 0, time.UTC)
	expectResumableReservedState(dbClient, 1, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), `{"lastID":"750"}`, executedAt,
		testProgressID, testStateID, testRunID).Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	executor := newTestResumableExecutor(t, dbClient, transactioner)
	executor.now = func() time.Time { return executedAt }
	progress := resumableTestProgress(executedAt)
	progress.CheckpointData = json.RawMessage(`{"lastID":"500"}`)

	result, err := executor.Resume(context.Background(), operation, MigrationSelection{}, progress)

	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
	assert.Equal(t, 1, result.CommittedBatches)
	require.Len(t, operation.checkpoints, 1)
	assert.JSONEq(t, `{"lastID":"500"}`, string(operation.checkpoints[0]))
	assert.Equal(t, 1, transactioner.calls)
}

func TestResumableOperationExecutorPersistsDeploymentTarget(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := newScriptedResumableOperation(ResumableBatchResult{Completed: true})
	operation.target = ExecutionTarget{DeploymentID: "deployment-a"}
	executedAt := time.Date(2026, time.October, 6, 11, 30, 0, 0, time.UTC)
	expectResumableReservedState(dbClient, 2, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 0,
		"deployment-a", string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, executedAt,
		testProgressID, testStateID, testRunID).Return(int64(1), nil).Once()
	executor := newTestResumableExecutor(t, dbClient, &testTransactioner{})
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation,
		MigrationSelection{DeploymentIDs: []string{"deployment-a"}}, testRunID, 0)

	require.NoError(t, err)
	assert.Equal(t, "deployment-a", result.Progress.ExecutionTargetID)
}

func TestResumableOperationExecutorRejectsBatchWithoutCheckpointProgress(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := newScriptedResumableOperation(ResumableBatchResult{})
	executedAt := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	expectResumableReservedState(dbClient, 2, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 0,
		"", string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	executor := newTestResumableExecutor(t, dbClient, transactioner)
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation, MigrationSelection{}, testRunID, 0)

	require.Error(t, err)
	assert.ErrorContains(t, err, "incomplete batch requires a checkpoint")
	assert.Equal(t, StatusInProgress, result.Progress.Status)
	assert.Zero(t, result.CommittedBatches)
	assert.Equal(t, 2, transactioner.calls)
}

func newScriptedResumableOperation(batches ...ResumableBatchResult) *scriptedResumableOperation {
	return &scriptedResumableOperation{
		definition: OperationDefinition{
			ID: "2.0.0/entitydb/backfill-example", Description: "Backfill example data",
			StoreID: StoreIDEntityDB, Kind: OperationKindDataChange,
			InterruptionMode: InterruptionModeResumable,
		},
		batches: append([]ResumableBatchResult(nil), batches...),
	}
}

func newTestResumableExecutor(
	t *testing.T,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *ResumableOperationExecutor {
	t.Helper()
	executor, err := NewResumableOperationExecutor(
		newTestMetadataStore(t, dbClient), transactioner, atomicTestStores(dbClient))
	require.NoError(t, err)
	return executor
}

func expectResumableReservedState(
	dbClient *providermock.DBClientInterfaceMock,
	times int,
	updatedAt time.Time,
) {
	row := validStateRow(updatedAt)
	row["store_id"] = string(StoreIDEntityDB)
	row["target_level"] = "2.0.0"
	row["stage"] = string(MigrationStageMigrating)
	row["status"] = string(StatusInProgress)
	row["active_run_id"] = testRunID
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDEntityDB)).
		Return([]map[string]interface{}{row}, nil).Times(times)
}

func resumableTestProgress(updatedAt time.Time) Progress {
	return Progress{
		ID: testProgressID, MigrationStateID: testStateID, RunID: testRunID, TargetLevel: "2.0.0",
		OperationID: "2.0.0/entitydb/backfill-example", OperationOrder: 2,
		Status: StatusInProgress, UpdatedAt: updatedAt,
	}
}
