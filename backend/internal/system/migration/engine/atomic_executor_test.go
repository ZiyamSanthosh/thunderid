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

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestAtomicOperationExecutorExecutesAndCompletesProgress(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := atomicTestOperation(t, VerificationSource, VerificationTarget)
	executedQuery := dbmodel.DBQuery{
		ID:    "2.0.0/entitydb/example/add-migrated-column",
		Query: "ALTER TABLE example ADD COLUMN migrated BOOLEAN",
	}
	executedAt := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 3,
		string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, executedQuery).Return(int64(0), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, executedAt, testProgressID, testStateID, testRunID).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	executor := newTestAtomicExecutor(t, dbClient, transactioner)
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation, testRunID, 3)

	require.NoError(t, err)
	assert.False(t, result.ExecutionSkipped)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
	assert.Equal(t, string(operation.Definition().ID), result.Progress.OperationID)
	assert.Equal(t, 1, transactioner.calls)
}

func TestAtomicOperationExecutorSkipsVerifiedTarget(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := atomicTestOperation(t, VerificationTarget, VerificationTarget)
	executedAt := time.Date(2026, time.October, 1, 10, 0, 0, 0, time.UTC)
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 0,
		string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, executedAt, testProgressID, testStateID, testRunID).
		Return(int64(1), nil).Once()
	executor := newTestAtomicExecutor(t, dbClient, &testTransactioner{})
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation, testRunID, 0)

	require.NoError(t, err)
	assert.True(t, result.ExecutionSkipped)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
}

func TestAtomicOperationExecutorRejectsInvalidPreVerification(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := atomicTestOperation(t, VerificationInvalid, VerificationTarget)
	executedAt := time.Date(2026, time.October, 1, 11, 0, 0, 0, time.UTC)
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 0,
		string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	executor := newTestAtomicExecutor(t, dbClient, transactioner)
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(context.Background(), operation, testRunID, 0)

	require.Error(t, err)
	assert.Empty(t, result.Progress.ID)
	assert.ErrorContains(t, err, "pre-verification is invalid")
	assert.Equal(t, 1, transactioner.calls)
}

func TestAtomicOperationExecutorRequiresMatchingReservation(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := atomicTestOperation(t, VerificationSource, VerificationTarget)
	updatedAt := time.Now().UTC()
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0",
		"0199a123-4571-7abc-8def-0123456789ab", updatedAt)
	executor := newTestAtomicExecutor(t, dbClient, &testTransactioner{})
	executor.newID = sequenceIDs(testProgressID)

	_, err := executor.Execute(context.Background(), operation, testRunID, 0)

	require.Error(t, err)
	assert.ErrorContains(t, err, "reserved by a different run")
}

func TestAtomicOperationExecutorRejectsUnsupportedExecution(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	executor := newTestAtomicExecutor(t, dbClient, &testTransactioner{})
	operation := testOperation{definition: OperationDefinition{
		ID: "2.0.0/entitydb/resumable", Description: "Resumable operation",
		StoreID: StoreIDEntityDB, InterruptionMode: InterruptionModeResumable, ScopeType: ScopeTypeStore,
	}}

	_, nilOperationErr := executor.Execute(context.Background(), nil, testRunID, 0)
	_, runIDErr := executor.Execute(context.Background(), operation, "invalid", 0)
	_, orderErr := executor.Execute(context.Background(), operation, testRunID, -1)
	_, interruptionErr := executor.Execute(context.Background(), operation, testRunID, 0)
	operation.definition.InterruptionMode = InterruptionModeAtomic
	operation.definition.ScopeType = ScopeTypeDeployment
	_, scopeErr := executor.Execute(context.Background(), operation, testRunID, 0)

	require.Error(t, nilOperationErr)
	require.Error(t, runIDErr)
	require.Error(t, orderErr)
	assert.ErrorContains(t, interruptionErr, "must be atomic")
	assert.ErrorContains(t, scopeErr, "supports only store scope")
}

func TestNewAtomicOperationExecutorValidatesDependencies(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	metadataStore := newTestMetadataStore(t, dbClient)
	transactioner := &testTransactioner{}

	executor, err := NewAtomicOperationExecutor(metadataStore, transactioner, atomicTestStores(dbClient))
	_, metadataErr := NewAtomicOperationExecutor(nil, transactioner, nil)
	_, transactionerErr := NewAtomicOperationExecutor(metadataStore, nil, nil)

	require.NoError(t, err)
	assert.NotNil(t, executor)
	require.Error(t, metadataErr)
	require.Error(t, transactionerErr)
}

func atomicTestOperation(
	t *testing.T,
	preState VerificationState,
	postState VerificationState,
) *SQLOperation {
	t.Helper()
	config := validSQLOperationConfig()
	config.PreVerifier = func(context.Context, ReadAccess, Scope) (VerificationResult, error) {
		result := VerificationResult{State: preState, Summary: "Pre-verification result"}
		if preState == VerificationInvalid {
			result.Findings = []Finding{{Code: "invalid_source", Message: "Source state is invalid"}}
		}
		return result, nil
	}
	config.PostVerifier = func(context.Context, ReadAccess, Scope) (VerificationResult, error) {
		return VerificationResult{State: postState, Summary: "Post-verification result"}, nil
	}
	operation, err := NewSQLOperation(config)
	require.NoError(t, err)
	return operation
}

func atomicTestStores(dbClient *providermock.DBClientInterfaceMock) map[StoreID]ReadStore {
	return map[StoreID]ReadStore{
		StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: dbClient},
		StoreIDConfigDB: {DatabaseType: DatabaseTypePostgres, Client: dbClient},
	}
}

func newTestAtomicExecutor(
	t *testing.T,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *AtomicOperationExecutor {
	t.Helper()
	executor, err := NewAtomicOperationExecutor(
		newTestMetadataStore(t, dbClient), transactioner, atomicTestStores(dbClient))
	require.NoError(t, err)
	return executor
}

func expectReservedState(
	dbClient *providermock.DBClientInterfaceMock,
	storeID StoreID,
	targetLevel string,
	runID string,
	updatedAt time.Time,
) {
	row := validStateRow(updatedAt)
	row["store_id"] = string(storeID)
	row["target_level"] = targetLevel
	row["status"] = string(StatusInProgress)
	row["active_run_id"] = runID
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(storeID)).
		Return([]map[string]interface{}{row}, nil).Once()
}

func TestAtomicOperationExecutorReturnsIDGenerationFailure(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	executor := newTestAtomicExecutor(t, dbClient, &testTransactioner{})
	executor.newID = func() (string, error) { return "", errors.New("random source failed") }

	_, err := executor.Execute(context.Background(),
		atomicTestOperation(t, VerificationSource, VerificationTarget), testRunID, 0)

	require.Error(t, err)
	assert.ErrorContains(t, err, "random source failed")
}
