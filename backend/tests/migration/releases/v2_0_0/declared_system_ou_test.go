// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func TestPostgresDeclaredSystemOrganizationUnitUpgrade(t *testing.T) {
	runDeclaredSystemOrganizationUnitUpgrade(t, "config.yaml")
}

func TestPostgresSharedDatabaseDeclaredSystemOrganizationUnitUpgrade(t *testing.T) {
	runDeclaredSystemOrganizationUnitUpgrade(t, "config-shared.yaml")
}

// runDeclaredSystemOrganizationUnitUpgrade upgrades an installation whose system organization unit is a declared
// resource and not a row in the database. The source installation stores it as the quick start does, so the row is
// removed and the same organization unit is declared in a resource file, which is the layout of an installation
// that keeps its resources declarative.
func runDeclaredSystemOrganizationUnitUpgrade(t *testing.T, configFile string) {
	t.Helper()
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), configFile))
	environment.RunPreviousServerBootstrap(t)

	entityDB := environment.OpenDatabase(t, "entitydb")
	result, err := entityDB.Exec(`DELETE FROM "ORGANIZATION_UNIT" WHERE OU_ID = $1`, systemOrganizationUnit)
	require.NoError(t, err)
	removed, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, removed, "the bootstrap creates the system organization unit as a database row")

	environment = environment.WithDeclaredOrganizationUnit(t, systemOrganizationUnit, "default", "Default")

	environment.RunMigrationCommand(t, "apply expand")
	reloadRuntimePersistentCleanupRoutine(t, environment)
	environment.RunMigrationCommand(t, "apply migrate")
	environment.RunMigrationCommand(t, "apply contract")

	configDB := environment.OpenDatabase(t, "configdb")
	for _, table := range []string{"FLOW", "IDP", "NOTIFICATION_SENDER"} {
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT COUNT(*)::TEXT FROM "`+table+`" WHERE OU_ID IS DISTINCT FROM $1`, "0", systemOrganizationUnit)
	}
	migrationharness.AssertQueryValue(t, configDB,
		`SELECT (COUNT(*) > 0)::TEXT FROM "FLOW" WHERE OU_ID = $1`, "true", systemOrganizationUnit)
	migrationharness.AssertQueryValue(t, entityDB,
		`SELECT COUNT(*)::TEXT FROM "ORGANIZATION_UNIT" WHERE OU_ID = $1`, "0", systemOrganizationUnit)

	target := environment.StartTargetServer(t)
	target.WaitReady(t)
	target.Authenticate(t, bootstrapAdminUsername, bootstrapAdminPassword)
	target.RequireNoErrorLogs(t)
	t.Log("verified: a declared system organization unit owns the migrated rows and the target server serves them")
}
