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
		_, err := configDB.Exec(`DELETE FROM "MIGRATION_PROGRESS"
			WHERE OPERATION_ID = '2.0.0/configdb/05_backfill_entity_type_handle'`)
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
			"2.0.0:complete:clean")
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
		_, err = runtimeDB.Exec(`ALTER TABLE "REVOKED_TOKEN"
			ADD CONSTRAINT "REVOKED_TOKEN_revocation_reason_check"
			CHECK (REVOCATION_REASON IN ('explicit', 'refresh_rotation'))`)
		require.NoError(t, err)
		prepareAbandonedStore(t, runtimeDB, "runtime_persistent")

		environment.RunMigrationCommand(t, "resume")

		migrationharness.AssertQueryValue(t, configDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
			"1.0.0:expanded:completed")
		migrationharness.AssertQueryValue(t, runtimeDB,
			`SELECT COMPLETED_LEVEL || ':' || STAGE || ':' || STATUS
				FROM "MIGRATION_STATE" WHERE STORE_ID = 'runtime_persistent'`,
			"2.0.0:complete:clean")
		environment.RunMigrationCommand(t, "apply migrate")
		verifyCreateAuthorizationPDPConnection(t, environment)
		verifyRemoveRevocationReasonCheck(t, environment)
		t.Log("verified: resume recovered an abandoned run and reapplied both pending operations")
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
