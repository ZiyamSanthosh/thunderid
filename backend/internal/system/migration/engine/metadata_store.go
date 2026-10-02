// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/database/provider"
)

var (
	queryGetMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-01",
		PostgresQuery: `SELECT ID, STORE_ID, METADATA_VERSION, COMPLETED_LEVEL, TARGET_LEVEL, STATUS,
            ACTIVE_RUN_ID, FAILURE_SUMMARY, UPDATED_AT
            FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
	}
	queryInsertMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-02",
		PostgresQuery: `INSERT INTO "MIGRATION_STATE" (ID, STORE_ID, METADATA_VERSION, COMPLETED_LEVEL,
            TARGET_LEVEL, STATUS, ACTIVE_RUN_ID, FAILURE_SUMMARY, UPDATED_AT)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
	}
	queryUpdateMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-03",
		PostgresQuery: `UPDATE "MIGRATION_STATE" SET METADATA_VERSION = $1, COMPLETED_LEVEL = $2,
            TARGET_LEVEL = $3, STATUS = $4, ACTIVE_RUN_ID = $5, FAILURE_SUMMARY = $6, UPDATED_AT = $7
            WHERE ID = $8 AND STORE_ID = $9`,
	}
	queryListMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-04",
		PostgresQuery: `SELECT ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
            STATUS, CHECKPOINT_DATA, UPDATED_AT FROM "MIGRATION_PROGRESS"
            WHERE MIGRATION_STATE_ID = $1 AND RUN_ID = $2 ORDER BY OPERATION_ORDER, OPERATION_ID`,
	}
	queryInsertMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-05",
		PostgresQuery: `INSERT INTO "MIGRATION_PROGRESS" (ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL,
            OPERATION_ID, OPERATION_ORDER, STATUS, CHECKPOINT_DATA, UPDATED_AT)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
	}
	queryUpdateMigrationProgress = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-06",
		PostgresQuery: `UPDATE "MIGRATION_PROGRESS" SET STATUS = $1, CHECKPOINT_DATA = $2, UPDATED_AT = $3
			WHERE ID = $4 AND MIGRATION_STATE_ID = $5 AND RUN_ID = $6`,
	}
	queryReserveMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-09",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET TARGET_LEVEL = $1, STATUS = $2, ACTIVE_RUN_ID = $3, FAILURE_SUMMARY = NULL, UPDATED_AT = $4
            WHERE ID = $5 AND STORE_ID = $6 AND METADATA_VERSION = $7 AND COMPLETED_LEVEL = $8
            AND STATUS = $9 AND TARGET_LEVEL IS NULL AND ACTIVE_RUN_ID IS NULL`,
	}
	queryFinalizeMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-10",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET COMPLETED_LEVEL = $1, TARGET_LEVEL = NULL, STATUS = $2, ACTIVE_RUN_ID = NULL,
                FAILURE_SUMMARY = NULL, UPDATED_AT = $3
            WHERE ID = $4 AND STORE_ID = $5 AND METADATA_VERSION = $6 AND COMPLETED_LEVEL = $7
            AND TARGET_LEVEL = $1 AND STATUS = $8 AND ACTIVE_RUN_ID = $9`,
	}
	queryCancelMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-11",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET TARGET_LEVEL = NULL, STATUS = $1, ACTIVE_RUN_ID = NULL, FAILURE_SUMMARY = NULL,
                UPDATED_AT = $2
            WHERE ID = $3 AND STORE_ID = $4 AND METADATA_VERSION = $5 AND COMPLETED_LEVEL = $6
            AND TARGET_LEVEL = $7 AND STATUS = $8 AND ACTIVE_RUN_ID = $9
            AND NOT EXISTS (
                SELECT 1 FROM "MIGRATION_PROGRESS" WHERE MIGRATION_STATE_ID = $3 AND RUN_ID = $9
            )`,
	}
	queryFailMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-12",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET STATUS = $1, FAILURE_SUMMARY = $2, UPDATED_AT = $3
            WHERE ID = $4 AND STORE_ID = $5 AND METADATA_VERSION = $6 AND COMPLETED_LEVEL = $7
            AND TARGET_LEVEL = $8 AND STATUS = $9 AND ACTIVE_RUN_ID = $10`,
	}
	queryResumeMigrationState = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-13",
		PostgresQuery: `UPDATE "MIGRATION_STATE"
            SET STATUS = $1, FAILURE_SUMMARY = NULL, UPDATED_AT = $2
            WHERE ID = $3 AND STORE_ID = $4 AND METADATA_VERSION = $5 AND COMPLETED_LEVEL = $6
			AND TARGET_LEVEL = $7 AND STATUS = $8 AND ACTIVE_RUN_ID = $9
			AND FAILURE_SUMMARY = $10 AND UPDATED_AT = $11`,
	}
)

// DatabaseType identifies a database implementation supported by the migration engine.
type DatabaseType string

const (
	// DatabaseTypePostgres identifies PostgreSQL migration metadata behavior.
	DatabaseTypePostgres DatabaseType = "postgres"
)

type metadataQuerySet struct {
	getState       dbmodel.DBQuery
	insertState    dbmodel.DBQuery
	updateState    dbmodel.DBQuery
	listProgress   dbmodel.DBQuery
	insertProgress dbmodel.DBQuery
	updateProgress dbmodel.DBQuery
	reserveState   dbmodel.DBQuery
	finalizeState  dbmodel.DBQuery
	cancelState    dbmodel.DBQuery
	failState      dbmodel.DBQuery
	resumeState    dbmodel.DBQuery
	inspectSchema  dbmodel.DBQuery
	createSchema   dbmodel.DBQuery
}

var migrationMetadataQueries = metadataQuerySet{
	getState: queryGetMigrationState, insertState: queryInsertMigrationState,
	updateState: queryUpdateMigrationState, listProgress: queryListMigrationProgress,
	insertProgress: queryInsertMigrationProgress, updateProgress: queryUpdateMigrationProgress,
	reserveState: queryReserveMigrationState, finalizeState: queryFinalizeMigrationState,
	cancelState: queryCancelMigrationState, failState: queryFailMigrationState,
	resumeState:   queryResumeMigrationState,
	inspectSchema: queryInspectMetadataSchema, createSchema: queryCreateMetadataSchema,
}

// ReserveState conditionally assigns one clean source-level store to a migration run.
func (r *MetadataStore) ReserveState(
	ctx context.Context,
	current State,
	targetLevel string,
	runID string,
	updatedAt time.Time,
) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, fmt.Errorf("reserve migration state: invalid current state: %w", err)
	}
	reserved := current
	reserved.TargetLevel = targetLevel
	reserved.Status = StatusInProgress
	reserved.ActiveRunID = runID
	reserved.FailureSummary = ""
	reserved.UpdatedAt = updatedAt
	if err := reserved.Validate(); err != nil {
		return false, fmt.Errorf("reserve migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.reserveState,
		reserved.TargetLevel, string(reserved.Status), reserved.ActiveRunID, reserved.UpdatedAt,
		reserved.ID, string(reserved.StoreID), reserved.MetadataVersion, reserved.CompletedLevel,
		string(StatusClean))
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
	completed.Status = StatusClean
	completed.ActiveRunID = ""
	completed.FailureSummary = ""
	completed.UpdatedAt = updatedAt
	if err := completed.Validate(); err != nil {
		return false, fmt.Errorf("finalize migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.finalizeState,
		current.TargetLevel, string(StatusClean), completed.UpdatedAt,
		current.ID, string(current.StoreID), current.MetadataVersion, current.CompletedLevel,
		string(StatusInProgress), current.ActiveRunID)
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
	cancelled.Status = StatusClean
	cancelled.ActiveRunID = ""
	cancelled.FailureSummary = ""
	cancelled.UpdatedAt = updatedAt
	if err := cancelled.Validate(); err != nil {
		return false, fmt.Errorf("cancel migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.cancelState,
		string(StatusClean), cancelled.UpdatedAt, current.ID, string(current.StoreID), current.MetadataVersion,
		current.CompletedLevel, current.TargetLevel, string(StatusInProgress), current.ActiveRunID)
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
		current.TargetLevel, string(StatusInProgress), current.ActiveRunID)
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
		current.TargetLevel, string(StatusFailed), current.ActiveRunID, current.FailureSummary, current.UpdatedAt)
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
		nullableString(state.TargetLevel), string(state.Status), nullableString(state.ActiveRunID),
		nullableString(state.FailureSummary), state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert migration state for store %q: %w", state.StoreID, err)
	}
	return requireOneRow("insert migration state", rowsAffected)
}

// UpdateState replaces the mutable state fields for an existing logical store.
func (r *MetadataStore) UpdateState(ctx context.Context, state State) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("update migration state: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.updateState,
		state.MetadataVersion, state.CompletedLevel, nullableString(state.TargetLevel), string(state.Status),
		nullableString(state.ActiveRunID), nullableString(state.FailureSummary), state.UpdatedAt,
		state.ID, string(state.StoreID))
	if err != nil {
		return fmt.Errorf("update migration state for store %q: %w", state.StoreID, err)
	}
	return requireOneRow("update migration state", rowsAffected)
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

// InsertProgress creates the durable record for a migration operation.
func (r *MetadataStore) InsertProgress(ctx context.Context, progress Progress) error {
	if err := progress.Validate(); err != nil {
		return fmt.Errorf("insert migration progress: %w", err)
	}

	rowsAffected, err := r.dbClient.ExecuteContext(ctx, r.queries.insertProgress,
		progress.ID, progress.MigrationStateID, progress.RunID, progress.TargetLevel,
		progress.OperationID, progress.OperationOrder, string(progress.Status),
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
		ID:               id,
		MigrationStateID: migrationStateID,
		RunID:            runID,
		TargetLevel:      targetLevel,
		OperationID:      operationID,
		OperationOrder:   operationOrder,
		Status:           Status(status),
		CheckpointData:   json.RawMessage(checkpoint),
		UpdatedAt:        updatedAt,
	}
	if err := progress.Validate(); err != nil {
		return Progress{}, fmt.Errorf("invalid stored migration progress: %w", err)
	}
	return progress, nil
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
