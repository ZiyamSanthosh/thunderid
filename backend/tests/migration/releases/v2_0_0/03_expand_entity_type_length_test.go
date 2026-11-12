// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyExpandEntityTypeLength(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("03_expand_entity_type_length", func(t *testing.T) {
		db := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT CHARACTER_MAXIMUM_LENGTH FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY' AND COLUMN_NAME = 'type'`,
			"100")
		migrationharness.AssertQueryValue(t, db,
			`SELECT TYPE FROM "ENTITY" WHERE ID = '0199a123-4703-7abc-8def-0123456789ab'`,
			"legacy-person")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/entitydb/03_expand_entity_type_length")
		t.Log("verified: ENTITY.TYPE accepts 100 characters before the later reference migration")
	})
}
