// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyRemoveEntityState(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("11_remove_entity_state", func(t *testing.T) {
		db := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY' AND COLUMN_NAME = 'state'`, "0")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM "ENTITY" WHERE DEPLOYMENT_ID = 'migration-deployment'`, "2")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/entitydb/11_remove_entity_state")
		t.Log("verified: contract removed ENTITY.STATE without losing entity rows")
	})
}
