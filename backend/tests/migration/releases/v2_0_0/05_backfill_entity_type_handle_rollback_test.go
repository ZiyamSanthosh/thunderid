// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const rollbackRunID = "0199a123-4900-7abc-8def-0123456789ab"

const (
	phaseRollbackRunID        = "0199a123-4910-7abc-8def-0123456789ab"
	deploymentRollbackRunID   = "0199a123-4920-7abc-8def-0123456789ab"
	interruptedRollbackRunID  = "0199a123-4930-7abc-8def-0123456789ab"
	expandPhaseRollbackRunID  = "0199a123-4940-7abc-8def-0123456789ab"
	interruptedRevertAllRunID = "0199a123-4950-7abc-8def-0123456789ab"
)

func TestPostgresMigrationRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply expand")

	db := environment.OpenDatabase(t, "configdb")
	_, err := db.Exec(`UPDATE "ENTITY_TYPES" SET HANDLE = 'legacy-person'
		WHERE ID = '0199a123-4701-7abc-8def-0123456789ab'`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE "ENTITY_TYPES" SET HANDLE = 'legacy-person_1'
		WHERE ID = '0199a123-4704-7abc-8def-0123456789ab'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "ENTITY_TYPES"
		(DEPLOYMENT_ID, ID, CATEGORY, NAME, HANDLE, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
		VALUES ('unselected-deployment', '0199a123-4902-7abc-8def-0123456789ab', 'user',
			'Preserved Entity', 'preserved-entity', '0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "MIGRATION_PROGRESS"
		(ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
		 EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT)
		SELECT '0199a123-4901-7abc-8def-0123456789ab', ID, $1, '2.0.0',
			'2.0.0/configdb/05_backfill_entity_type_handle', 4, 'migration-deployment', 'in_progress',
			'{"lastID":"0199a123-4704-7abc-8def-0123456789ab"}', NOW()
		FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`, rollbackRunID)
	require.NoError(t, err)
	insertMigrationRun(t, db, rollbackRunID, "forward", "migrate", "",
		[]string{"migration-deployment"}, "configdb",
		"2.0.0/configdb/05_backfill_entity_type_handle",
		"2.0.0/configdb/05_backfill_entity_type_handle", "migration-deployment", "forward",
		"simulated partial backfill failure")
	_, err = db.Exec(`UPDATE "MIGRATION_STATE"
		SET STAGE = 'migrating', STATUS = 'failed', ACTIVE_RUN_ID = $1,
			FAILURE_SUMMARY = 'simulated partial backfill failure', UPDATED_AT = NOW()
		WHERE STORE_ID = 'configdb'`, rollbackRunID)
	require.NoError(t, err)

	environment.RunMigrationCommand(t, "rollback")

	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'migration-deployment' AND HANDLE IS NOT NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT HANDLE FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'unselected-deployment'`, "preserved-entity")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:migrating:failed")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE RUN_ID = $1`, "0", rollbackRunID)
	migrationharness.AssertQueryValue(t, db,
		`SELECT ROLLED_BACK::TEXT FROM "MIGRATION_RUN" WHERE ID = $1`, "true", rollbackRunID)

	environment.RunMigrationCommand(t, "resume")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'migration-deployment' AND HANDLE IS NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:expanded:completed")
	t.Log("verified: rollback reverted only the failed operation and resume reapplied it")
}

func TestPostgresMigrationPhaseRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply")

	db := environment.OpenDatabase(t, "configdb")
	prepareFailedCompletedMigrateRun(t, db, phaseRollbackRunID, nil)

	environment.RunMigrationCommand(t, "rollback --phase migrate")

	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NOT NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:expanded:completed")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE RUN_ID = $1`, "0", phaseRollbackRunID)
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_RUN" WHERE ID = $1`, "0", phaseRollbackRunID)
	t.Log("verified: phase rollback reverted completed migrate work and restored the expand boundary")
}

func TestPostgresExpandPhaseRollbackRestoresEveryParticipatingStore(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	environment.RunMigrationCommand(t, "apply expand")

	databases := []string{"configdb", "entitydb", "runtime_persistent"}
	for _, database := range databases {
		db := environment.OpenDatabase(t, database)
		_, err := db.Exec(`UPDATE "MIGRATION_PROGRESS" SET RUN_ID = $1, UPDATED_AT = NOW()
			WHERE OPERATION_ID IN (
				'2.0.0/runtime_persistent/01_remove_revocation_reason_check',
				'2.0.0/configdb/02_create_authorization_pdp_connection',
				'2.0.0/configdb/03_add_entity_type_handle',
				'2.0.0/entitydb/04_expand_entity_type_length'
			)`, expandPhaseRollbackRunID)
		require.NoError(t, err)
	}

	for _, database := range databases {
		insertMigrationRun(t, environment.OpenDatabase(t, database), expandPhaseRollbackRunID,
			"forward", "expand", "", nil, "entitydb",
			"2.0.0/entitydb/04_expand_entity_type_length",
			"2.0.0/entitydb/04_expand_entity_type_length", "", "forward",
			"simulated expand finalization failure")
	}

	entityDB := environment.OpenDatabase(t, "entitydb")
	_, err := entityDB.Exec(`UPDATE "MIGRATION_STATE"
		SET COMPLETED_LEVEL = '1.0.0', TARGET_LEVEL = '2.0.0', STAGE = 'expanding',
			STATUS = 'failed', ACTIVE_RUN_ID = $1,
			FAILURE_SUMMARY = 'simulated expand finalization failure', UPDATED_AT = NOW()
		WHERE STORE_ID = 'entitydb'`, expandPhaseRollbackRunID)
	require.NoError(t, err)

	environment.RunMigrationCommand(t, "rollback --phase expand")

	for _, database := range databases {
		db := environment.OpenDatabase(t, database)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
			"1.0.0:complete:clean", database)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE RUN_ID = $1`,
			"0", expandPhaseRollbackRunID)
	}
	t.Log("verified: expand phase rollback restored every store that participated in the failed run")
}

func TestPostgresDeploymentSpecificPhaseRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	db := environment.OpenDatabase(t, "configdb")
	_, err := db.Exec(`INSERT INTO "ENTITY_TYPES"
		(DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
		VALUES
			('deployment-a', '0199a123-4921-7abc-8def-0123456789ab', 'user', 'Alpha User',
				'0199a123-4924-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
			('deployment-b', '0199a123-4922-7abc-8def-0123456789ab', 'user', 'Beta User',
				'0199a123-4925-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
			('deployment-c', '0199a123-4923-7abc-8def-0123456789ab', 'user', 'Preserved User',
				'0199a123-4926-7abc-8def-0123456789ab', FALSE, '{}'::jsonb)`)
	require.NoError(t, err)

	environment.RunMigrationCommand(t, "apply expand")
	environment.RunMigrationCommand(t,
		"apply migrate --deployment-id deployment-a --deployment-id deployment-b")
	_, err = db.Exec(`UPDATE "ENTITY_TYPES" SET HANDLE = 'preserved-user'
		WHERE DEPLOYMENT_ID = 'deployment-c'`)
	require.NoError(t, err)
	prepareFailedCompletedMigrateRun(t, db, deploymentRollbackRunID,
		[]string{"deployment-a", "deployment-b"})

	environment.RunMigrationCommand(t, "rollback --phase migrate")

	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID IN ('deployment-a', 'deployment-b') AND HANDLE IS NOT NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT HANDLE FROM "ENTITY_TYPES" WHERE DEPLOYMENT_ID = 'deployment-c'`, "preserved-user")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_DEPLOYMENT_STATE"
			WHERE DEPLOYMENT_ID IN ('deployment-a', 'deployment-b')`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:expanded:completed")
	t.Log("verified: phase rollback preserved the exact deployment selection")
}

func TestPostgresMigrationCompleteRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply")

	db := environment.OpenDatabase(t, "configdb")
	environment.RunMigrationCommand(t, "rollback --revert-all")

	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NOT NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:complete:clean")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS"
			WHERE TARGET_LEVEL = '2.0.0'`, "0")
	for _, database := range []string{"entitydb", "runtime_persistent"} {
		migrationharness.AssertQueryValue(t, environment.OpenDatabase(t, database),
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
			"1.0.0:complete:clean", database)
	}
	t.Log("verified: complete rollback reverted executed work and restored the accepted source boundary")
}

func TestPostgresMigrationCompleteRollbackResumesInterruptedRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply")

	configDB := environment.OpenDatabase(t, "configdb")
	const checkpointID = "0199a123-4849-7abc-8def-0123456789ab"
	_, err := configDB.Exec(`UPDATE "ENTITY_TYPES" SET HANDLE = NULL WHERE ID <= $1`, checkpointID)
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "MIGRATION_PROGRESS"
		(ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
		 EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT)
		SELECT '0199a123-4951-7abc-8def-0123456789ab', ID, $1, '2.0.0',
			'2.0.0/configdb/08_rollback_entity_type_handle_backfill', 4, '', 'in_progress',
			jsonb_build_object('lastID', $2::TEXT)::TEXT, NOW()
		FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`, interruptedRevertAllRunID, checkpointID)
	require.NoError(t, err)
	for _, database := range []string{"configdb", "entitydb", "runtime_persistent"} {
		db := environment.OpenDatabase(t, database)
		insertMigrationRun(t, db, interruptedRevertAllRunID, "revert_all", "", "all", nil,
			"configdb", "2.0.0/configdb/05_backfill_entity_type_handle",
			"2.0.0/configdb/08_rollback_entity_type_handle_backfill", "", "rollback",
			"simulated interrupted complete rollback")
		_, err = db.Exec(`UPDATE "MIGRATION_STATE"
			SET COMPLETED_LEVEL = '1.0.0', TARGET_LEVEL = '2.0.0', STAGE = 'rolling_back',
				STATUS = 'failed', ACTIVE_RUN_ID = $1,
				FAILURE_SUMMARY = 'simulated interrupted complete rollback', UPDATED_AT = NOW()
			WHERE STORE_ID = $2`, interruptedRevertAllRunID, database)
		require.NoError(t, err)
	}

	environment.RunMigrationCommand(t, "rollback --revert-all")

	migrationharness.AssertQueryValue(t, configDB,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NOT NULL`, "0")
	for _, database := range []string{"configdb", "entitydb", "runtime_persistent"} {
		db := environment.OpenDatabase(t, database)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
			"1.0.0:complete:clean", database)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE RUN_ID = $1`,
			"0", interruptedRevertAllRunID)
	}
	t.Log("verified: complete rollback resumed its interrupted rollback run")
}

func TestPostgresMigrationCompleteRollbackBlockedByContract(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply")
	environment.RunMigrationCommand(t, "apply contract")

	db := environment.OpenDatabase(t, "configdb")
	output := environment.RunMigrationCommandExpectFailure(t, "rollback --revert-all")
	require.Contains(t, output, "complete migration rollback is blocked by contract operation")
	require.Contains(t, output, "2.0.0/configdb/07_finalize_entity_type_handle")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"2.0.0:complete:clean")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NULL`, "0")
	t.Log("verified: a completed contract operation without rollback blocks complete rollback before mutation")
}

func TestPostgresMigrationRollbackResumesInterruptedRollback(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	prepareEntityTypeMigrationSource(t, environment)
	environment.RunMigrationCommand(t, "apply")

	db := environment.OpenDatabase(t, "configdb")
	prepareInterruptedRollbackRun(t, db)

	output := environment.RunMigrationCommandExpectFailure(t, "resume")
	require.Contains(t, output, "migration run failed during rollback")
	require.Contains(t, output, "rerun the applicable migration rollback command")

	environment.RunMigrationCommand(t, "rollback")

	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NOT NULL`, "0")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE RUN_ID = $1`,
		"0", interruptedRollbackRunID)
	migrationharness.AssertQueryValue(t, db,
		`SELECT EXECUTION_DIRECTION || ':' || ROLLED_BACK::TEXT
			FROM "MIGRATION_RUN" WHERE ID = $1`,
		"forward:true", interruptedRollbackRunID)
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
			FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"1.0.0:migrating:failed")
	t.Log("verified: a second rollback invocation resumed from its durable checkpoint")
}

func prepareFailedCompletedMigrateRun(t *testing.T, db *sql.DB, runID string, deploymentIDs []string) {
	t.Helper()
	result, err := db.Exec(`UPDATE "MIGRATION_PROGRESS" SET RUN_ID = $1, UPDATED_AT = NOW()
		WHERE OPERATION_ID IN (
			'2.0.0/configdb/05_backfill_entity_type_handle',
			'2.0.0/configdb/06_add_entity_type_handle_uniqueness'
		)`, runID)
	require.NoError(t, err)
	rowsAffected, err := result.RowsAffected()
	require.NoError(t, err)
	require.GreaterOrEqual(t, rowsAffected, int64(2))

	insertMigrationRun(t, db, runID, "forward", "migrate", "", deploymentIDs, "configdb",
		"2.0.0/configdb/06_add_entity_type_handle_uniqueness",
		"2.0.0/configdb/06_add_entity_type_handle_uniqueness", "", "forward",
		"simulated migrate finalization failure")
	_, err = db.Exec(`UPDATE "MIGRATION_STATE"
		SET STAGE = 'migrating', STATUS = 'failed', ACTIVE_RUN_ID = $1,
			FAILURE_SUMMARY = 'simulated migrate finalization failure', UPDATED_AT = NOW()
		WHERE STORE_ID = 'configdb'`, runID)
	require.NoError(t, err)
}

func prepareInterruptedRollbackRun(t *testing.T, db *sql.DB) {
	t.Helper()
	const checkpointID = "0199a123-4849-7abc-8def-0123456789ab"
	_, err := db.Exec(`UPDATE "MIGRATION_PROGRESS" SET RUN_ID = $1, UPDATED_AT = NOW()
		WHERE OPERATION_ID = '2.0.0/configdb/05_backfill_entity_type_handle'`, interruptedRollbackRunID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE "ENTITY_TYPES" SET HANDLE = NULL WHERE ID <= $1`, checkpointID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "MIGRATION_PROGRESS"
		(ID, MIGRATION_STATE_ID, RUN_ID, TARGET_LEVEL, OPERATION_ID, OPERATION_ORDER,
		 EXECUTION_TARGET_ID, STATUS, CHECKPOINT_DATA, UPDATED_AT)
		SELECT '0199a123-4931-7abc-8def-0123456789ab', ID, $1, '2.0.0',
			'2.0.0/configdb/08_rollback_entity_type_handle_backfill', 4, '', 'in_progress',
			jsonb_build_object('lastID', $2::TEXT)::TEXT, NOW()
		FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`, interruptedRollbackRunID, checkpointID)
	require.NoError(t, err)
	insertMigrationRun(t, db, interruptedRollbackRunID, "rollback", "migrate", "failed_operation", nil,
		"configdb", "2.0.0/configdb/05_backfill_entity_type_handle",
		"2.0.0/configdb/08_rollback_entity_type_handle_backfill", "", "rollback",
		"simulated interrupted rollback")
	_, err = db.Exec(`UPDATE "MIGRATION_STATE"
		SET STAGE = 'migrating', STATUS = 'failed', ACTIVE_RUN_ID = $1,
			FAILURE_SUMMARY = 'simulated interrupted rollback', UPDATED_AT = NOW()
		WHERE STORE_ID = 'configdb'`, interruptedRollbackRunID)
	require.NoError(t, err)
}

func insertMigrationRun(
	t *testing.T,
	db *sql.DB,
	runID string,
	mode string,
	phase string,
	rollbackScope string,
	deploymentIDs []string,
	failedStoreID string,
	forwardOperationID string,
	currentOperationID string,
	executionTargetID string,
	executionDirection string,
	failureSummary string,
) {
	t.Helper()
	selectionData, err := json.Marshal(deploymentIDs)
	require.NoError(t, err)
	if deploymentIDs == nil {
		selectionData = []byte("[]")
	}
	_, err = db.Exec(`INSERT INTO "MIGRATION_RUN"
		(ID, MODE, PHASE, ROLLBACK_SCOPE, SELECTION_DATA, FAILED_STORE_ID,
		 FORWARD_OPERATION_ID, CURRENT_OPERATION_ID, EXECUTION_TARGET_ID,
		 EXECUTION_DIRECTION, ROLLED_BACK, FAILURE_SUMMARY, UPDATED_AT)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''), NULLIF($7, ''),
			NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), FALSE, NULLIF($11, ''), NOW())`,
		runID, mode, phase, rollbackScope, string(selectionData), failedStoreID,
		forwardOperationID, currentOperationID, executionTargetID, executionDirection, failureSummary)
	require.NoError(t, err)
}
