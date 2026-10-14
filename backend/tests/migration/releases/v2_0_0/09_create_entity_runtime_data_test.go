// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyCreateEntityRuntimeData(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("09_create_entity_runtime_data", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_RUNTIME_DATA'`, "7")
		migrationharness.AssertQueryValue(t, db,
			`SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
				FROM pg_constraint c
				INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
				CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
				WHERE table_class.relname = 'ENTITY_RUNTIME_DATA' AND c.contype = 'p'
				GROUP BY c.oid`, "DEPLOYMENT_ID,ENTITY_ID")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/runtime_persistent/09_create_entity_runtime_data")
		t.Log("verified: ENTITY_RUNTIME_DATA matches the required runtime-persistent schema")
	})
}
