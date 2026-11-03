// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyRemoveRevocationReasonCheck(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("01_remove_revocation_reason_check", func(t *testing.T) {
		db := environment.OpenDatabase(t, "runtime_persistent")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_constraint WHERE conname = 'REVOKED_TOKEN_revocation_reason_check'`,
			"0",
		)
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/runtime_persistent/01_remove_revocation_reason_check")
		t.Log("verified: legacy revoked-token reason constraint is absent and operation progress is completed")
	})
}
