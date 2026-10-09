// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyFinalizeEntityTypeHandle(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("07_finalize_entity_type_handle", func(t *testing.T) {
		environment.RunMigrationCommand(t, "apply contract")
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_TYPES' AND COLUMN_NAME = 'name'`,
			"1")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
                WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_TYPES'
					AND COLUMN_NAME = 'handle' AND IS_NULLABLE = 'NO'`,
			"1")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_constraint c
                INNER JOIN pg_class t ON t.oid = c.conrelid
                INNER JOIN pg_namespace n ON n.oid = t.relnamespace
                WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES'
                    AND c.contype = 'u' AND c.conname = 'UQ_ENTITY_TYPES_HANDLE_CATEGORY_DEPLOYMENT'`,
			"1")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/configdb/07_finalize_entity_type_handle")
		t.Log("verified: entity-type contract retained NAME and enforced required unique handles")
	})
}
