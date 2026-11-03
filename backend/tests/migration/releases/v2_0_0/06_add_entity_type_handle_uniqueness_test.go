// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAddEntityTypeHandleUniqueness(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("06_add_entity_type_handle_uniqueness", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_constraint c
                INNER JOIN pg_class t ON t.oid = c.conrelid
                INNER JOIN pg_namespace n ON n.oid = t.relnamespace
                WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES'
                    AND c.contype = 'u'`,
			"2")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_constraint c
                INNER JOIN pg_class t ON t.oid = c.conrelid
                INNER JOIN pg_namespace n ON n.oid = t.relnamespace
                WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES'
                    AND c.contype = 'u' AND c.conname = 'UQ_ENTITY_TYPES_HANDLE_CATEGORY_DEPLOYMENT'`,
			"1")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS
                WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_TYPES'
                    AND COLUMN_NAME = 'handle'`,
			"YES")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/configdb/06_add_entity_type_handle_uniqueness")
		t.Log("verified: source name uniqueness and target handle uniqueness coexist before contract")
	})
}
