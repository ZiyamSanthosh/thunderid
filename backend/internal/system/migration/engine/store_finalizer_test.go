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

func TestStoreFinalizerCompletesReservedStore(t *testing.T) {
	release := finalizationTestRelease()
	operation := release.Plan.Operations[0].Definition()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	startedAt := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	finalizedAt := startedAt.Add(time.Minute)
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, startedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			operation.ID, 0, release.Manifest.ReleaseVersion, startedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState,
		release.Manifest.ReleaseVersion, string(StatusClean), finalizedAt,
		testStateID, string(StoreIDConfigDB), 1, "1.0.0", string(StatusInProgress), testRunID).
		Return(int64(1), nil).Once()
	transactioner := &testTransactioner{}
	finalizer := newTestStoreFinalizer(t, release, dbClient, transactioner)
	finalizer.now = func() time.Time { return finalizedAt }

	result, err := finalizer.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.NoError(t, err)
	assert.True(t, result.Finalized)
	assert.False(t, result.AlreadyAtTarget)
	assert.Equal(t, StatusClean, result.State.Status)
	assert.Equal(t, release.Manifest.ReleaseVersion, result.State.CompletedLevel)
	assert.Empty(t, result.State.ActiveRunID)
	assert.Equal(t, finalizedAt, result.State.UpdatedAt)
	assert.Equal(t, 1, transactioner.calls)
}

func TestStoreFinalizerPreservesStoreAlreadyAtTarget(t *testing.T) {
	release := finalizationTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	row := validStateRow(time.Now().UTC())
	row["completed_level"] = release.Manifest.ReleaseVersion
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{row}, nil).Once()
	finalizer := newTestStoreFinalizer(t, release, dbClient, &testTransactioner{})

	result, err := finalizer.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.NoError(t, err)
	assert.False(t, result.Finalized)
	assert.True(t, result.AlreadyAtTarget)
}

func TestStoreFinalizerRequiresCompleteProgress(t *testing.T) {
	release := finalizationTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{}, nil).Once()
	finalizer := newTestStoreFinalizer(t, release, dbClient, &testTransactioner{})

	result, err := finalizer.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.Error(t, err)
	assert.False(t, result.Finalized)
	assert.ErrorContains(t, err, "expected 1")
}

func TestStoreFinalizerReportsConditionalUpdateConflict(t *testing.T) {
	release := finalizationTestRelease()
	operation := release.Plan.Operations[0].Definition()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	updatedAt := time.Now().UTC()
	expectReservedState(dbClient, StoreIDConfigDB, release.Manifest.ReleaseVersion, testRunID, updatedAt)
	dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
		Return([]map[string]interface{}{completedProgressRow(
			operation.ID, 0, release.Manifest.ReleaseVersion, updatedAt,
		)}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryFinalizeMigrationState, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(int64(0), nil).Once()
	finalizer := newTestStoreFinalizer(t, release, dbClient, &testTransactioner{})

	_, err := finalizer.Finalize(context.Background(), StoreIDConfigDB, testRunID)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFinalizationConflict))
}

func TestStoreFinalizerRejectsInvalidRequests(t *testing.T) {
	release := finalizationTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	finalizer := newTestStoreFinalizer(t, release, dbClient, &testTransactioner{})

	_, runIDErr := finalizer.Finalize(context.Background(), StoreIDConfigDB, "invalid")
	_, unchangedStoreErr := finalizer.Finalize(context.Background(), StoreIDEntityDB, testRunID)
	_, unknownStoreErr := finalizer.Finalize(context.Background(), "unknown", testRunID)

	require.Error(t, runIDErr)
	assert.ErrorContains(t, unchangedStoreErr, "does not require migration")
	assert.ErrorContains(t, unknownStoreErr, "is not declared")
}

func TestNewStoreFinalizerValidatesDependencies(t *testing.T) {
	release := finalizationTestRelease()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	metadataStore := newTestMetadataStore(t, dbClient)
	transactioner := &testTransactioner{}
	invalidRelease := release
	invalidRelease.Plan.TargetVersion = "invalid"

	finalizer, err := NewStoreFinalizer(release, metadataStore, transactioner)
	_, releaseErr := NewStoreFinalizer(invalidRelease, metadataStore, transactioner)
	_, metadataErr := NewStoreFinalizer(release, nil, transactioner)
	_, transactionerErr := NewStoreFinalizer(release, metadataStore, nil)

	require.NoError(t, err)
	assert.NotNil(t, finalizer)
	require.Error(t, releaseErr)
	require.Error(t, metadataErr)
	require.Error(t, transactionerErr)
}

func finalizationTestRelease() ReleaseDefinition {
	manifest := changedStoreManifest(StoreIDConfigDB)
	return ReleaseDefinition{
		Manifest: manifest,
		Plan: MigrationPlan{
			TargetVersion: manifest.ReleaseVersion,
			Operations:    []Operation{validPlanOperation(StoreIDConfigDB)},
		},
	}
}

func newTestStoreFinalizer(
	t *testing.T,
	release ReleaseDefinition,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *StoreFinalizer {
	t.Helper()
	finalizer, err := NewStoreFinalizer(release, newTestMetadataStore(t, dbClient), transactioner)
	require.NoError(t, err)
	return finalizer
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
		"operation_order": int32(operationOrder), "status": string(StatusCompleted),
		"checkpoint_data": nil, "updated_at": updatedAt,
	}
}
