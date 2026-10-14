// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAddSSOSessionProperties(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("12_add_sso_session_properties", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE || ':' || DATA_TYPE || ':' ||
				CASE WHEN COLUMN_DEFAULT IS NULL THEN 'none' ELSE COLUMN_DEFAULT END
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SSO_SESSION'
				AND COLUMN_NAME = 'properties'`,
			"YES:jsonb:none")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/runtime_persistent/12_add_sso_session_properties")
		t.Log("verified: SSO_SESSION has the nullable JSONB properties column")
	})
}
