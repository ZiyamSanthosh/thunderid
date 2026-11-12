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

func TestOnlineOperationExecutorExecutesOutsideTransactionAndCompletesProgress(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	finalizedInTransaction := false
	operation := &finalizingOnlineTestOperation{
		SQLOperation: onlineTestOperation(t, VerificationSource, VerificationTarget),
		finalized:    &finalizedInTransaction,
	}
	executedAt := time.Date(2026, time.October, 16, 9, 0, 0, 0, time.UTC)
	executedQuery := dbmodel.DBQuery{
		ID:    string(operation.Definition().ID) + "/create-index-concurrently",
		Query: `CREATE UNIQUE INDEX CONCURRENTLY example_online_idx ON example (deployment_id, id)`,
	}
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationProgress,
		testStateID, "2.0.0", string(operation.Definition().ID), "").Return(nil, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
		testProgressID, testStateID, testRunID, "2.0.0", string(operation.Definition().ID), 3,
		"", string(StatusInProgress), nil, executedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.MatchedBy(func(ctx context.Context) bool {
		return ctx.Value(onlineTransactionContextKey{}) == nil
	}), executedQuery).Return(int64(0), nil).Once()
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, executedAt, testProgressID, testStateID, testRunID).
		Return(int64(1), nil).Once()
	transactioner := &onlineTestTransactioner{}
	executor := newTestOnlineExecutor(t, dbClient, transactioner)
	executor.newID = sequenceIDs(testProgressID)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(
		context.Background(), operation, MigrationSelection{}, testRunID, 3, nil)

	require.NoError(t, err)
	assert.False(t, result.ExecutionSkipped)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
	assert.True(t, finalizedInTransaction)
	assert.Equal(t, 2, transactioner.calls)
}

func TestOnlineOperationExecutorCompletesInterruptedOperationFromVerification(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := onlineTestOperation(t, VerificationTarget, VerificationTarget)
	executedAt := time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)
	progress := Progress{
		ID: testProgressID, MigrationStateID: testStateID, RunID: testRunID, TargetLevel: "2.0.0",
		OperationID: string(operation.Definition().ID), OperationOrder: 3,
		Status: StatusInProgress, UpdatedAt: executedAt,
	}
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
		string(StatusCompleted), nil, executedAt, testProgressID, testStateID, testRunID).
		Return(int64(1), nil).Once()
	transactioner := &onlineTestTransactioner{}
	executor := newTestOnlineExecutor(t, dbClient, transactioner)
	executor.now = func() time.Time { return executedAt }

	result, err := executor.Execute(
		context.Background(), operation, MigrationSelection{}, testRunID, 3, &progress)

	require.NoError(t, err)
	assert.True(t, result.ExecutionSkipped)
	assert.Equal(t, StatusCompleted, result.Progress.Status)
	assert.Equal(t, 1, transactioner.calls)
}

func TestOnlineOperationExecutorRejectsMismatchedProgress(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	operation := onlineTestOperation(t, VerificationSource, VerificationTarget)
	executedAt := time.Date(2026, time.October, 16, 11, 0, 0, 0, time.UTC)
	progress := Progress{
		ID: testProgressID, MigrationStateID: testStateID, RunID: testRunID, TargetLevel: "2.0.0",
		OperationID: string(operation.Definition().ID), OperationOrder: 4,
		Status: StatusInProgress, UpdatedAt: executedAt,
	}
	expectReservedState(dbClient, StoreIDEntityDB, "2.0.0", testRunID, executedAt)
	executor := newTestOnlineExecutor(t, dbClient, &onlineTestTransactioner{})

	_, err := executor.Execute(
		context.Background(), operation, MigrationSelection{}, testRunID, 3, &progress)

	require.Error(t, err)
	assert.ErrorContains(t, err, "progress does not identify")
}

func TestNewOnlineOperationExecutorValidatesDependencies(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	metadataStore := newTestMetadataStore(t, dbClient)
	transactioner := &onlineTestTransactioner{}

	executor, err := NewOnlineOperationExecutor(metadataStore, transactioner, atomicTestStores(dbClient))
	_, metadataErr := NewOnlineOperationExecutor(nil, transactioner, nil)
	_, transactionerErr := NewOnlineOperationExecutor(metadataStore, nil, nil)

	require.NoError(t, err)
	assert.NotNil(t, executor)
	require.Error(t, metadataErr)
	require.Error(t, transactionerErr)
}

func TestOnlineOperationExecutorReturnsIDGenerationFailure(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	executor := newTestOnlineExecutor(t, dbClient, &onlineTestTransactioner{})
	executor.newID = func() (string, error) { return "", errors.New("random source failed") }

	_, err := executor.Execute(context.Background(), onlineTestOperation(t, VerificationSource, VerificationTarget),
		MigrationSelection{}, testRunID, 0, nil)

	require.Error(t, err)
	assert.ErrorContains(t, err, "random source failed")
}

func onlineTestOperation(
	t *testing.T,
	preState VerificationState,
	postState VerificationState,
) *SQLOperation {
	t.Helper()
	config := validSQLOperationConfig()
	config.Definition.Kind = OperationKindSchemaChange
	config.Definition.InterruptionMode = InterruptionModeOnline
	config.TargetResolver = nil
	config.PreVerifier = func(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error) {
		return VerificationResult{State: preState, Summary: "Pre-verification result"}, nil
	}
	config.Implementations[DatabaseTypePostgres] = SQLImplementation{Statements: []SQLStatement{{
		ID:    "create-index-concurrently",
		Query: `CREATE UNIQUE INDEX CONCURRENTLY example_online_idx ON example (deployment_id, id)`,
	}}}
	config.PostVerifier = func(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error) {
		return VerificationResult{State: postState, Summary: "Post-verification result"}, nil
	}
	operation, err := NewSQLOperation(config)
	require.NoError(t, err)
	return operation
}

func newTestOnlineExecutor(
	t *testing.T,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *onlineTestTransactioner,
) *OnlineOperationExecutor {
	t.Helper()
	executor, err := NewOnlineOperationExecutor(
		newTestMetadataStore(t, dbClient), transactioner, atomicTestStores(dbClient))
	require.NoError(t, err)
	return executor
}

type onlineTransactionContextKey struct{}

type finalizingOnlineTestOperation struct {
	*SQLOperation
	finalized *bool
}

func (o *finalizingOnlineTestOperation) Finalize(ctx context.Context, _ OperationExecution) error {
	*o.finalized = ctx.Value(onlineTransactionContextKey{}) == true
	return nil
}

type onlineTestTransactioner struct {
	calls int
}

func (t *onlineTestTransactioner) Transact(ctx context.Context, txFunc func(context.Context) error) error {
	t.calls++
	return txFunc(context.WithValue(ctx, onlineTransactionContextKey{}, true))
}
