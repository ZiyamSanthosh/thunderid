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

func TestMetadataStoreGetState(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()

	t.Run("returns stored state", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
			Return([]map[string]interface{}{{
				"id": testStateID, "store_id": []byte(StoreIDConfigDB), "metadata_version": int64(1),
				"completed_level": "1.0.0", "target_level": nil, "stage": "complete", "status": "clean",
				"active_run_id": nil, "failure_summary": nil, "updated_at": updatedAt,
			}}, nil).Once()

		metadataStore := newTestMetadataStore(t, dbClient)
		state, err := metadataStore.GetState(ctx, StoreIDConfigDB)

		require.NoError(t, err)
		require.NotNil(t, state)
		assert.Equal(t, StoreIDConfigDB, state.StoreID)
		assert.Equal(t, updatedAt, state.UpdatedAt)
	})

	t.Run("returns nil when absent", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDEntityDB)).
			Return([]map[string]interface{}{}, nil).Once()

		state, err := newTestMetadataStore(t, dbClient).GetState(ctx, StoreIDEntityDB)

		require.NoError(t, err)
		assert.Nil(t, state)
	})

	t.Run("rejects unknown store", func(t *testing.T) {
		state, err := newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t)).GetState(ctx, "unknown")

		require.Error(t, err)
		assert.Nil(t, state)
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("wraps query failure", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
			Return(nil, errors.New("database unavailable")).Once()

		state, err := newTestMetadataStore(t, dbClient).GetState(ctx, StoreIDConfigDB)

		require.Error(t, err)
		assert.Nil(t, state)
		assert.ErrorContains(t, err, "database unavailable")
	})

	t.Run("rejects duplicate rows", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
			Return([]map[string]interface{}{{}, {}}, nil).Once()

		_, err := newTestMetadataStore(t, dbClient).GetState(ctx, StoreIDConfigDB)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected one row")
	})

	t.Run("rejects malformed stored state", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		row := validStateRow(updatedAt)
		row["status"] = "unknown"
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
			Return([]map[string]interface{}{row}, nil).Once()

		_, err := newTestMetadataStore(t, dbClient).GetState(ctx, StoreIDConfigDB)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid stored migration state")
	})
}

func TestNewMetadataStore(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)

	metadataStore, err := NewMetadataStore(dbClient, DatabaseTypePostgres)

	require.NoError(t, err)
	assert.NotNil(t, metadataStore)

	missingClientStore, missingClientErr := NewMetadataStore(nil, DatabaseTypePostgres)
	require.Error(t, missingClientErr)
	assert.Nil(t, missingClientStore)

	unsupportedStore, unsupportedErr := NewMetadataStore(dbClient, "mysql")
	require.Error(t, unsupportedErr)
	assert.Nil(t, unsupportedStore)
	assert.ErrorContains(t, unsupportedErr, "unsupported database type")
}

func TestMetadataStoreDeploymentState(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()
	state := DeploymentState{
		ID: testProgressID, MigrationStateID: testStateID, DeploymentID: "deployment-a",
		CompletedLevel: "2.0.0", UpdatedAt: updatedAt,
	}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("ExecuteContext", mock.Anything, queryUpsertDeploymentMigrationState,
		state.ID, state.MigrationStateID, state.DeploymentID, state.CompletedLevel, state.UpdatedAt).
		Return(int64(1), nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryGetDeploymentMigrationState,
		state.DeploymentID, state.MigrationStateID).Return([]map[string]interface{}{{
		"id": state.ID, "migration_state_id": state.MigrationStateID,
		"deployment_id": state.DeploymentID, "completed_level": state.CompletedLevel,
		"updated_at": state.UpdatedAt,
	}}, nil).Once()
	store := newTestMetadataStore(t, dbClient)

	require.NoError(t, store.UpsertDeploymentState(ctx, state))
	stored, err := store.GetDeploymentState(ctx, state.MigrationStateID, state.DeploymentID)

	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, state, *stored)
}

func TestMetadataStoreMigrationRunTargets(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()
	dbClient := providermock.NewDBClientInterfaceMock(t)
	for _, deploymentID := range []string{"deployment-a", "deployment-b"} {
		dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationRunTarget,
			testRunID, deploymentID, updatedAt).Return(int64(1), nil).Once()
	}
	dbClient.On("QueryContext", mock.Anything, queryListMigrationRunTargets, testRunID).
		Return([]map[string]interface{}{
			{"deployment_id": "deployment-a"}, {"deployment_id": []byte("deployment-b")},
		}, nil).Once()
	store := newTestMetadataStore(t, dbClient)

	require.NoError(t, store.RecordRunTargets(
		ctx, testRunID, []string{"deployment-a", "deployment-b"}, updatedAt))
	targets, err := store.ListRunTargets(ctx, testRunID)

	require.NoError(t, err)
	assert.Equal(t, []string{"deployment-a", "deployment-b"}, targets)
}

func newTestMetadataStore(
	t *testing.T,
	dbClient *providermock.DBClientInterfaceMock,
) *MetadataStore {
	t.Helper()
	metadataStore, err := NewMetadataStore(dbClient, DatabaseTypePostgres)
	require.NoError(t, err)
	return metadataStore
}

func TestMetadataStoreWritesState(t *testing.T) {
	ctx := context.Background()

	t.Run("inserts validated state", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		state := validState()
		dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationState,
			state.ID, string(state.StoreID), state.MetadataVersion, state.CompletedLevel,
			nil, string(state.Stage), string(state.Status), nil, nil, state.UpdatedAt).Return(int64(1), nil).Once()

		err := newTestMetadataStore(t, dbClient).InsertState(ctx, state)

		require.NoError(t, err)
	})

	t.Run("validates before insert", func(t *testing.T) {
		state := validState()
		state.ID = "invalid"

		err := newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t)).InsertState(ctx, state)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "state ID")
	})

	t.Run("requires one inserted row", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		state := validState()
		dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationState, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything).Return(int64(0), nil).Once()

		err := newTestMetadataStore(t, dbClient).InsertState(ctx, state)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected one affected row")
	})

	t.Run("updates mutable state", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		state := validState()
		state.Status = StatusInProgress
		state.TargetLevel = "1.1.0"
		state.Stage = MigrationStageExpanding
		state.ActiveRunID = testRunID
		dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationState,
			state.MetadataVersion, state.CompletedLevel, state.TargetLevel, string(state.Stage), string(state.Status),
			state.ActiveRunID, nil, state.UpdatedAt, state.ID, string(state.StoreID)).Return(int64(1), nil).Once()

		err := newTestMetadataStore(t, dbClient).UpdateState(ctx, state)

		require.NoError(t, err)
	})

	t.Run("wraps update failure", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		state := validState()
		dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationState,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(int64(0), errors.New("write failed")).Once()

		err := newTestMetadataStore(t, dbClient).UpdateState(ctx, state)

		require.Error(t, err)
		assert.ErrorContains(t, err, "write failed")
	})
}

func TestMetadataStoreListProgress(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()

	t.Run("returns validated progress", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
			Return([]map[string]interface{}{validProgressRow(updatedAt)}, nil).Once()

		progress, err := newTestMetadataStore(t, dbClient).ListProgress(ctx, testStateID, testRunID)

		require.NoError(t, err)
		require.Len(t, progress, 1)
		assert.Equal(t, "1.1.0/add-session-subject", progress[0].OperationID)
		assert.Equal(t, "deployment-a", progress[0].ExecutionTargetID)
		assert.JSONEq(t, `{"lastID":"0199a123-4567-7abc-8def-0123456789ab"}`,
			string(progress[0].CheckpointData))
	})

	t.Run("rejects invalid identifiers", func(t *testing.T) {
		metadataStore := newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t))

		_, stateErr := metadataStore.ListProgress(ctx, "invalid", testRunID)
		_, runErr := metadataStore.ListProgress(ctx, testStateID, "invalid")

		require.Error(t, stateErr)
		require.Error(t, runErr)
	})

	t.Run("wraps query failure", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
			Return(nil, errors.New("read failed")).Once()

		_, err := newTestMetadataStore(t, dbClient).ListProgress(ctx, testStateID, testRunID)

		require.Error(t, err)
		assert.ErrorContains(t, err, "read failed")
	})

	t.Run("rejects malformed progress", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		row := validProgressRow(updatedAt)
		delete(row, "operation_id")
		dbClient.On("QueryContext", mock.Anything, queryListMigrationProgress, testStateID, testRunID).
			Return([]map[string]interface{}{row}, nil).Once()

		_, err := newTestMetadataStore(t, dbClient).ListProgress(ctx, testStateID, testRunID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation_id")
	})
}

func TestMetadataStoreWritesProgress(t *testing.T) {
	ctx := context.Background()

	t.Run("inserts validated progress", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		progress := validProgress()
		dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationProgress,
			progress.ID, progress.MigrationStateID, progress.RunID, progress.TargetLevel,
			progress.OperationID, progress.OperationOrder, progress.ExecutionTargetID, string(progress.Status),
			string(progress.CheckpointData), progress.UpdatedAt).Return(int64(1), nil).Once()

		err := newTestMetadataStore(t, dbClient).InsertProgress(ctx, progress)

		require.NoError(t, err)
	})

	t.Run("validates before insert", func(t *testing.T) {
		progress := validProgress()
		progress.OperationID = ""

		err := newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t)).InsertProgress(ctx, progress)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation ID")
	})

	t.Run("updates mutable progress", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		progress := validProgress()
		progress.Status = StatusCompleted
		dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
			string(progress.Status), string(progress.CheckpointData), progress.UpdatedAt,
			progress.ID, progress.MigrationStateID, progress.RunID).Return(int64(1), nil).Once()

		err := newTestMetadataStore(t, dbClient).UpdateProgress(ctx, progress)

		require.NoError(t, err)
	})

	t.Run("requires one updated row", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		progress := validProgress()
		dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationProgress,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(int64(0), nil).Once()

		err := newTestMetadataStore(t, dbClient).UpdateProgress(ctx, progress)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected one affected row")
	})
}

func validStateRow(updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id": testStateID, "store_id": string(StoreIDConfigDB), "metadata_version": int64(1),
		"completed_level": "1.0.0", "target_level": nil, "stage": "complete", "status": "clean",
		"active_run_id": nil, "failure_summary": nil, "updated_at": updatedAt,
	}
}

func validProgressRow(updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id": testProgressID, "migration_state_id": testStateID, "run_id": testRunID,
		"target_level": "1.1.0", "operation_id": "1.1.0/add-session-subject",
		"operation_order": int32(0), "execution_target_id": "deployment-a", "status": "in_progress",
		"checkpoint_data": []byte(`{"lastID":"0199a123-4567-7abc-8def-0123456789ab"}`),
		"updated_at":      updatedAt,
	}
}
