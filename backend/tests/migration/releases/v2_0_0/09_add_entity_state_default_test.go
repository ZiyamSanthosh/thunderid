// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAddEntityStateDefault(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("09_add_entity_state_default", func(t *testing.T) {
		db := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COLUMN_DEFAULT FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY' AND COLUMN_NAME = 'state'`,
			"'ACTIVE'::character varying")
		_, err := db.Exec(`INSERT INTO "ENTITY"
			(DEPLOYMENT_ID, ID, CATEGORY, TYPE, OU_ID, CREATED_AT, UPDATED_AT)
			VALUES ('migration-deployment', '0199a123-4710-7abc-8def-0123456789ab', 'user', 'person',
				'0199a123-4702-7abc-8def-0123456789ab', NOW(), NOW())`)
		require.NoError(t, err, "insert entity without STATE after compatibility expansion")
		migrationharness.AssertQueryValue(t, db,
			`SELECT STATE FROM "ENTITY" WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4710-7abc-8def-0123456789ab'`, "ACTIVE")
		migrationharness.AssertOperationCompleted(t, db, "2.0.0/entitydb/09_add_entity_state_default")
		t.Log("verified: target-style entity inserts receive the temporary ACTIVE state default")
	})
}
