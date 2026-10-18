// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const abandonedRunID = "0199a123-456a-7abc-8def-0123456789ab"

func verifyResumeAfterFinalizationFailure(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("resume_after_finalization_failure", func(t *testing.T) {
		configDB := environment.OpenDatabase(t, "configdb")
		_, err := configDB.Exec(`UPDATE "INBOUND_CLIENT"
			SET PROPERTIES = jsonb_build_object(
				'allowedUserTypes', jsonb_build_array($1::text),
				'subjectAttribute', jsonb_build_object($1::text, 'email'))
			WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`, legacyEntityTypeName)
		require.NoError(t, err)
		_, err = configDB.Exec(`UPDATE "IDP"
			SET ATTRIBUTE_CONFIGURATION = jsonb_build_object(
				'userTypeResolution', jsonb_build_object(
					'default', $1::text,
					'valueMapping', jsonb_build_object('legacy', $1::text)),
				'userTypeAttributeMappings', jsonb_build_array(
					jsonb_build_object('userType', $1::text, 'attributes', '[]'::jsonb)))
			WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4792-7abc-8def-0123456789ab'`, legacyEntityTypeName)
		require.NoError(t, err)
		_, err = configDB.Exec(`UPDATE "FLOW_VERSION"
			SET NODES = jsonb_build_array(jsonb_build_object(
				'id', 'resolver',
				'type', 'TASK_EXECUTION',
				'executor', jsonb_build_object('name', 'UserTypeResolver'),
				'properties', jsonb_build_object(
					'allowedUserTypes', jsonb_build_array($1::text))))
			WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND FLOW_ID = '0199a123-4791-7abc-8def-0123456789ab' AND VERSION = 1`, legacyEntityTypeName)
		require.NoError(t, err)
		_, err = configDB.Exec(`DELETE FROM "MIGRATION_PROGRESS"
			WHERE OPERATION_ID IN (
				'2.0.0/configdb/04_backfill_entity_type_handle',
				'2.0.0/configdb/05_add_entity_type_handle_uniqueness',
				'2.0.0/configdb/44_migrate_inbound_client_entity_type_references',
				'2.0.0/configdb/45_migrate_idp_entity_type_references',
				'2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references',
				'2.0.0/configdb/47_migrate_flow_entity_type_references'
			)`)
		require.NoError(t, err)

		for _, database := range []string{"configdb", "runtime_persistent"} {
			db := environment.OpenDatabase(t, database)
			result, err := db.Exec(`UPDATE "MIGRATION_STATE" AS state
				SET COMPLETED_LEVEL = '1.0.0', TARGET_LEVEL = '2.0.0', STAGE = 'expanding', STATUS = 'failed',
                    ACTIVE_RUN_ID = (
                        SELECT progress.RUN_ID FROM "MIGRATION_PROGRESS" AS progress
                        WHERE progress.MIGRATION_STATE_ID = state.ID LIMIT 1
                    ),
                    FAILURE_SUMMARY = 'simulated finalization failure', UPDATED_AT = NOW()
                WHERE STORE_ID = $1`, database)
			require.NoError(t, err)
			rowsAffected, err := result.RowsAffected()
			require.NoError(t, err)
			require.Equal(t, int64(1), rowsAffected)
			_, err = db.Exec(`INSERT INTO "MIGRATION_RUN"
				(ID, MODE, PHASE, SELECTION_DATA, ROLLED_BACK, UPDATED_AT)
				SELECT ACTIVE_RUN_ID, 'forward', 'expand', '[]', FALSE, NOW()
				FROM "MIGRATION_STATE" WHERE STORE_ID = $1
				ON CONFLICT (ID) DO NOTHING`, database)
			require.NoError(t, err)
		}

		environment.RunMigrationCommand(t, "resume")

		migrationharness.AssertQueryValue(t, configDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
			"1.0.0:expanded:completed")
		runtimeDB := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, runtimeDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'runtime_persistent'`,
			"1.0.0:contract_ready:completed")
		environment.RunMigrationCommand(t, "apply migrate")
		t.Log("verified: resume reused completed operation progress and finalized both failed stores")
	})
}

func verifyResumeAbandonedRun(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("resume_abandoned_run", func(t *testing.T) {
		configDB := environment.OpenDatabase(t, "configdb")
		_, err := configDB.Exec(`DROP TABLE "AUTHORIZATION_PDP_CONNECTION"`)
		require.NoError(t, err)
		prepareAbandonedStore(t, configDB, "configdb")

		runtimeDB := environment.OpenDatabase(t, "runtime_persistent")
		prepareAbandonedStore(t, runtimeDB, "runtime_persistent")

		environment.RunMigrationCommand(t, "resume")

		migrationharness.AssertQueryValue(t, configDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
			"1.0.0:expanded:completed")
		migrationharness.AssertQueryValue(t, runtimeDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'runtime_persistent'`,
			"1.0.0:contract_ready:completed")
		environment.RunMigrationCommand(t, "apply migrate")
		verifyCreateAuthorizationPDPConnection(t, environment)
		t.Log("verified: resume recovered an abandoned run and reapplied pending operations")
	})
}

func prepareAbandonedStore(t *testing.T, db *sql.DB, storeID string) {
	t.Helper()
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.Exec(`DELETE FROM "MIGRATION_PROGRESS"
		WHERE MIGRATION_STATE_ID = (SELECT ID FROM "MIGRATION_STATE" WHERE STORE_ID = $1)`, storeID)
	require.NoError(t, err)
	result, err := tx.Exec(`UPDATE "MIGRATION_STATE"
		SET COMPLETED_LEVEL = '1.0.0', TARGET_LEVEL = '2.0.0', STAGE = 'expanding', STATUS = 'in_progress',
			ACTIVE_RUN_ID = $1, FAILURE_SUMMARY = NULL, UPDATED_AT = NOW()
		WHERE STORE_ID = $2`, abandonedRunID, storeID)
	require.NoError(t, err)
	rowsAffected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rowsAffected)
	require.NoError(t, tx.Commit())
}
