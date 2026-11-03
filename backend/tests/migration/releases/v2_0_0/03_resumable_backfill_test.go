// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/internal/system/transaction"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const (
	resumableBackfillOperationID engine.OperationID = "2.1.0/entitydb/resumable-backfill-test"
)

var resumableBackfillIDs = []string{
	"0199a123-4601-7abc-8def-0123456789ab",
	"0199a123-4602-7abc-8def-0123456789ab",
	"0199a123-4603-7abc-8def-0123456789ab",
	"0199a123-4604-7abc-8def-0123456789ab",
	"0199a123-4605-7abc-8def-0123456789ab",
}

type resumableBackfillCheckpoint struct {
	LastID string `json:"lastID"`
}

type resumableBackfillOperation struct {
	failAfterBatches int
	completedBatches int
}

func TestPostgresResumableBackfill(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	environment.RunMigrationCommand(t, "check")
	environment.RunMigrationCommand(t, "apply")
	environment.RunMigrationCommand(t, "apply contract")
	verifyResumableBackfill(t, environment)
}

func (o *resumableBackfillOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID: resumableBackfillOperationID, Description: "Backfill resumable integration-test rows",
		StoreID: engine.StoreIDEntityDB, Kind: engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (o *resumableBackfillOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *resumableBackfillOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	remaining, err := resumableBackfillRemaining(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if remaining == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: "Backfill is complete"}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: "Backfill is required"}, nil
}

func (o *resumableBackfillOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	if o.failAfterBatches > 0 && o.completedBatches >= o.failAfterBatches {
		return engine.ResumableBatchResult{}, errors.New("simulated resumable backfill interruption")
	}
	checkpoint := resumableBackfillCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode backfill checkpoint: %w", err)
		}
	}
	access := execution.Access()
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, dbmodel.DBQuery{
		ID: "MIG-TEST-BACKFILL-01",
		PostgresQuery: `SELECT ID FROM "MIGRATION_BACKFILL_TEST"
			WHERE ID > $1 ORDER BY ID LIMIT 2`,
	}, checkpoint.LastID)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	if len(rows) == 0 {
		return engine.ResumableBatchResult{CheckpointData: checkpointData, Completed: true}, nil
	}
	for _, row := range rows {
		id, err := row.String("id")
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		if _, err := access.Execute(ctx, engine.StoreIDEntityDB, dbmodel.DBQuery{
			ID: "MIG-TEST-BACKFILL-02",
			PostgresQuery: `UPDATE "MIGRATION_BACKFILL_TEST"
				SET TARGET_VALUE = UPPER(SOURCE_VALUE) WHERE ID = $1`,
		}, id); err != nil {
			return engine.ResumableBatchResult{}, err
		}
		checkpoint.LastID = id
	}
	nextCheckpoint, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	o.completedBatches++
	return engine.ResumableBatchResult{
		CheckpointData: nextCheckpoint,
		Completed:      len(rows) < 2,
	}, nil
}

func (o *resumableBackfillOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	remaining, err := resumableBackfillRemaining(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if remaining != 0 {
		return engine.VerificationResult{
			State: engine.VerificationInvalid, Summary: "Backfill still contains incomplete rows",
			Findings: []engine.Finding{{
				Code: "incomplete_backfill", Message: "Some integration-test rows were not transformed",
			}},
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationTarget, Summary: "Backfill is complete"}, nil
}

func resumableBackfillRemaining(ctx context.Context, access engine.ReadAccess) (int64, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, dbmodel.DBQuery{
		ID: "MIG-TEST-BACKFILL-03",
		PostgresQuery: `SELECT COUNT(*) AS REMAINING FROM "MIGRATION_BACKFILL_TEST"
			WHERE TARGET_VALUE IS NULL`,
	})
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("backfill verification returned %d rows", len(rows))
	}
	return rows[0].Int64("remaining")
}

func verifyResumableBackfill(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("resumable_backfill", func(t *testing.T) {
		prepareResumableBackfill(t, environment)

		interruptedCoordinator := newResumableBackfillCoordinator(t, environment,
			&resumableBackfillOperation{failAfterBatches: 1})
		result, err := interruptedCoordinator.ApplyPhase(
			context.Background(), engine.OperationPhaseMigrate, engine.MigrationSelection{})
		require.ErrorContains(t, err, "simulated resumable backfill interruption")
		require.Len(t, result.Operations, 0)

		entityDB := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM "MIGRATION_BACKFILL_TEST" WHERE TARGET_VALUE IS NOT NULL`, "2")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT CHECKPOINT_DATA::jsonb ->> 'lastID' FROM "MIGRATION_PROGRESS"
				WHERE OPERATION_ID = $1`, resumableBackfillIDs[1], string(resumableBackfillOperationID))
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT STATUS FROM "MIGRATION_STATE" WHERE STORE_ID = 'entitydb'`, "failed")

		resumeCoordinator := newResumableBackfillCoordinator(t, environment, &resumableBackfillOperation{})
		resumeResult, err := resumeCoordinator.Resume(context.Background())
		require.NoError(t, err)
		require.Len(t, resumeResult.Operations, 1)
		require.Equal(t, 2, resumeResult.Operations[0].CommittedBatches)

		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM "MIGRATION_BACKFILL_TEST"
				WHERE TARGET_VALUE = UPPER(SOURCE_VALUE)`, "5")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'entitydb'`, "2.1.0:complete:clean")
		t.Log("verified: resumable backfill continued from the last PostgreSQL checkpoint")
	})
}

func prepareResumableBackfill(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	db := environment.OpenDatabase(t, "entitydb")
	_, err := db.Exec(`CREATE TABLE "MIGRATION_BACKFILL_TEST" (
		ID VARCHAR(36) PRIMARY KEY,
		SOURCE_VALUE VARCHAR(64) NOT NULL,
		TARGET_VALUE VARCHAR(64),
		DEPLOYMENT_ID VARCHAR(255) NOT NULL
	)`)
	require.NoError(t, err)
	for index, id := range resumableBackfillIDs {
		_, err = db.Exec(`INSERT INTO "MIGRATION_BACKFILL_TEST"
			(ID, SOURCE_VALUE, TARGET_VALUE, DEPLOYMENT_ID) VALUES ($1, $2, NULL, $3)`,
			id, fmt.Sprintf("value-%d", index+1), "integration-deployment")
		require.NoError(t, err)
	}
}

func newResumableBackfillCoordinator(
	t *testing.T,
	environment migrationharness.Environment,
	operation engine.Operation,
) *engine.MigrationCoordinator {
	t.Helper()
	storeLevels := map[engine.StoreID]string{
		engine.StoreIDConfigDB:          "2.0.0",
		engine.StoreIDEntityDB:          "2.0.0",
		engine.StoreIDRuntimeTransient:  "1.0.0",
		engine.StoreIDRuntimePersistent: "2.0.0",
	}
	manifest := engine.ReleaseManifest{
		ManifestVersion: 1, ReleaseVersion: "2.1.0", MigrationMetadataVersion: 2,
		MigrationDocumentation: "Resumable PostgreSQL integration test",
	}
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		requiredLevel := storeLevels[storeID]
		changed := storeID == engine.StoreIDEntityDB
		if changed {
			requiredLevel = manifest.ReleaseVersion
		}
		manifest.Stores = append(manifest.Stores, engine.StoreRequirement{
			ID: storeID, AcceptedSourceLevel: storeLevels[storeID],
			RequiredLevel: requiredLevel, Changed: changed,
		})
	}
	release := engine.ReleaseDefinition{Manifest: manifest, Plan: engine.MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Migrate:       []engine.Operation{operation},
	}}
	namespaces := make([]engine.MigrationNamespace, 0, len(manifest.Stores))
	for _, requirement := range manifest.Stores {
		db := environment.OpenDatabase(t, string(requirement.ID))
		client := &postgresIntegrationClient{db: db, transactionKey: string(requirement.ID)}
		namespaces = append(namespaces, engine.MigrationNamespace{
			StoreIDs: []engine.StoreID{requirement.ID}, DatabaseType: engine.DatabaseTypePostgres,
			Client: client, Transactioner: transaction.NewTransactioner(db, client.transactionKey),
			Locker: integrationMigrationLocker{},
		})
	}
	coordinator, err := engine.NewMigrationCoordinator(release, namespaces)
	require.NoError(t, err)
	return coordinator
}

type integrationMigrationLocker struct{}

func (integrationMigrationLocker) WithLock(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type postgresIntegrationClient struct {
	db             *sql.DB
	transactionKey string
}

func (c *postgresIntegrationClient) Query(
	query dbmodel.DBQuery,
	args ...interface{},
) ([]map[string]interface{}, error) {
	return c.QueryContext(context.Background(), query, args...)
}

func (c *postgresIntegrationClient) QueryContext(
	ctx context.Context,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]map[string]interface{}, error) {
	var rows *sql.Rows
	var err error
	if tx := transaction.KeyedTxFromContext(ctx, c.transactionKey); tx != nil {
		rows, err = tx.QueryContext(ctx, query.GetQuery("postgres"), args...)
	} else {
		rows, err = c.db.QueryContext(ctx, query.GetQuery("postgres"), args...)
	}
	if err != nil {
		return nil, err
	}
	return readIntegrationRows(rows)
}

func (c *postgresIntegrationClient) Execute(
	query dbmodel.DBQuery,
	args ...interface{},
) (int64, error) {
	return c.ExecuteContext(context.Background(), query, args...)
}

func (c *postgresIntegrationClient) ExecuteContext(
	ctx context.Context,
	query dbmodel.DBQuery,
	args ...interface{},
) (int64, error) {
	var result sql.Result
	var err error
	if tx := transaction.KeyedTxFromContext(ctx, c.transactionKey); tx != nil {
		result, err = tx.ExecContext(ctx, query.GetQuery("postgres"), args...)
	} else {
		result, err = c.db.ExecContext(ctx, query.GetQuery("postgres"), args...)
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (c *postgresIntegrationClient) BeginTx() (dbmodel.TxInterface, error) {
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	return dbmodel.NewTx(tx, "postgres"), nil
}

func (c *postgresIntegrationClient) GetTransactioner() (providers.Transactioner, error) {
	return transaction.NewTransactioner(c.db, c.transactionKey), nil
}

func (c *postgresIntegrationClient) WithSession(
	ctx context.Context,
	fn func(dbprovider.DBSessionInterface) error,
) error {
	connection, err := c.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = connection.Close()
	}()
	return fn(postgresIntegrationSession{connection: connection})
}

type postgresIntegrationSession struct {
	connection *sql.Conn
}

func (s postgresIntegrationSession) QueryContext(
	ctx context.Context,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]map[string]interface{}, error) {
	rows, err := s.connection.QueryContext(ctx, query.GetQuery("postgres"), args...)
	if err != nil {
		return nil, err
	}
	return readIntegrationRows(rows)
}

func readIntegrationRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			row[strings.ToLower(column)] = values[index]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
