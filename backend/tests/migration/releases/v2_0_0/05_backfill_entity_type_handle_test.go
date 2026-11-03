// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyBackfillEntityTypeHandle(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("05_backfill_entity_type_handle", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT HANDLE || ':' || NAME FROM "ENTITY_TYPES"
                WHERE ID = '0199a123-4701-7abc-8def-0123456789ab'`,
			"legacy-person:"+legacyEntityTypeName)
		migrationharness.AssertQueryValue(t, db,
			`SELECT HANDLE FROM "ENTITY_TYPES"
				WHERE ID = '0199a123-4704-7abc-8def-0123456789ab'`,
			"legacy-person_1")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES" WHERE HANDLE IS NOT NULL`,
			"101")
		migrationharness.AssertQueryValue(t, db,
			`SELECT CHECKPOINT_DATA::jsonb ->> 'lastID' FROM "MIGRATION_PROGRESS"
				WHERE OPERATION_ID = $1`,
			"0199a123-4898-7abc-8def-0123456789ab",
			"2.0.0/configdb/05_backfill_entity_type_handle")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/configdb/05_backfill_entity_type_handle")
		t.Log("verified: 101 names were converted in two batches with a durable final checkpoint")
	})
}
