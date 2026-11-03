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

func TestPostgresDeploymentSelectiveMigration(t *testing.T) {
	environment := migrationharness.Start(t, filepath.Join(releaseTestDirectory(t), "config.yaml"))
	db := environment.OpenDatabase(t, "configdb")
	_, err := db.Exec(`INSERT INTO "ENTITY_TYPES"
		(DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
		VALUES
			('deployment-a', '0199a123-4901-7abc-8def-0123456789ab', 'user', 'Alpha User',
				'0199a123-4902-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
			('deployment-b', '0199a123-4903-7abc-8def-0123456789ab', 'user', 'Beta User',
				'0199a123-4904-7abc-8def-0123456789ab', FALSE, '{}'::jsonb)`)
	require.NoError(t, err)

	environment.RunMigrationCommand(t, "apply expand")
	environment.RunMigrationCommand(t, "apply migrate --deployment-id deployment-a")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'deployment-a' AND HANDLE IS NOT NULL`, "1")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'deployment-b' AND HANDLE IS NOT NULL`, "1")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COMPLETED_LEVEL FROM "MIGRATION_DEPLOYMENT_STATE" WHERE DEPLOYMENT_ID = 'deployment-a'`,
		"2.0.0-m1-pidp")
	migrationharness.AssertQueryValue(t, db,
		`SELECT STAGE || ':' || STATUS FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"expanded:completed")

	// Store-wide execution must accept the mixed state left by the selective migration and finish
	// every remaining deployment.
	environment.RunMigrationCommand(t, "apply migrate")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "ENTITY_TYPES"
			WHERE DEPLOYMENT_ID = 'deployment-b' AND HANDLE IS NOT NULL`, "1")
	migrationharness.AssertQueryValue(t, db,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_DEPLOYMENT_STATE" WHERE COMPLETED_LEVEL = '2.0.0-m1-pidp'`, "1")
	migrationharness.AssertQueryValue(t, db,
		`SELECT STAGE || ':' || STATUS FROM "MIGRATION_STATE" WHERE STORE_ID = 'configdb'`,
		"contract_ready:completed")
	t.Log("verified: store-wide migration completes the mixed state left by a selective migration")
}
