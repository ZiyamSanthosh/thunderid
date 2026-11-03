// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
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

func TestMetadataStoreMigrationRun(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()
	failure := MigrationFailure{
		RunID: testRunID, StoreID: StoreIDConfigDB,
		ForwardOperationID: "2.0.0/configdb/backfill", CurrentOperationID: "2.0.0/configdb/backfill",
		ExecutionTargetID: "deployment-a", ExecutionDirection: ExecutionDirectionForward,
		FailureSummary: "batch failed", UpdatedAt: updatedAt,
	}
	run := MigrationRun{
		ID: testRunID, Mode: MigrationRunModeForward, Phase: OperationPhaseMigrate,
		Selection: MigrationSelection{DeploymentIDs: []string{"deployment-a", "deployment-b"}},
		Failure:   &failure, UpdatedAt: updatedAt,
	}
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationRun,
		run.ID, string(run.Mode), string(run.Phase), nil, `["deployment-a","deployment-b"]`,
		string(failure.StoreID), string(failure.ForwardOperationID), string(failure.CurrentOperationID),
		failure.ExecutionTargetID, string(failure.ExecutionDirection), false, failure.FailureSummary, updatedAt).
		Return(int64(1), nil).Once()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationRun, testRunID).
		Return([]map[string]interface{}{migrationRunRow(run)}, nil).Once()
	nextRun := run
	nextRun.Mode = MigrationRunModeRollback
	nextRun.RollbackScope = RollbackScopePhase
	nextRun.UpdatedAt = updatedAt.Add(time.Minute)
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationRun,
		string(nextRun.Mode), string(nextRun.Phase), string(nextRun.RollbackScope), `["deployment-a","deployment-b"]`,
		string(failure.StoreID), string(failure.ForwardOperationID), string(failure.CurrentOperationID),
		failure.ExecutionTargetID, string(failure.ExecutionDirection), false, failure.FailureSummary,
		nextRun.UpdatedAt, nextRun.ID, run.UpdatedAt).Return(int64(1), nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryDeleteInactiveMigrationRun, testRunID).
		Return(int64(1), nil).Once()
	store := newTestMetadataStore(t, dbClient)

	require.NoError(t, store.InsertRun(ctx, run))
	stored, err := store.GetRun(ctx, testRunID)

	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, run, *stored)
	require.NoError(t, store.UpdateRun(ctx, run, nextRun))
	require.NoError(t, store.DeleteRunIfInactive(ctx, testRunID))
}

func migrationRunRow(run MigrationRun) map[string]interface{} {
	row := map[string]interface{}{
		"id": run.ID, "mode": string(run.Mode), "phase": string(run.Phase),
		"rollback_scope":  nullableString(string(run.RollbackScope)),
		"selection_data":  string(mustJSON(run.Selection.DeploymentIDs)),
		"failed_store_id": nil, "forward_operation_id": nil, "current_operation_id": nil,
		"execution_target_id": nil, "execution_direction": nil, "rolled_back": false,
		"failure_summary": nil, "updated_at": run.UpdatedAt,
	}
	if run.Failure != nil {
		row["failed_store_id"] = string(run.Failure.StoreID)
		row["forward_operation_id"] = string(run.Failure.ForwardOperationID)
		row["current_operation_id"] = string(run.Failure.CurrentOperationID)
		row["execution_target_id"] = run.Failure.ExecutionTargetID
		row["execution_direction"] = string(run.Failure.ExecutionDirection)
		row["rolled_back"] = run.Failure.RolledBack
		row["failure_summary"] = run.Failure.FailureSummary
	}
	return row
}

func mustJSON(value interface{}) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func newTestMetadataStore(
	t *testing.T,
	dbClient *providermock.DBClientInterfaceMock,
) *MetadataStore {
	t.Helper()
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationRun,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).Return(int64(1), nil).Maybe()
	dbClient.On("ExecuteContext", mock.Anything, queryDeleteInactiveMigrationRun, mock.Anything).
		Return(int64(1), nil).Maybe()
	dbClient.On("ExecuteContext", mock.Anything, queryUpdateMigrationRun,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(int64(1), nil).Maybe()
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

func TestMetadataStoreGetProgress(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Now().UTC()
	operationID := OperationID("1.1.0/add-session-subject")

	t.Run("returns matching progress", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationProgress,
			testStateID, "1.1.0", string(operationID), "deployment-a").
			Return([]map[string]interface{}{validProgressRow(updatedAt)}, nil).Once()

		progress, err := newTestMetadataStore(t, dbClient).GetProgress(
			ctx, testStateID, "1.1.0", operationID, "deployment-a")

		require.NoError(t, err)
		require.NotNil(t, progress)
		assert.Equal(t, string(operationID), progress.OperationID)
	})

	t.Run("returns nil when progress is absent", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationProgress,
			testStateID, "1.1.0", string(operationID), "").Return(nil, nil).Once()

		progress, err := newTestMetadataStore(t, dbClient).GetProgress(
			ctx, testStateID, "1.1.0", operationID, "")

		require.NoError(t, err)
		assert.Nil(t, progress)
	})

	t.Run("validates lookup identity", func(t *testing.T) {
		store := newTestMetadataStore(t, providermock.NewDBClientInterfaceMock(t))

		_, stateErr := store.GetProgress(ctx, "invalid", "1.1.0", operationID, "")
		_, levelErr := store.GetProgress(ctx, testStateID, "invalid", operationID, "")
		_, operationErr := store.GetProgress(ctx, testStateID, "1.1.0", "", "")

		require.Error(t, stateErr)
		require.Error(t, levelErr)
		require.Error(t, operationErr)
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

func TestMetadataStoreDeletesProgressForRolledBackRun(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("ExecuteContext", mock.Anything, queryDeleteMigrationProgressForRun,
		testStateID, testRunID).Return(int64(2), nil).Once()

	err := newTestMetadataStore(t, dbClient).DeleteProgressForRun(
		context.Background(), testStateID, testRunID)

	require.NoError(t, err)
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
