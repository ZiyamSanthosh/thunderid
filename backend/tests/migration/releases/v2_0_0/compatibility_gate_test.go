// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyCompatibilityGate(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("compatibility_gate", func(t *testing.T) {
		statusOutput := environment.RunMigrationCommandExpectFailure(t, "status")
		require.Contains(t, statusOutput,
			"database stores are not compatible with this ThunderID release")

		startupOutput := environment.RunServerExpectCompatibilityFailure(t)
		require.Contains(t, startupOutput,
			"ThunderID cannot start because its databases are not compatible with this release")

		verifyLegacyThemeLayoutForeignKeys(t, environment)
		environment.RunMigrationCommand(t, "check")
		environment.RunMigrationCommand(t, "apply expand")
		preparePostExpandMigrationSource(t, environment)
		verifyAgentNameCollisionGuard(t, environment)

		statusOutput = environment.RunMigrationCommand(t, "status")
		require.Contains(t, statusOutput, "Overall compatibility: YES",
			"successful migration status did not report compatibility")

		environment.RunMigrationCommand(t, "check contract")
		t.Log("verified: status and normal startup reject source-level databases, and status accepts migrated databases")
	})
}
