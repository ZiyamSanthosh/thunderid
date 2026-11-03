// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyCreateNotificationTemplate(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("12_create_notification_template", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'NOTIFICATION_TEMPLATE'`, "11")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_constraint c
				INNER JOIN pg_class t ON t.oid = c.conrelid
				INNER JOIN pg_namespace n ON n.oid = t.relnamespace
				WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'NOTIFICATION_TEMPLATE'
				AND c.contype IN ('p', 'u')`, "2")
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/configdb/12_create_notification_template")
		t.Log("verified: NOTIFICATION_TEMPLATE matches the required configuration schema")
	})
}
