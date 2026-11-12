// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"path/filepath"
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const (
	bootstrapAdminUsername = "admin"
	bootstrapAdminPassword = "Adm1n-Pass-123!"
)

func TestPostgresRealServerUpgrade(t *testing.T) {
	runRealServerUpgrade(t, "config.yaml")
}

func TestPostgresSharedDatabaseRealServerUpgrade(t *testing.T) {
	runRealServerUpgrade(t, "config-shared.yaml")
}

// runRealServerUpgrade upgrades a real installation of the source release with real servers: the source
// release's own server serves through the expand phase, and the server built from this checkout serves once the
// migration is complete. Signing a user in reads the users and their credentials, so it fails when a server
// cannot work against the schema it finds.
func runRealServerUpgrade(t *testing.T, configFile string) {
	t.Helper()
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), configFile))
	environment.RunPreviousServerBootstrap(t)

	previous := environment.StartPreviousServer(t)
	previous.WaitReady(t)
	previous.Authenticate(t, bootstrapAdminUsername, bootstrapAdminPassword)

	environment.RunMigrationCommand(t, "apply expand")

	// The source server has kept running while the databases expanded, and has to keep working against them.
	previous.WaitReady(t)
	previous.Authenticate(t, bootstrapAdminUsername, bootstrapAdminPassword)
	previous.RequireNoErrorLogs(t)

	// A source server that starts against the expanded databases works too, which is what a restart or a
	// second instance of it does while the migration waits to be finished.
	previous.Stop(t)
	previous = environment.StartPreviousServer(t)
	previous.WaitReady(t)
	previous.Authenticate(t, bootstrapAdminUsername, bootstrapAdminPassword)
	previous.RequireNoErrorLogs(t)

	previous.Stop(t)
	reloadRuntimePersistentCleanupRoutine(t, environment)
	environment.RunMigrationCommand(t, "apply migrate")
	environment.RunMigrationCommand(t, "apply contract")

	target := environment.StartTargetServer(t)
	target.WaitReady(t)
	target.Authenticate(t, bootstrapAdminUsername, bootstrapAdminPassword)
	target.RequireNoErrorLogs(t)
	t.Log("verified: the source server served through expand, and the target server serves the migrated databases")
}
