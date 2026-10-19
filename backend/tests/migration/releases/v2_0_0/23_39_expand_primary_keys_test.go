// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"strings"
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

type deploymentScopedPrimaryKeyOperation struct {
	store   string
	number  string
	table   string
	columns string
}

var deploymentScopedPrimaryKeyOperations = []deploymentScopedPrimaryKeyOperation{
	{store: "configdb", number: "23", table: "ENTITY_TYPES", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "24", table: "ROLE", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "25", table: "THEME", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "26", table: "LAYOUT", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "27", table: "INBOUND_CLIENT", columns: "DEPLOYMENT_ID,ENTITY_ID"},
	{store: "configdb", number: "28", table: "IDP", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "29", table: "NOTIFICATION_SENDER", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "30", table: "CERTIFICATE", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "31", table: "RESOURCE_SERVER", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "32", table: "RESOURCE", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "33", table: "ACTION", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "34", table: "FLOW", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "35", table: "PRESENTATION_DEFINITION", columns: "DEPLOYMENT_ID,ID"},
	{store: "configdb", number: "36", table: "CREDENTIAL_CONFIGURATION", columns: "DEPLOYMENT_ID,ID"},
	{store: "entitydb", number: "37", table: "ORGANIZATION_UNIT", columns: "DEPLOYMENT_ID,OU_ID"},
	{store: "entitydb", number: "38", table: "ENTITY", columns: "DEPLOYMENT_ID,ID"},
	{store: "entitydb", number: "39", table: "GROUP", columns: "DEPLOYMENT_ID,ID"},
}

func verifyLegacyThemeLayoutForeignKeys(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	db := environment.OpenDatabase(t, "configdb")
	for _, parentTable := range []string{"THEME", "LAYOUT"} {
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM pg_constraint constraint_def
				INNER JOIN pg_class child_table ON child_table.oid = constraint_def.conrelid
				INNER JOIN pg_class parent_table ON parent_table.oid = constraint_def.confrelid
				WHERE constraint_def.contype = 'f'
				AND child_table.relname = 'INBOUND_CLIENT' AND parent_table.relname = $1`,
			"1", parentTable)
	}
}

func verifyDeploymentScopedPrimaryKeys(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	for _, operation := range deploymentScopedPrimaryKeyOperations {
		t.Run(operation.number+"_"+operation.table, func(t *testing.T) {
			db := environment.OpenDatabase(t, operation.store)
			migrationharness.AssertQueryValue(t, db, `SELECT string_agg(UPPER(a.attname), ',' ORDER BY p.ordinality)
				FROM pg_constraint c
				INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
				INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
				CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS p(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = p.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1 AND c.contype = 'p'`,
				operation.columns, operation.table)
			migrationharness.AssertOperationCompleted(t, db,
				"2.0.0/"+operation.store+"/"+operation.number+"_expand_"+
					strings.ToLower(operation.table)+"_primary_key")
			migrationharness.AssertQueryValue(t, db,
				`SELECT COUNT(*) FROM pg_indexes
					WHERE schemaname = CURRENT_SCHEMA() AND tablename = $1 AND indexname = $2`,
				"1", operation.table, legacyPrimaryKeyIndexName(operation.table))
		})
	}
	db := environment.OpenDatabase(t, "configdb")
	for _, parentTable := range []string{"THEME", "LAYOUT"} {
		migrationharness.AssertQueryValue(t, db,
			`SELECT COUNT(*)::TEXT FROM pg_constraint constraint_def
				INNER JOIN pg_class child_table ON child_table.oid = constraint_def.conrelid
				INNER JOIN pg_class parent_table ON parent_table.oid = constraint_def.confrelid
				WHERE constraint_def.contype = 'f'
				AND child_table.relname = 'INBOUND_CLIENT' AND parent_table.relname = $1`,
			"0", parentTable)
	}
	migrationharness.AssertQueryValue(t, db,
		`SELECT THEME_ID || ':' || LAYOUT_ID FROM "INBOUND_CLIENT"
			WHERE DEPLOYMENT_ID = 'migration-deployment'
			AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`,
		"0199a123-4780-7abc-8def-0123456789ab:0199a123-4781-7abc-8def-0123456789ab")
	t.Log("verified: deployment-scoped primary keys retain legacy uniqueness for mixed-version serving")
}

func verifyFinalizedDeploymentScopedPrimaryKeys(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	for _, operation := range deploymentScopedPrimaryKeyOperations {
		t.Run(operation.number+"_finalize_"+operation.table, func(t *testing.T) {
			db := environment.OpenDatabase(t, operation.store)
			migrationharness.AssertQueryValue(t, db,
				`SELECT COUNT(*) FROM pg_indexes
					WHERE schemaname = CURRENT_SCHEMA() AND tablename = $1 AND indexname = $2`,
				"0", operation.table, legacyPrimaryKeyIndexName(operation.table))
			migrationharness.AssertOperationCompleted(t, db,
				"2.0.0/"+operation.store+"/"+operation.number+"_finalize_"+
					strings.ToLower(operation.table)+"_primary_key")
		})
	}
	t.Log("verified: contract removed legacy global uniqueness after source drain")
}

func legacyPrimaryKeyIndexName(table string) string {
	return "idx_" + strings.ToLower(table) + "_legacy_primary_key_unique"
}
