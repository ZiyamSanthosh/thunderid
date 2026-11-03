// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAddSSOSessionParticipantApplicationIndex(
	t *testing.T,
	environment migrationharness.Environment,
) {
	t.Helper()
	t.Run("42_add_sso_session_participant_application_index", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db,
			`SELECT i.indisunique::TEXT || ':' || i.indisvalid::TEXT || ':' ||
				i.indisready::TEXT || ':' || am.amname || ':' ||
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) || ':' ||
				(i.indpred IS NOT NULL)::TEXT
				FROM pg_class index_class
				INNER JOIN pg_namespace n ON n.oid = index_class.relnamespace
				INNER JOIN pg_index i ON i.indexrelid = index_class.oid
				INNER JOIN pg_am am ON am.oid = index_class.relam
				INNER JOIN pg_class table_class ON table_class.oid = i.indrelid
				CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a
					ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA()
					AND table_class.relname = 'SSO_SESSION_PARTICIPANT'
					AND index_class.relname = 'idx_sso_session_participant_app'
				GROUP BY i.indisunique, i.indisvalid, i.indisready, am.amname, i.indpred`,
			"false:true:true:btree:DEPLOYMENT_ID,APP_ID:false")
		migrationharness.AssertQueryValue(t, db,
			`SELECT APP_ID FROM "SSO_SESSION_PARTICIPANT"
				WHERE SESSION_ID = '0199a123-4796-7abc-8def-0123456789ab'
					AND DEPLOYMENT_ID = 'migration-deployment'`,
			"0199a123-4795-7abc-8def-0123456789ab")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/runtime_persistent/42_add_sso_session_participant_application_index")
		t.Log("verified: the concurrent participant application index is exact and preserves existing rows")
	})
}
