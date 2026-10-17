// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyExpandRevocationCriterionValue(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("41_expand_revocation_criterion_value", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE || ':' || DATA_TYPE || ':' || CHARACTER_MAXIMUM_LENGTH
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'REVOCATION_CRITERIA'
					AND COLUMN_NAME = 'criterion_value'`,
			"NO:character varying:2048")
		migrationharness.AssertQueryValue(t, db,
			`SELECT CRITERION_VALUE FROM "REVOCATION_CRITERIA"
				WHERE ID = '0199a123-4797-7abc-8def-0123456789ab'`,
			legacyRevocationCriterionValue)

		longCriterionValue := strings.Repeat("r", 2048)
		_, err := db.Exec(`INSERT INTO "REVOCATION_CRITERIA"
			(DEPLOYMENT_ID, ID, CRITERION_TYPE, CRITERION_VALUE, REASON, REVOKED_AT, EXPIRY_TIME)
			VALUES ('migration-deployment', '0199a123-4794-7abc-8def-0123456789ab',
				'token_family', $1, 'test', NOW(), NOW() + INTERVAL '1 day')`, longCriterionValue)
		require.NoError(t, err)
		migrationharness.AssertQueryValue(t, db,
			`SELECT LENGTH(CRITERION_VALUE)::TEXT FROM "REVOCATION_CRITERIA"
				WHERE ID = '0199a123-4794-7abc-8def-0123456789ab'`, "2048")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/runtime_persistent/41_expand_revocation_criterion_value")
		t.Log("verified: revocation criterion values accept 2048 characters without changing existing values")
	})
}
