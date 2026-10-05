// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func TestPostgresMigration(t *testing.T) {
	runPostgresMigrationSuite(t, "config.yaml")
}

func TestPostgresSharedDatabaseMigration(t *testing.T) {
	runPostgresMigrationSuite(t, "config-shared.yaml")
}

func runPostgresMigrationSuite(t *testing.T, configFile string) {
	t.Helper()
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), configFile))
	verifyCompatibilityGate(t, environment)
	verifyRemoveRevocationReasonCheck(t, environment)
	verifyCreateAuthorizationPDPConnection(t, environment)
	verifyReleaseState(t, environment)
	verifyResumeAfterFinalizationFailure(t, environment)
	verifyResumeAbandonedRun(t, environment)
	verifyConcurrentMigrationRejected(t, environment)
}

func verifyReleaseState(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("release_state", func(t *testing.T) {
		expectedLevels := []struct {
			database string
			level    string
		}{
			{database: "configdb", level: "2.0.0:clean"},
			{database: "entitydb", level: "1.0.0:clean"},
			{database: "runtime_transient", level: "1.0.0:clean"},
			{database: "runtime_persistent", level: "2.0.0:clean"},
		}
		for _, expected := range expectedLevels {
			db := environment.OpenDatabase(t, expected.database)
			migrationharness.AssertQueryValue(t, db,
				`SELECT COMPLETED_LEVEL || ':' || STATUS FROM "MIGRATION_STATE" WHERE STORE_ID = $1`,
				expected.level,
				expected.database,
			)
		}
		t.Log("verified: every logical store has the expected clean migration level")
	})
}

func releaseTestDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filename)
}
