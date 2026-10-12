// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/database/provider"
)

var (
	queryGetMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-01",
		PostgresQuery: `SELECT ID, STORE_ID, METADATA_VERSION, COMPLETED_LEVEL, TARGET_LEVEL, STAGE, STATUS,
            ACTIVE_RUN_ID, FAILURE_SUMMARY, UPDATED_AT
            FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
	}
	queryInsertMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-02",
		PostgresQuery: `INSERT INTO "MIGRATION_STATE" (ID, STORE_ID, METADATA_VERSION, COMPLETED_LEVEL,
            TARGET_LEVEL, STAGE, STATUS, ACTIVE_RUN_ID, FAILURE_SUMMARY, UPDATED_AT)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
	}
	queryListMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-04",
		PostgresQuery: `SELECT ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
			EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND RUN_ID = $2
			ORDER BY OPERATION_ORDER, OPERATION_ID, EXECUTION_TARGET_ID`,
	}
	queryGetMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-20",
		PostgresQuery: `SELECT ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
			EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND TARGET_LEVEL = $2 AND OPERATION_ID = $3
				AND EXECUTION_TARGET_ID = $4`,
	}
	queryInsertMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-05",
		PostgresQuery: `INSERT INTO "MIGRATION_PROGRESS" (ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL,
			OPERATION_ID, OPERATION_ORDER, EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
	}
	queryUpdateMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-06",
		PostgresQuery: `UPDATE "MIGRATION_PROGRESS" SET STATUS = $1, CHECKPOINT_DATA = $2, UPDATED_AT = $3
			WHERE ID = $4 AND MIGRATION_STATE_ID = $5 AND RUN_ID = $6`,
	}
	queryReserveMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-09",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET TARGET_LEVEL = $1, STAGE = $2, STATUS = $3, ACTIVE_RUN_ID = $4,
                FAILURE_SUMMARY = NULL, UPDATED_AT = $5
            WHERE ID = $6 AND STORE_ID = $7 AND METADATA_VERSION = $8 AND COMPLETED_LEVEL = $9
            AND STAGE = $10 AND STATUS = $11 AND TARGET_LEVEL IS NULL AND ACTIVE_RUN_ID IS NULL`,
	}
	queryFinalizeMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-10",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET COMPLETED_LEVEL = $1, TARGET_LEVEL = NULL, STAGE = $2, STATUS = $3,
                ACTIVE_RUN_ID = NULL, FAILURE_SUMMARY = NULL, UPDATED_AT = $4
            WHERE ID = $5 AND STORE_ID = $6 AND METADATA_VERSION = $7 AND COMPLETED_LEVEL = $8
            AND TARGET_LEVEL = $1 AND STAGE = $9 AND STATUS = $10 AND ACTIVE_RUN_ID = $11`,
	}
	queryCancelMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-11",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET TARGET_LEVEL = NULL, STAGE = $1, STATUS = $2, ACTIVE_RUN_ID = NULL,
                FAILURE_SUMMARY = NULL, UPDATED_AT = $3
            WHERE ID = $4 AND STORE_ID = $5 AND METADATA_VERSION = $6 AND COMPLETED_LEVEL = $7
            AND TARGET_LEVEL = $8 AND STAGE = $9 AND STATUS = $10 AND ACTIVE_RUN_ID = $11
            AND NOT EXISTS (
                SELECT 1 FROM "MIGRATION_PROGRESS" WHERE MIGRATION_STATE_ID = $4 AND RUN_ID = $11
            )`,
	}
	queryFailMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-12",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET STATUS = $1, FAILURE_SUMMARY = $2, UPDATED_AT = $3
            WHERE ID = $4 AND STORE_ID = $5 AND METADATA_VERSION = $6 AND COMPLETED_LEVEL = $7
            AND TARGET_LEVEL = $8 AND STAGE = $9 AND STATUS = $10 AND ACTIVE_RUN_ID = $11`,
	}
	queryResumeMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-13",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET STATUS = $1, FAILURE_SUMMARY = NULL, UPDATED_AT = $2
            WHERE ID = $3 AND STORE_ID = $4 AND METADATA_VERSION = $5 AND COMPLETED_LEVEL = $6
			AND TARGET_LEVEL = $7 AND STAGE = $8 AND STATUS = $9 AND ACTIVE_RUN_ID = $10
			AND FAILURE_SUMMARY = $11 AND UPDATED_AT = $12`,
	}
	queryTransitionMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-14",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET COMPLETED_LEVEL = $1, TARGET_LEVEL = $2, STAGE = $3, STATUS = $4,
                ACTIVE_RUN_ID = $5, FAILURE_SUMMARY = $6, UPDATED_AT = $7
            WHERE ID = $8 AND STORE_ID = $9 AND METADATA_VERSION = $10 AND COMPLETED_LEVEL = $11
            AND TARGET_LEVEL IS NOT DISTINCT FROM $12 AND STAGE = $13 AND STATUS = $14
            AND ACTIVE_RUN_ID IS NOT DISTINCT FROM $15
            AND FAILURE_SUMMARY IS NOT DISTINCT FROM $16 AND UPDATED_AT = $17`,
	}
	queryCancelMigrationPhase = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-15",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET TARGET_LEVEL = $1, STAGE = $2, STATUS = $3, ACTIVE_RUN_ID = NULL,
                FAILURE_SUMMARY = NULL, UPDATED_AT = $4
            WHERE ID = $5 AND STORE_ID = $6 AND METADATA_VERSION = $7 AND COMPLETED_LEVEL = $8
            AND TARGET_LEVEL = $9 AND STAGE = $10 AND STATUS = $11 AND ACTIVE_RUN_ID = $12
            AND NOT EXISTS (
                SELECT 1 FROM "MIGRATION_PROGRESS" WHERE MIGRATION_STATE_ID = $5 AND RUN_ID = $12
            )`,
	}
	queryUpsertDeploymentMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-16",
		PostgresQuery: `INSERT INTO "MIGRATION_DEPLOYMENT_STATE"
			(ID, MIGRATION_STATE_ID, DEPLOYMENT_ID, COMPLETED_LEVEL, UPDATED_AT)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (DEPLOYMENT_ID, MIGRATION_STATE_ID) DO UPDATE
			SET COMPLETED_LEVEL = EXCLUDED.COMPLETED_LEVEL, UPDATED_AT = EXCLUDED.UPDATED_AT`,
	}
	queryGetDeploymentMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-17",
		PostgresQuery: `SELECT ID, MIGRATION_STATE_ID, DEPLOYMENT_ID, COMPLETED_LEVEL, UPDATED_AT
			FROM "MIGRATION_DEPLOYMENT_STATE"
			WHERE DEPLOYMENT_ID = $1 AND MIGRATION_STATE_ID = $2`,
	}
	queryInsertMigrationRun = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-18",
		PostgresQuery: `INSERT INTO "MIGRATION_RUN" (ID, MODE, PHASE, ROLLBACK_SCOPE, SELECTION_DATA,
			FAILED_STORE_ID, FORWARD_OPERATION_ID, CURRENT_OPERATION_ID, EXECUTION_TARGET_ID,
			EXECUTION_DIRECTION, ROLLED_BACK, FAILURE_SUMMARY, UPDATED_AT)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
	}
	queryGetMigrationRun = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-19",
		PostgresQuery: `SELECT ID, MODE, PHASE, ROLLBACK_SCOPE, SELECTION_DATA, FAILED_STORE_ID,
			FORWARD_OPERATION_ID, CURRENT_OPERATION_ID, EXECUTION_TARGET_ID, EXECUTION_DIRECTION,
			ROLLED_BACK, FAILURE_SUMMARY, UPDATED_AT FROM "MIGRATION_RUN" WHERE ID = $1`,
	}
	queryDeleteMigrationProgressForRun = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-21",
		PostgresQuery: `DELETE FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND RUN_ID = $2`,
	}
	queryListMigrationProgressForTarget = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-22",
		PostgresQuery: `SELECT ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
			EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND TARGET_LEVEL = $2
			ORDER BY OPERATION_ORDER, OPERATION_ID, EXECUTION_TARGET_ID`,
	}
	queryDeleteMigrationProgressForOperation = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-23",
		PostgresQuery: `DELETE FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND TARGET_LEVEL = $2 AND OPERATION_ID = $3
				AND EXECUTION_TARGET_ID = $4`,
	}
	queryDeleteMigrationProgressForTarget = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-24",
		PostgresQuery: `DELETE FROM "MIGRATION_PROGRESS"
			WHERE MIGRATION_STATE_ID = $1 AND TARGET_LEVEL = $2`,
	}
	queryUpdateMigrationRun = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-25",
		PostgresQuery: `UPDATE "MIGRATION_RUN" SET MODE = $1, PHASE = $2, ROLLBACK_SCOPE = $3,
			SELECTION_DATA = $4, FAILED_STORE_ID = $5, FORWARD_OPERATION_ID = $6,
			CURRENT_OPERATION_ID = $7, EXECUTION_TARGET_ID = $8, EXECUTION_DIRECTION = $9,
			ROLLED_BACK = $10, FAILURE_SUMMARY = $11, UPDATED_AT = $12
			WHERE ID = $13 AND UPDATED_AT = $14`,
	}
	queryDeleteInactiveMigrationRun = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-27",
		PostgresQuery: `DELETE FROM "MIGRATION_RUN" WHERE ID = $1 AND NOT EXISTS
			(SELECT 1 FROM "MIGRATION_STATE" WHERE ACTIVE_RUN_ID = $1)`,
	}
	queryDeleteDeploymentStatesForLevel = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-29",
		PostgresQuery: `DELETE FROM "MIGRATION_DEPLOYMENT_STATE"
			WHERE MIGRATION_STATE_ID = $1 AND COMPLETED_LEVEL = $2`,
	}
	queryDeleteDeploymentState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-30",
		PostgresQuery: `DELETE FROM "MIGRATION_DEPLOYMENT_STATE"
			WHERE MIGRATION_STATE_ID = $1 AND DEPLOYMENT_ID = $2 AND COMPLETED_LEVEL = $3`,
	}
)

// DatabaseType identifies a database implementation supported by the migration engine.
type DatabaseType string

const (
	// DatabaseTypePostgres identifies PostgreSQL migration metadata behavior.
	DatabaseTypePostgres DatabaseType = "postgres"
)

type metadataQuerySet struct {
	getState                       dbmodel.DBQuery
	insertState                    dbmodel.DBQuery
	listProgress                   dbmodel.DBQuery
	getProgress                    dbmodel.DBQuery
	insertProgress                 dbmodel.DBQuery
	updateProgress                 dbmodel.DBQuery
	reserveState                   dbmodel.DBQuery
	finalizeState                  dbmodel.DBQuery
	cancelState                    dbmodel.DBQuery
	failState                      dbmodel.DBQuery
	resumeState                    dbmodel.DBQuery
	transitionState                dbmodel.DBQuery
	cancelPhase                    dbmodel.DBQuery
	upsertDeploymentState          dbmodel.DBQuery
	getDeploymentState             dbmodel.DBQuery
	insertRun                      dbmodel.DBQuery
	getRun                         dbmodel.DBQuery
	updateRun                      dbmodel.DBQuery
	deleteInactiveRun              dbmodel.DBQuery
	listProgressForTarget          dbmodel.DBQuery
	deleteProgressForOperation     dbmodel.DBQuery
	deleteProgressForTarget        dbmodel.DBQuery
	deleteDeploymentStatesForLevel dbmodel.DBQuery
	deleteDeploymentState          dbmodel.DBQuery
	inspectSchema                  dbmodel.DBQuery
	createSchema                   dbmodel.DBQuery
}

var migrationMetadataQueries = metadataQuerySet{
	getState: queryGetMigrationState, insertState: queryInsertMigrationState,
	listProgress:   queryListMigrationProgress,
	getProgress:    queryGetMigrationProgress,
	insertProgress: queryInsertMigrationProgress, updateProgress: queryUpdateMigrationProgress,
	reserveState: queryReserveMigrationState, finalizeState: queryFinalizeMigrationState,
	cancelState: queryCancelMigrationState, failState: queryFailMigrationState,
	resumeState:                    queryResumeMigrationState,
	transitionState:                queryTransitionMigrationState,
	cancelPhase:                    queryCancelMigrationPhase,
	upsertDeploymentState:          queryUpsertDeploymentMigrationState,
	getDeploymentState:             queryGetDeploymentMigrationState,
	insertRun:                      queryInsertMigrationRun,
	getRun:                         queryGetMigrationRun,
	updateRun:                      queryUpdateMigrationRun,
	deleteInactiveRun:              queryDeleteInactiveMigrationRun,
	listProgressForTarget:          queryListMigrationProgressForTarget,
	deleteProgressForOperation:     queryDeleteMigrationProgressForOperation,
	deleteProgressForTarget:        queryDeleteMigrationProgressForTarget,
	deleteDeploymentStatesForLevel: queryDeleteDeploymentStatesForLevel,
	deleteDeploymentState:          queryDeleteDeploymentState,
	inspectSchema:                  queryInspectMetadataSchema, createSchema: queryCreateMetadataSchema,
}

// GetProgress returns the durable execution record for one operation target, if present.
func (r *MetadataStore) GetProgress(
	ctx context.Context,
	migrationStateID string,
	targetLevel string,
	operationID OperationID,
	executionTargetID string,
) (*Progress, error) {
	if !isUUIDv7(migrationStateID) {
		return nil, errors.New("get migration progress: state ID must be a canonical UUIDv7")
	}
	if !isCanonicalVersion(targetLevel) {
		return nil, errors.New("get migration progress: target level must be a canonical semantic version")
	}
	if strings.TrimSpace(string(operationID)) == "" {
		return nil, errors.New("get migration progress: operation ID is required")
	}

	rows, err := r.dbClient.QueryContext(ctx, r.queries.getProgress,
		migrationStateID, targetLevel, string(operationID), executionTargetID)
	if err != nil {
		return nil, fmt.Errorf("get migration progress for operation %q: %w", operationID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("get migration progress for operation %q: expected at most one row, got %d",
			operationID, len(rows))
	}
	progress, err := progressFromRow(rows[0])
	if err != nil {
		return nil, fmt.Errorf("get migration progress for operation %q: %w", operationID, err)
	}
	return &progress, nil
}

// InsertRun creates the durable context for one active migration run.
func (r *MetadataStore) InsertRun(ctx context.Context, run MigrationRun) error {
	if err := run.Validate(); err != nil {
		return fmt.Errorf("insert migration run: %w", err)
	}
	selectionData, err := json.Marshal(append([]string{}, run.Selection.DeploymentIDs...))
	if err != nil {
		return fmt.Errorf("insert migration run: encode selection: %w", err)
	}
	args := migrationRunMutableArgs(run, selectionData)
	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.insertRun,
		append([]interface{}{run.ID}, args...)...)
	if err != nil {
		return fmt.Errorf("insert migration run %q: %w", run.ID, err)
	}
	return requireOneRow("insert migration run", rowsAffected)
}

// GetRun returns the active migration run context, if present.
func (r *MetadataStore) GetRun(ctx context.Context, runID string) (*MigrationRun, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("get migration run: run ID must be a canonical UUIDv7")
	}
	rows, err := r.dbClient.QueryContext(ctx, r.queries.getRun, runID)
	if err != nil {
		return nil, fmt.Errorf("get migration run %q: %w", runID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("get migration run %q: expected at most one row, got %d", runID, len(rows))
	}
	run, err := migrationRunFromRow(rows[0])
	if err != nil {
		return nil, fmt.Errorf("get migration run %q: %w", runID, err)
	}
	return &run, nil
}

// UpdateRun conditionally replaces mutable active-run context.
func (r *MetadataStore) UpdateRun(ctx context.Context, current MigrationRun, next MigrationRun) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("update migration run: invalid current run: %w", err)
	}
	if err := next.Validate(); err != nil {
		return fmt.Errorf("update migration run: invalid next run: %w", err)
	}
	if current.ID != next.ID {
		return errors.New("update migration run: run ID cannot change")
	}
	selectionData, err := json.Marshal(append([]string{}, next.Selection.DeploymentIDs...))
	if err != nil {
		return fmt.Errorf("update migration run: encode selection: %w", err)
	}
	args := migrationRunMutableArgs(next, selectionData)
	args = append(args, next.ID, current.UpdatedAt)
	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.updateRun, args...)
	if err != nil {
		return fmt.Errorf("update migration run %q: %w", next.ID, err)
	}
	return requireOneRow("update migration run", rowsAffected)
}

// DeleteRunIfInactive removes a run only after every store has released it.
func (r *MetadataStore) DeleteRunIfInactive(ctx context.Context, runID string) error {
	if !isUUIDv7(runID) {
		return errors.New("delete inactive migration run: run ID must be a canonical UUIDv7")
	}
	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.deleteInactiveRun, runID)
	if err != nil {
		return fmt.Errorf("delete inactive migration run %q: %w", runID, err)
	}
	if rowsAffected < 0 || rowsAffected > 1 {
		return fmt.Errorf("delete inactive migration run %q: expected at most one row, got %d", runID, rowsAffected)
	}
	return nil
}

// UpsertDeploymentState records the release level completed by one deployment in a logical store.
func (r *MetadataStore) UpsertDeploymentState(ctx context.Context, state DeploymentState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("upsert deployment migration state: %w", err)
	}
	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.upsertDeploymentState,
		state.ID, state.MigrationStateID, state.DeploymentID, state.CompletedLevel, state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert deployment migration state for deployment %q: %w", state.DeploymentID, err)
	}
	return requireOneRow("upsert deployment migration state", rowsAffected)
}

// GetDeploymentState returns a deployment completion marker, or nil when it has not been migrated.
func (r *MetadataStore) GetDeploymentState(
	ctx context.Context,
	migrationStateID string,
	deploymentID string,
) (*DeploymentState, error) {
	if !isUUIDv7(migrationStateID) {
		return nil, errors.New("read deployment migration state: parent ID must be a canonical UUIDv7")
	}
	if strings.TrimSpace(deploymentID) == "" || deploymentID != strings.TrimSpace(deploymentID) {
		return nil, errors.New("read deployment migration state: deployment ID is invalid")
	}
	rows, err := r.dbClient.QueryContext(ctx, r.queries.getDeploymentState, deploymentID, migrationStateID)
	if err != nil {
		return nil, fmt.Errorf("read deployment migration state for deployment %q: %w", deploymentID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("read deployment migration state for deployment %q: expected one row, got %d",
			deploymentID, len(rows))
	}
	state, err := deploymentStateFromRow(rows[0])
	if err != nil {
		return nil, fmt.Errorf("read deployment migration state for deployment %q: %w", deploymentID, err)
	}
	return &state, nil
}

// CancelPhaseState conditionally restores a phase boundary when its run has no operation progress.
func (r *MetadataStore) CancelPhaseState(
	ctx context.Context,
	current State,
	next State,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("cancel migration phase: invalid current state: %w", err)
	}
	if err := next.Validate(); err != nil {
		return false, fmt.Errorf("cancel migration phase: invalid next state: %w", err)
	}
	if current.ID != next.ID || current.StoreID != next.StoreID ||
		current.MetadataVersion != next.MetadataVersion || current.CompletedLevel != next.CompletedLevel {
		return false, errors.New("cancel migration phase: state identity and completed level cannot change")
	}
	if current.Status != StatusInProgress || next.Status != StatusCompleted || next.ActiveRunID != "" {
		return false, errors.New("cancel migration phase: invalid phase cancellation transition")
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.cancelPhase,
		nullableString(next.TargetLevel), string(next.Stage), string(next.Status), next.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		current.TargetLevel, string(current.Stage), string(current.Status), current.ActiveRunID)
	if err != nil {
		return false, fmt.Errorf("cancel migration phase for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("cancel migration phase for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// TransitionState conditionally replaces one validated state with another state for the same store.
func (r *MetadataStore) TransitionState(
	ctx context.Context,
	current State,
	next State,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("transition migration state: invalid current state: %w", err)
	}
	if err := next.Validate(); err != nil {
		return false, fmt.Errorf("transition migration state: invalid next state: %w", err)
	}
	if current.ID != next.ID || current.StoreID != next.StoreID ||
		current.MetadataVersion != next.MetadataVersion {
		return false, errors.New("transition migration state: state identity cannot change")
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.transitionState,
		next.CompletedLevel, nullableString(next.TargetLevel), string(next.Stage), string(next.Status),
		nullableString(next.ActiveRunID), nullableString(next.FailureSummary), next.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		nullableString(current.TargetLevel), string(current.Stage), string(current.Status),
		nullableString(current.ActiveRunID), nullableString(current.FailureSummary), current.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("transition migration state for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("transition migration state for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// ReserveState conditionally assigns one clean source-level store to a migration run.
func (r *MetadataStore) ReserveState(
	ctx context.Context,
	current State,
	targetLevel string,
	stage MigrationStage,
	runID string,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("reserve migration state: invalid current state: %w", err)
	}
	reserved := current
	reserved.TargetLevel = targetLevel
	reserved.Stage = stage
	reserved.Status = StatusInProgress
	reserved.ActiveRunID = runID
	reserved.FailureSummary = ""
	reserved.UpdatedAt = updatedAt
	if err := reserved.Validate(); err != nil {
		return false, fmt.Errorf("reserve migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.reserveState,
		reserved.TargetLevel, string(reserved.Stage), string(reserved.Status), reserved.ActiveRunID,
		reserved.UpdatedAt, reserved.ID, string(reserved.StoreID), reserved.MetadataVersion,
		reserved.CompletedLevel, string(MigrationStageComplete), string(StatusClean))
	if err != nil {
		return false, fmt.Errorf("reserve migration state for store %q: %w", reserved.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("reserve migration state for store %q: expected at most one row, got %d",
			reserved.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// FinalizeState conditionally advances a store reserved by a migration run to its target level.
func (r *MetadataStore) FinalizeState(
	ctx context.Context,
	current State,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("finalize migration state: invalid current state: %w", err)
	}
	if current.Status != StatusInProgress {
		return false, fmt.Errorf("finalize migration state for store %q: status must be %q",
			current.StoreID, StatusInProgress)
	}
	completed := current
	completed.CompletedLevel = current.TargetLevel
	completed.TargetLevel = ""
	completed.Stage = MigrationStageComplete
	completed.Status = StatusClean
	completed.ActiveRunID = ""
	completed.FailureSummary = ""
	completed.UpdatedAt = updatedAt
	if err := completed.Validate(); err != nil {
		return false, fmt.Errorf("finalize migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.finalizeState,
		current.TargetLevel, string(MigrationStageComplete), string(StatusClean), completed.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		string(current.Stage), string(StatusInProgress), current.ActiveRunID)
	if err != nil {
		return false, fmt.Errorf("finalize migration state for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("finalize migration state for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// CancelState conditionally releases a reservation that has no operation progress.
func (r *MetadataStore) CancelState(
	ctx context.Context,
	current State,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("cancel migration state: invalid current state: %w", err)
	}
	if current.Status != StatusInProgress {
		return false, fmt.Errorf("cancel migration state for store %q: status must be %q",
			current.StoreID, StatusInProgress)
	}
	cancelled := current
	cancelled.TargetLevel = ""
	cancelled.Stage = MigrationStageComplete
	cancelled.Status = StatusClean
	cancelled.ActiveRunID = ""
	cancelled.FailureSummary = ""
	cancelled.UpdatedAt = updatedAt
	if err := cancelled.Validate(); err != nil {
		return false, fmt.Errorf("cancel migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.cancelState,
		string(MigrationStageComplete), string(StatusClean), cancelled.UpdatedAt, current.ID,
		string(current.StoreID), current.MetadataVersion, current.CompletedLevel, current.TargetLevel,
		string(current.Stage), string(StatusInProgress), current.ActiveRunID)
	if err != nil {
		return false, fmt.Errorf("cancel migration state for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("cancel migration state for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// FailState conditionally marks a store reserved by a migration run as failed.
func (r *MetadataStore) FailState(
	ctx context.Context,
	current State,
	failureSummary string,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("fail migration state: invalid current state: %w", err)
	}
	if current.Status != StatusInProgress {
		return false, fmt.Errorf("fail migration state for store %q: status must be %q",
			current.StoreID, StatusInProgress)
	}
	failed := current
	failed.Status = StatusFailed
	failed.FailureSummary = failureSummary
	failed.UpdatedAt = updatedAt
	if err := failed.Validate(); err != nil {
		return false, fmt.Errorf("fail migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.failState,
		string(StatusFailed), failed.FailureSummary, failed.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		current.TargetLevel, string(current.Stage), string(StatusInProgress), current.ActiveRunID)
	if err != nil {
		return false, fmt.Errorf("fail migration state for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("fail migration state for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

// ResumeState conditionally returns a failed store to its active atomic migration run.
func (r *MetadataStore) ResumeState(
	ctx context.Context,
	current State,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("resume migration state: invalid current state: %w", err)
	}
	if current.Status != StatusFailed {
		return false, fmt.Errorf("resume migration state for store %q: status must be %q",
			current.StoreID, StatusFailed)
	}
	resumed := current
	resumed.Status = StatusInProgress
	resumed.FailureSummary = ""
	resumed.UpdatedAt = updatedAt
	if err := resumed.Validate(); err != nil {
		return false, fmt.Errorf("resume migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.resumeState,
		string(StatusInProgress), resumed.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		current.TargetLevel, string(current.Stage), string(StatusFailed), current.ActiveRunID,
		current.FailureSummary, current.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("resume migration state for store %q: %w", current.StoreID, err)
	}
	if rowsAffected > 1 {
		return false, fmt.Errorf("resume migration state for store %q: expected at most one row, got %d",
			current.StoreID, rowsAffected)
	}
	return rowsAffected == 1, nil
}

var supportedMigrationDatabaseTypes = map[DatabaseType]struct{}{
	DatabaseTypePostgres: {},
}

// MetadataStore persists migration state in one physical database namespace.
type MetadataStore struct {
	dbClient provider.DBClientInterface
	queries  metadataQuerySet
}

// NewMetadataStore creates a metadata store using an explicitly registered database implementation.
func NewMetadataStore(
	dbClient provider.DBClientInterface,
	databaseType DatabaseType,
) (*MetadataStore, error) {
	if dbClient == nil {
		return nil, errors.New("create migration metadata store: database client is required")
	}
	_, supported := supportedMigrationDatabaseTypes[databaseType]
	if !supported {
		return nil, fmt.Errorf("create migration metadata store: unsupported database type %q", databaseType)
	}
	return &MetadataStore{dbClient: dbClient, queries: migrationMetadataQueries}, nil
}

// GetState returns the state for a logical store, or nil when it has not been initialized.
func (r *MetadataStore) GetState(ctx context.Context, storeID StoreID) (*State, error) {
	if !isSupportedStoreID(storeID) {
		return nil, fmt.Errorf("unsupported migration state store %q", storeID)
	}

	rows, err := r.dbClient.QueryContext(ctx, r.queries.getState, string(storeID))
	if err != nil {
		return nil, fmt.Errorf("read migration state for store %q: %w", storeID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("read migration state for store %q: expected one row, got %d", storeID, len(rows))
	}

	state, err := stateFromRow(rows[0])
	if err != nil {
		return nil, fmt.Errorf("read migration state for store %q: %w", storeID, err)
	}
	return &state, nil
}

// InsertState creates the first state row for a logical store.
func (r *MetadataStore) InsertState(ctx context.Context, state State) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("insert migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.insertState,
		state.ID, string(state.StoreID), state.MetadataVersion, state.CompletedLevel,
		nullableString(state.TargetLevel), string(state.Stage), string(state.Status),
		nullableString(state.ActiveRunID), nullableString(state.FailureSummary), state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert migration state for store %q: %w", state.StoreID, err)
	}
	return requireOneRow("insert migration state", rowsAffected)
}

// ListProgress returns ordered operation progress for one migration run.
func (r *MetadataStore) ListProgress(
	ctx context.Context,
	migrationStateID string,
	runID string,
) ([]Progress, error) {
	if !isUUIDv7(migrationStateID) {
		return nil, errors.New("list migration progress: state ID must be a canonical UUIDv7")
	}
	if !isUUIDv7(runID) {
		return nil, errors.New("list migration progress: run ID must be a canonical UUIDv7")
	}

	rows, err := r.dbClient.QueryContext(ctx, r.queries.listProgress, migrationStateID, runID)
	if err != nil {
		return nil, fmt.Errorf("list migration progress: %w", err)
	}

	progress := make([]Progress, 0, len(rows))
	for index, row := range rows {
		item, err := progressFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("list migration progress row %d: %w", index, err)
		}
		progress = append(progress, item)
	}
	return progress, nil
}

// ListProgressForTarget returns operation progress across all runs for one release target.
func (r *MetadataStore) ListProgressForTarget(
	ctx context.Context,
	migrationStateID string,
	targetLevel string,
) ([]Progress, error) {
	if !isUUIDv7(migrationStateID) {
		return nil, errors.New("list migration target progress: state ID must be a canonical UUIDv7")
	}
	if !isCanonicalVersion(targetLevel) {
		return nil, errors.New("list migration target progress: target level must be a canonical semantic version")
	}
	rows, err := r.dbClient.QueryContext(ctx, r.queries.listProgressForTarget, migrationStateID, targetLevel)
	if err != nil {
		return nil, fmt.Errorf("list migration target progress: %w", err)
	}
	progress := make([]Progress, 0, len(rows))
	for index, row := range rows {
		item, err := progressFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("list migration target progress row %d: %w", index, err)
		}
		progress = append(progress, item)
	}
	return progress, nil
}

// InsertProgress creates the durable record for a migration operation.
func (r *MetadataStore) InsertProgress(ctx context.Context, progress Progress) error {
	if err := progress.Validate(); err != nil {
		return fmt.Errorf("insert migration progress: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.insertProgress,
		progress.ID, progress.MigrationStateID, progress.RunID, progress.TargetLevel,
		progress.OperationID, progress.OperationOrder, progress.ExecutionTargetID, string(progress.Status),
		nullableCheckpoint(progress.CheckpointData), progress.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert migration progress for operation %q: %w", progress.OperationID, err)
	}
	return requireOneRow("insert migration progress", rowsAffected)
}

// UpdateProgress writes the mutable execution fields of an existing operation record.
func (r *MetadataStore) UpdateProgress(ctx context.Context, progress Progress) error {
	if err := progress.Validate(); err != nil {
		return fmt.Errorf("update migration progress: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.updateProgress,
		string(progress.Status), nullableCheckpoint(progress.CheckpointData), progress.UpdatedAt,
		progress.ID, progress.MigrationStateID, progress.RunID)
	if err != nil {
		return fmt.Errorf("update migration progress for operation %q: %w", progress.OperationID, err)
	}
	return requireOneRow("update migration progress", rowsAffected)
}

// DeleteProgressForRun removes operation progress after a run has been completely rolled back.
func (r *MetadataStore) DeleteProgressForRun(
	ctx context.Context,
	migrationStateID string,
	runID string,
) error {
	if !isUUIDv7(migrationStateID) {
		return errors.New("delete migration progress: state ID must be a canonical UUIDv7")
	}
	if !isUUIDv7(runID) {
		return errors.New("delete migration progress: run ID must be a canonical UUIDv7")
	}
	if _, err := r.dbClient.ExecuteContext(
		ctx, queryDeleteMigrationProgressForRun, migrationStateID, runID); err != nil {
		return fmt.Errorf("delete migration progress for run %q: %w", runID, err)
	}
	return nil
}

// DeleteProgressForOperation removes one forward or rollback operation target after rollback.
func (r *MetadataStore) DeleteProgressForOperation(
	ctx context.Context,
	migrationStateID string,
	targetLevel string,
	operationID OperationID,
	executionTargetID string,
) error {
	if !isUUIDv7(migrationStateID) {
		return errors.New("delete migration operation progress: state ID must be a canonical UUIDv7")
	}
	if !isCanonicalVersion(targetLevel) {
		return errors.New("delete migration operation progress: target level must be a canonical semantic version")
	}
	if strings.TrimSpace(string(operationID)) == "" {
		return errors.New("delete migration operation progress: operation ID is required")
	}
	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.deleteProgressForOperation,
		migrationStateID, targetLevel, string(operationID), executionTargetID)
	if err != nil {
		return fmt.Errorf("delete migration operation progress %q: %w", operationID, err)
	}
	if rowsAffected < 0 || rowsAffected > 1 {
		return fmt.Errorf("delete migration operation progress %q: expected at most one row, got %d",
			operationID, rowsAffected)
	}
	return nil
}

// DeleteProgressForTarget removes all progress for one release target after a complete revert.
func (r *MetadataStore) DeleteProgressForTarget(
	ctx context.Context,
	migrationStateID string,
	targetLevel string,
) error {
	if !isUUIDv7(migrationStateID) {
		return errors.New("delete migration target progress: state ID must be a canonical UUIDv7")
	}
	if !isCanonicalVersion(targetLevel) {
		return errors.New("delete migration target progress: target level must be a canonical semantic version")
	}
	if _, err := r.dbClient.ExecuteContext(ctx, r.queries.deleteProgressForTarget,
		migrationStateID, targetLevel); err != nil {
		return fmt.Errorf("delete migration target progress: %w", err)
	}
	return nil
}

// DeleteDeploymentStatesForLevel removes deployment completion markers after rollback.
func (r *MetadataStore) DeleteDeploymentStatesForLevel(
	ctx context.Context,
	migrationStateID string,
	completedLevel string,
) error {
	if !isUUIDv7(migrationStateID) || !isCanonicalVersion(completedLevel) {
		return errors.New("delete deployment migration states: invalid state ID or completed level")
	}
	if _, err := r.dbClient.ExecuteContext(ctx, r.queries.deleteDeploymentStatesForLevel,
		migrationStateID, completedLevel); err != nil {
		return fmt.Errorf("delete deployment migration states: %w", err)
	}
	return nil
}

// DeleteDeploymentState removes one deployment completion marker after phase rollback.
func (r *MetadataStore) DeleteDeploymentState(
	ctx context.Context,
	migrationStateID string,
	deploymentID string,
	completedLevel string,
) error {
	if !isUUIDv7(migrationStateID) || strings.TrimSpace(deploymentID) == "" ||
		!isCanonicalVersion(completedLevel) {
		return errors.New("delete deployment migration state: invalid identity")
	}
	if _, err := r.dbClient.ExecuteContext(ctx, r.queries.deleteDeploymentState,
		migrationStateID, deploymentID, completedLevel); err != nil {
		return fmt.Errorf("delete deployment migration state: %w", err)
	}
	return nil
}

func stateFromRow(row map[string]interface{}) (State, error) {
	id, err := requiredString(row, "id")
	if err != nil {
		return State{}, err
	}
	storeID, err := requiredString(row, "store_id")
	if err != nil {
		return State{}, err
	}
	metadataVersion, err := requiredInt(row, "metadata_version")
	if err != nil {
		return State{}, err
	}
	completedLevel, err := requiredString(row, "completed_level")
	if err != nil {
		return State{}, err
	}
	targetLevel, err := optionalString(row, "target_level")
	if err != nil {
		return State{}, err
	}
	stage, err := requiredString(row, "stage")
	if err != nil {
		return State{}, err
	}
	status, err := requiredString(row, "status")
	if err != nil {
		return State{}, err
	}
	activeRunID, err := optionalString(row, "active_run_id")
	if err != nil {
		return State{}, err
	}
	failureSummary, err := optionalString(row, "failure_summary")
	if err != nil {
		return State{}, err
	}
	updatedAt, err := requiredTime(row, "updated_at")
	if err != nil {
		return State{}, err
	}

	state := State{
		ID:              id,
		StoreID:         StoreID(storeID),
		MetadataVersion: metadataVersion,
		CompletedLevel:  completedLevel,
		TargetLevel:     targetLevel,
		Stage:           MigrationStage(stage),
		Status:          Status(status),
		ActiveRunID:     activeRunID,
		FailureSummary:  failureSummary,
		UpdatedAt:       updatedAt,
	}
	if err := state.Validate(); err != nil {
		return State{}, fmt.Errorf("invalid stored migration state: %w", err)
	}
	return state, nil
}

func deploymentStateFromRow(row map[string]interface{}) (DeploymentState, error) {
	id, err := requiredString(row, "id")
	if err != nil {
		return DeploymentState{}, err
	}
	migrationStateID, err := requiredString(row, "migration_state_id")
	if err != nil {
		return DeploymentState{}, err
	}
	deploymentID, err := requiredString(row, "deployment_id")
	if err != nil {
		return DeploymentState{}, err
	}
	completedLevel, err := requiredString(row, "completed_level")
	if err != nil {
		return DeploymentState{}, err
	}
	updatedAt, err := requiredTime(row, "updated_at")
	if err != nil {
		return DeploymentState{}, err
	}
	state := DeploymentState{
		ID: id, MigrationStateID: migrationStateID, DeploymentID: deploymentID,
		CompletedLevel: completedLevel, UpdatedAt: updatedAt,
	}
	if err := state.Validate(); err != nil {
		return DeploymentState{}, fmt.Errorf("invalid stored deployment migration state: %w", err)
	}
	return state, nil
}

func progressFromRow(row map[string]interface{}) (Progress, error) {
	id, err := requiredString(row, "id")
	if err != nil {
		return Progress{}, err
	}
	migrationStateID, err := requiredString(row, "migration_state_id")
	if err != nil {
		return Progress{}, err
	}
	runID, err := requiredString(row, "run_id")
	if err != nil {
		return Progress{}, err
	}
	targetLevel, err := requiredString(row, "target_level")
	if err != nil {
		return Progress{}, err
	}
	operationID, err := requiredString(row, "operation_id")
	if err != nil {
		return Progress{}, err
	}
	operationOrder, err := requiredInt(row, "operation_order")
	if err != nil {
		return Progress{}, err
	}
	executionTargetID, err := requiredString(row, "execution_target_id")
	if err != nil {
		return Progress{}, err
	}
	status, err := requiredString(row, "status")
	if err != nil {
		return Progress{}, err
	}
	checkpoint, err := optionalBytes(row, "checkpoint_data")
	if err != nil {
		return Progress{}, err
	}
	updatedAt, err := requiredTime(row, "updated_at")
	if err != nil {
		return Progress{}, err
	}

	progress := Progress{
		ID:                id,
		MigrationStateID:  migrationStateID,
		RunID:             runID,
		TargetLevel:       targetLevel,
		OperationID:       operationID,
		OperationOrder:    operationOrder,
		ExecutionTargetID: executionTargetID,
		Status:            Status(status),
		CheckpointData:    json.RawMessage(checkpoint),
		UpdatedAt:         updatedAt,
	}
	if err := progress.Validate(); err != nil {
		return Progress{}, fmt.Errorf("invalid stored migration progress: %w", err)
	}
	return progress, nil
}

func migrationRunMutableArgs(run MigrationRun, selectionData []byte) []interface{} {
	var failedStoreID interface{}
	var forwardOperationID interface{}
	var currentOperationID interface{}
	var executionTargetID interface{}
	var executionDirection interface{}
	var failureSummary interface{}
	rolledBack := false
	if run.Failure != nil {
		failedStoreID = string(run.Failure.StoreID)
		forwardOperationID = string(run.Failure.ForwardOperationID)
		currentOperationID = string(run.Failure.CurrentOperationID)
		executionTargetID = run.Failure.ExecutionTargetID
		executionDirection = string(run.Failure.ExecutionDirection)
		rolledBack = run.Failure.RolledBack
		failureSummary = run.Failure.FailureSummary
	}
	return []interface{}{
		string(run.Mode), string(run.Phase), nullableString(string(run.RollbackScope)), string(selectionData),
		failedStoreID, forwardOperationID, currentOperationID, executionTargetID, executionDirection,
		rolledBack, failureSummary, run.UpdatedAt,
	}
}

func migrationRunFromRow(row map[string]interface{}) (MigrationRun, error) {
	id, err := requiredString(row, "id")
	if err != nil {
		return MigrationRun{}, err
	}
	mode, err := requiredString(row, "mode")
	if err != nil {
		return MigrationRun{}, err
	}
	phase, err := requiredString(row, "phase")
	if err != nil {
		return MigrationRun{}, err
	}
	rollbackScope, err := optionalString(row, "rollback_scope")
	if err != nil {
		return MigrationRun{}, err
	}
	selectionData, err := requiredString(row, "selection_data")
	if err != nil {
		return MigrationRun{}, err
	}
	selection := MigrationSelection{}
	if err := json.Unmarshal([]byte(selectionData), &selection.DeploymentIDs); err != nil {
		return MigrationRun{}, fmt.Errorf("decode migration run selection: %w", err)
	}
	failedStoreID, err := optionalString(row, "failed_store_id")
	if err != nil {
		return MigrationRun{}, err
	}
	forwardOperationID, err := optionalString(row, "forward_operation_id")
	if err != nil {
		return MigrationRun{}, err
	}
	currentOperationID, err := optionalString(row, "current_operation_id")
	if err != nil {
		return MigrationRun{}, err
	}
	executionTargetID, err := optionalString(row, "execution_target_id")
	if err != nil {
		return MigrationRun{}, err
	}
	executionDirection, err := optionalString(row, "execution_direction")
	if err != nil {
		return MigrationRun{}, err
	}
	rolledBack, err := requiredBool(row, "rolled_back")
	if err != nil {
		return MigrationRun{}, err
	}
	failureSummary, err := optionalString(row, "failure_summary")
	if err != nil {
		return MigrationRun{}, err
	}
	updatedAt, err := requiredTime(row, "updated_at")
	if err != nil {
		return MigrationRun{}, err
	}

	run := MigrationRun{
		ID: id, Mode: MigrationRunMode(mode), Phase: OperationPhase(phase),
		RollbackScope: RollbackScope(rollbackScope), Selection: selection, UpdatedAt: updatedAt,
	}
	if failedStoreID != "" {
		run.Failure = &MigrationFailure{
			RunID: id, StoreID: StoreID(failedStoreID),
			ForwardOperationID: OperationID(forwardOperationID),
			CurrentOperationID: OperationID(currentOperationID),
			ExecutionTargetID:  executionTargetID, ExecutionDirection: ExecutionDirection(executionDirection),
			RolledBack: rolledBack, FailureSummary: failureSummary, UpdatedAt: updatedAt,
		}
	} else if forwardOperationID != "" || currentOperationID != "" || executionTargetID != "" ||
		executionDirection != "" || rolledBack || failureSummary != "" {
		return MigrationRun{}, errors.New("stored migration run contains failure data without a failed store")
	}
	if err := run.Validate(); err != nil {
		return MigrationRun{}, fmt.Errorf("invalid stored migration run: %w", err)
	}
	return run, nil
}

func requiredString(row map[string]interface{}, column string) (string, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return "", fmt.Errorf("required migration metadata column %q is missing", column)
	}
	return databaseString(value, column)
}

func optionalString(row map[string]interface{}, column string) (string, error) {
	value, exists := row[column]
	if !exists {
		return "", fmt.Errorf("migration metadata column %q is missing", column)
	}
	if value == nil {
		return "", nil
	}
	return databaseString(value, column)
}

func requiredBool(row map[string]interface{}, column string) (bool, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return false, fmt.Errorf("required migration metadata column %q is missing", column)
	}
	boolValue, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("migration metadata column %q has unexpected type %T", column, value)
	}
	return boolValue, nil
}

func databaseString(value interface{}, column string) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("migration metadata column %q has unexpected type %T", column, value)
	}
}

func requiredInt(row map[string]interface{}, column string) (int, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return 0, fmt.Errorf("required migration metadata column %q is missing", column)
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("migration metadata column %q has unexpected type %T", column, value)
	}
}

func requiredTime(row map[string]interface{}, column string) (time.Time, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return time.Time{}, fmt.Errorf("required migration metadata column %q is missing", column)
	}
	result, ok := value.(time.Time)
	if !ok {
		return time.Time{}, fmt.Errorf("migration metadata column %q has unexpected type %T", column, value)
	}
	return result, nil
}

func optionalBytes(row map[string]interface{}, column string) ([]byte, error) {
	value, exists := row[column]
	if !exists {
		return nil, fmt.Errorf("migration metadata column %q is missing", column)
	}
	if value == nil {
		return nil, nil
	}
	text, err := databaseString(value, column)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableCheckpoint(value json.RawMessage) interface{} {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func requireOneRow(action string, rowsAffected int64) error {
	if rowsAffected != 1 {
		return fmt.Errorf("%s: expected one affected row, got %d", action, rowsAffected)
	}
	return nil
}
