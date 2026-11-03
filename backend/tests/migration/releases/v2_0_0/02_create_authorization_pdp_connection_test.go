// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyCreateAuthorizationPDPConnection(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("02_create_authorization_pdp_connection", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'AUTHORIZATION_PDP_CONNECTION'`,
			"1",
		)
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'AUTHORIZATION_PDP_CONNECTION' AND indexname = 'idx_authorization_pdp_connection_type_name_deployment'`,
			"1",
		)
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/configdb/02_create_authorization_pdp_connection")
		t.Log("verified: authorization PDP connection table and index exist and operation progress is completed")
	})
}
