// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyAgentNameCollisionGuard(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	output := environment.RunMigrationCommandExpectFailure(t, "apply migrate")
	require.Contains(t, output, "colliding name values")

	entityDB := environment.OpenDatabase(t, "entitydb")
	migrationharness.AssertQueryValue(t, entityDB,
		`SELECT COUNT(*)::TEXT FROM "MIGRATION_PROGRESS" WHERE OPERATION_ID = $1`,
		"0", "2.0.0/entitydb/49_migrate_agent_names_to_attributes")
	_, err := entityDB.Exec(`UPDATE "ENTITY"
		SET ATTRIBUTES = jsonb_set(ATTRIBUTES, '{name}', '"Different User"'::jsonb)
		WHERE DEPLOYMENT_ID = 'migration-deployment'
		AND ID = '0199a123-4714-7abc-8def-0123456789ab'`)
	require.NoError(t, err)

	environment.RunMigrationCommand(t, "resume")
	t.Log("verified: an agent-name collision blocks migrate, while duplicate non-agent names do not")
}

func verifyMigrateAgentNames(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("48_49_migrate_agent_names", func(t *testing.T) {
		configDB := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT SCHEMA_DEF -> 'name' ->> 'type' FROM "ENTITY_TYPES"
				WHERE DEPLOYMENT_ID = 'migration-deployment' AND CATEGORY = 'agent' AND HANDLE = 'default'`,
			"string")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT SCHEMA_DEF -> 'name' ->> 'required' FROM "ENTITY_TYPES"
				WHERE DEPLOYMENT_ID = 'migration-deployment' AND CATEGORY = 'agent' AND HANDLE = 'default'`,
			"true")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT SCHEMA_DEF -> 'name' ->> 'unique' FROM "ENTITY_TYPES"
				WHERE DEPLOYMENT_ID = 'migration-deployment' AND CATEGORY = 'agent' AND HANDLE = 'default'`,
			"true")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT SYSTEM_ATTRIBUTES ->> 'display' FROM "ENTITY_TYPES"
				WHERE DEPLOYMENT_ID = 'migration-deployment' AND CATEGORY = 'agent' AND HANDLE = 'default'`,
			"name")
		migrationharness.AssertOperationCompleted(t, configDB,
			"2.0.0/configdb/48_migrate_default_agent_name_schema")

		entityDB := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT ATTRIBUTES ->> 'name' FROM "ENTITY"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4706-7abc-8def-0123456789ab'`,
			"Calendar Agent")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT ATTRIBUTES ->> 'modelProvider' FROM "ENTITY"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4706-7abc-8def-0123456789ab'`,
			"openai")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT SYSTEM_ATTRIBUTES ->> 'description' FROM "ENTITY"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4706-7abc-8def-0123456789ab'`,
			"Schedules meetings")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM "ENTITY"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4706-7abc-8def-0123456789ab'
				AND SYSTEM_ATTRIBUTES ? 'name'`,
			"0")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT SOURCE FROM "ENTITY_IDENTIFIER"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4706-7abc-8def-0123456789ab'
				AND NAME = 'name' AND VALUE = 'Calendar Agent'`,
			"attribute")
		migrationharness.AssertOperationCompleted(t, entityDB,
			"2.0.0/entitydb/49_migrate_agent_names_to_attributes")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM "ENTITY"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ATTRIBUTES ->> 'name' = 'Shared User Name'`,
			"2")
		t.Log("verified: default agent schema and existing agent names use schema attributes")
	})
}
