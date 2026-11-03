// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyExpandEntityIdentifierPrimaryKey(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("07_expand_entity_identifier_primary_key", func(t *testing.T) {
		db := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, db, `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
			FROM pg_constraint c
			INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = 'ENTITY_IDENTIFIER'
			AND c.contype = 'p'`, "ENTITY_ID,DEPLOYMENT_ID,NAME,VALUE")
		migrationharness.AssertQueryValue(t, db, `SELECT VALUE FROM "ENTITY_IDENTIFIER"
			WHERE ENTITY_ID = '0199a123-4703-7abc-8def-0123456789ab'
			AND NAME = 'username' AND DEPLOYMENT_ID = 'migration-deployment'`, "legacy-person")

		_, err := db.Exec(`INSERT INTO "ENTITY_IDENTIFIER"
			(DEPLOYMENT_ID, ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT)
			VALUES ('migration-deployment', '0199a123-4703-7abc-8def-0123456789ab',
				'username', 'legacy-person-alias', 'attribute', NOW())`)
		require.NoError(t, err)
		migrationharness.AssertQueryValue(t, db, `SELECT COUNT(*) FROM "ENTITY_IDENTIFIER"
			WHERE ENTITY_ID = '0199a123-4703-7abc-8def-0123456789ab'
			AND NAME = 'username' AND DEPLOYMENT_ID = 'migration-deployment'`, "2")
		migrationharness.AssertOperationCompleted(
			t, db, "2.0.0/entitydb/07_expand_entity_identifier_primary_key")
		t.Log("verified: ENTITY_IDENTIFIER accepts multiple values per entity and name without losing existing rows")
	})
}
