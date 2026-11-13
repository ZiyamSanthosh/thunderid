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

const sessionOperationPrefix = "runtime_persistent/"

var expandedSessionOperations = []string{
	"51_rename_session_tables",
	"52_rename_session_indexes",
	"53_add_session_type_check",
	"54_validate_session_type_check",
	"55_add_session_subject_index",
}

func relationKinds() string {
	return `SELECT string_agg(c.relname || '=' || c.relkind::TEXT, ',' ORDER BY c.relname)
		FROM pg_class c INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = CURRENT_SCHEMA()
			AND c.relname IN ('SESSION', 'SESSION_PARTICIPANT', 'SSO_SESSION', 'SSO_SESSION_CONTEXT',
				'SSO_SESSION_PARTICIPANT')
			AND c.relkind IN ('r', 'v')`
}

func mustExec(t *testing.T, db *sql.DB, query string, arguments ...any) {
	t.Helper()
	_, err := db.Exec(query, arguments...)
	require.NoError(t, err)
}

// verifyExpandedSessionTables checks the session schema while the previous binary still serves: renamed tables,
// compatibility views, the type column and check, the organization unit column, the indexes, and the live session.
func verifyExpandedSessionTables(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("expanded_session_tables", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db, relationKinds(),
			"SESSION=r,SESSION_PARTICIPANT=r,SSO_SESSION=v,SSO_SESSION_CONTEXT=r,SSO_SESSION_PARTICIPANT=v")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE || ':' || DATA_TYPE || ':' || CHARACTER_MAXIMUM_LENGTH::TEXT || ':' ||
				COALESCE(COLUMN_DEFAULT, 'none')
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SESSION' AND COLUMN_NAME = 'type'`,
			"NO:character varying:10:'SSO'::character varying")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE || ':' || COALESCE(COLUMN_DEFAULT, 'none')
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SESSION_PARTICIPANT' AND COLUMN_NAME = 'ou_id'`,
			"NO:''::character varying")
		migrationharness.AssertQueryValue(t, db,
			`SELECT c.conname || ':' || c.convalidated::TEXT
			FROM pg_constraint c INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'SESSION' AND c.contype = 'c'`,
			"SESSION_type_check:true")
		migrationharness.AssertQueryValue(t, db,
			`SELECT string_agg(t.relname || '.' || idx.relname || ':' || i.indisvalid::TEXT,
				',' ORDER BY t.relname, idx.relname)
			FROM pg_index i INNER JOIN pg_class idx ON idx.oid = i.indexrelid
			INNER JOIN pg_class t ON t.oid = i.indrelid INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname IN ('SESSION', 'SESSION_PARTICIPANT')`,
			"SESSION.SESSION_pkey:true,SESSION.idx_session_absolute_expires_at:true,"+
				"SESSION.idx_session_flow_execution:true,SESSION.idx_session_handle_id:true,"+
				"SESSION.idx_session_subject:true,SESSION_PARTICIPANT.SESSION_PARTICIPANT_pkey:true,"+
				"SESSION_PARTICIPANT.idx_session_participant_app:true")
		for _, operation := range expandedSessionOperations {
			migrationharness.AssertOperationCompleted(t, db, sessionOperationPrefix+operation)
		}
		migrationharness.AssertQueryValue(t, db,
			`SELECT TYPE || ':' || SUBJECT_ID FROM "SESSION" WHERE SESSION_ID = $1 AND DEPLOYMENT_ID = $2`,
			"SSO:subject-1", existingSession, fixtureDeployment)
		migrationharness.AssertQueryValue(t, db,
			`SELECT APP_ID FROM "SESSION_PARTICIPANT" WHERE SESSION_ID = $1 AND DEPLOYMENT_ID = $2`,
			existingApp, existingSession, fixtureDeployment)
		migrationharness.AssertQueryValue(t, db,
			`SELECT CHECKPOINT_ID FROM "SSO_SESSION_CONTEXT" WHERE SESSION_ID = $1`, "checkpoint-1", existingSession)
		t.Log("verified: tables, views, columns, check, indexes and the live session after expand")
	})
}

// verifyPreviousBinarySessionStatements runs the statements the previous binary's session store issues, against
// the legacy view names, and checks that the new representation stays consistent.
func verifyPreviousBinarySessionStatements(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("previous_binary_session_statements", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		const created = "0199a123-4810-7abc-8def-0123456789ab"

		insertSession := `INSERT INTO "SSO_SESSION" (SESSION_ID, DEPLOYMENT_ID, SUBJECT_ID, FLOW_ID, FLOW_VERSION, ` +
			`FLOW_EXECUTION_ID, HANDLE_ID, AUTHENTICATED_AT, CREATED_AT, LAST_ACTIVE_AT, IDLE_EXPIRES_AT, ` +
			`ABSOLUTE_EXPIRES_AT, STATE, VERSION) VALUES ($1, $2, 'subject-2', $3, 1, 'execution-2', $4, ` +
			`NOW(), NOW(), NOW(), NOW() + INTERVAL '1 hour', NOW() + INTERVAL '1 day', 'ACTIVE', 1) ` +
			`ON CONFLICT (FLOW_EXECUTION_ID, DEPLOYMENT_ID) DO NOTHING`
		result, err := db.Exec(insertSession, created, fixtureDeployment, existingFlow, "handle-2")
		require.NoError(t, err)
		affected, _ := result.RowsAffected()
		require.EqualValues(t, 1, affected)
		migrationharness.AssertQueryValue(t, db, `SELECT TYPE FROM "SESSION" WHERE SESSION_ID = $1`, "SSO", created)

		result, err = db.Exec(insertSession, "duplicate", fixtureDeployment, existingFlow, "handle-3")
		require.NoError(t, err)
		affected, _ = result.RowsAffected()
		require.EqualValues(t, 0, affected, "the flow execution conflict target converges on one session")

		mustExec(t, db, `INSERT INTO "SSO_SESSION_PARTICIPANT" (SESSION_ID, DEPLOYMENT_ID, APP_ID, FIRST_JOINED_AT, `+
			`LAST_ACTIVE_AT, TFID) VALUES ($1, $2, $3, NOW(), NOW(), 'tfid-a') `+
			`ON CONFLICT (SESSION_ID, DEPLOYMENT_ID, APP_ID) DO UPDATE SET LAST_ACTIVE_AT = EXCLUDED.LAST_ACTIVE_AT, `+
			`TFID = EXCLUDED.TFID`, created, fixtureDeployment, existingApp)
		mustExec(t, db, `INSERT INTO "SSO_SESSION_PARTICIPANT" (SESSION_ID, DEPLOYMENT_ID, APP_ID, FIRST_JOINED_AT, `+
			`LAST_ACTIVE_AT, TFID) VALUES ($1, $2, $3, NOW(), NOW(), 'tfid-b') `+
			`ON CONFLICT (SESSION_ID, DEPLOYMENT_ID, APP_ID) DO UPDATE SET LAST_ACTIVE_AT = EXCLUDED.LAST_ACTIVE_AT, `+
			`TFID = EXCLUDED.TFID`, created, fixtureDeployment, existingApp)
		migrationharness.AssertQueryValue(t, db,
			`SELECT TFID || ':' || OU_ID FROM "SESSION_PARTICIPANT" WHERE SESSION_ID = $1 AND APP_ID = $2`,
			"tfid-b:", created, existingApp)

		// The new binary writes the type itself. A direct session stays invisible to the previous binary, and a
		// value outside SSO and DIRECT is refused.
		mustExec(t, db, `INSERT INTO "SESSION" (SESSION_ID, DEPLOYMENT_ID, TYPE, SUBJECT_ID, FLOW_ID, FLOW_VERSION, `+
			`FLOW_EXECUTION_ID, HANDLE_ID, AUTHENTICATED_AT, LAST_ACTIVE_AT, STATE, VERSION) `+
			`VALUES ('direct-1', $1, 'DIRECT', 'subject-3', $2, 1, 'execution-3', 'handle-4', NOW(), NOW(), `+
			`'ACTIVE', 1)`,
			fixtureDeployment, existingFlow)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM "SSO_SESSION" WHERE SESSION_ID = 'direct-1'`, "0")
		_, err = db.Exec(`INSERT INTO "SESSION" (SESSION_ID, DEPLOYMENT_ID, TYPE, SUBJECT_ID, FLOW_ID, FLOW_VERSION, `+
			`FLOW_EXECUTION_ID, HANDLE_ID, AUTHENTICATED_AT, LAST_ACTIVE_AT, STATE, VERSION) `+
			`VALUES ('bad-type', $1, 'OTHER', 'subject-3', $2, 1, 'execution-4', 'handle-5', NOW(), NOW(), `+
			`'ACTIVE', 1)`,
			fixtureDeployment, existingFlow)
		require.Error(t, err)
		require.Contains(t, err.Error(), "SESSION_type_check")

		mustExec(t, db, `DELETE FROM "SSO_SESSION_PARTICIPANT" WHERE SESSION_ID = $1`, created)
		mustExec(t, db, `DELETE FROM "SSO_SESSION" WHERE SESSION_ID = $1`, created)
		mustExec(t, db, `DELETE FROM "SESSION" WHERE SESSION_ID = 'direct-1'`)
		migrationharness.AssertQueryValue(t, db, `SELECT COUNT(*)::TEXT FROM "SESSION"`, "1")
		t.Log("verified: previous binary statements work through the views and the type stays correct")
	})
}

// verifyContractRefusedWhileCleanupRoutineIsStale checks that contract does not drop the views while a routine
// of the source release still reads them.
func verifyContractRefusedWhileCleanupRoutineIsStale(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("contract_refused_with_stale_cleanup_routine", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		mustExec(t, db, environment.SourceFile(t, "dbscripts/runtime_persistent/postgres-cleanup.sql"))
		checkOutput := environment.RunMigrationCommandExpectFailure(t, "check contract")
		require.Contains(t, checkOutput, "cleanup_expired_runtime_persistent_data")
		require.Contains(t, checkOutput, "postgres-cleanup.sql")
		t.Log("verified: contract is refused while a routine still reads the legacy views")
	})
}

// verifyFinalizedSessionTables checks the session schema once contract has removed the compatibility views.
func verifyFinalizedSessionTables(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("finalized_session_tables", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db, relationKinds(),
			"SESSION=r,SESSION_PARTICIPANT=r,SSO_SESSION_CONTEXT=r")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COALESCE(COLUMN_DEFAULT, 'none') FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SESSION' AND COLUMN_NAME = 'type'`, "none")
		migrationharness.AssertQueryValue(t, db,
			`SELECT string_agg(UPPER(a.attname), ',' ORDER BY k.ordinality)
			FROM pg_constraint c INNER JOIN pg_class t ON t.oid = c.conrelid
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			WHERE t.relname = 'SESSION_PARTICIPANT' AND c.contype = 'p'`,
			"SESSION_ID,DEPLOYMENT_ID,APP_ID,OU_ID")
		_, err := db.Exec(`INSERT INTO "SESSION" (SESSION_ID, DEPLOYMENT_ID, SUBJECT_ID, FLOW_ID, FLOW_VERSION, `+
			`FLOW_EXECUTION_ID, HANDLE_ID, AUTHENTICATED_AT, LAST_ACTIVE_AT, STATE, VERSION) `+
			`VALUES ('untyped', $1, 'subject-9', $2, 1, 'execution-9', 'handle-9', NOW(), NOW(), 'ACTIVE', 1)`,
			fixtureDeployment, existingFlow)
		require.Error(t, err, "a session must now name its type")
		migrationharness.AssertQueryValue(t, db,
			`SELECT TYPE FROM "SESSION" WHERE SESSION_ID = $1`, "SSO", existingSession)
		migrationharness.AssertOperationCompleted(t, db, sessionOperationPrefix+"56_finalize_session_tables")
		t.Log("verified: contract removed the views and the default, and widened the participant key")
	})
}
