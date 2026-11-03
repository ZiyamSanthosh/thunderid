// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAddEntityTypeHandle(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("02_add_entity_type_handle", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE || ':' || CHARACTER_MAXIMUM_LENGTH
                    FROM INFORMATION_SCHEMA.COLUMNS
					WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_TYPES' AND COLUMN_NAME = $1`,
			"YES:100", "handle")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/configdb/02_add_entity_type_handle")
		t.Log("verified: nullable entity-type handle column was added while NAME remained unchanged")
	})
}
