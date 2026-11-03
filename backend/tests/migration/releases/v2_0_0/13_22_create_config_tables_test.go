// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"fmt"
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyCreateAdditionalConfigTables(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	db := environment.OpenDatabase(t, "configdb")
	tables := []struct {
		name        string
		columnCount int
	}{
		{name: "VARIABLE", columnCount: 6},
		{name: "SECRET", columnCount: 6},
		{name: "GATEWAY", columnCount: 9},
		{name: "CONFIGURATION_VERSION", columnCount: 9},
		{name: "GATEWAY_APPLIED_VERSION", columnCount: 5},
		{name: "GATEWAY_EXCLUDED_RESOURCE", columnCount: 6},
		{name: "RESOURCE_SHARING_POLICY", columnCount: 12},
		{name: "RESOURCE_SHARING_POLICY_UNIT", columnCount: 5},
		{name: "RESOURCE_SHARING_POLICY_OVERLAY_RULE", columnCount: 8},
		{name: "RESOURCE_OVERLAY_VALUE", columnCount: 8},
	}

	for _, table := range tables {
		t.Run(table.name, func(t *testing.T) {
			migrationharness.AssertQueryValue(t, db,
				fmt.Sprintf(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
					WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = '%s'`, table.name),
				fmt.Sprintf("%d", table.columnCount))
		})
	}

	for _, operationID := range []string{
		"2.0.0/configdb/13_create_variable",
		"2.0.0/configdb/14_create_secret",
		"2.0.0/configdb/15_create_gateway",
		"2.0.0/configdb/16_create_configuration_version",
		"2.0.0/configdb/17_create_gateway_applied_version",
		"2.0.0/configdb/18_create_gateway_excluded_resource",
		"2.0.0/configdb/19_create_resource_sharing_policy",
		"2.0.0/configdb/20_create_resource_sharing_policy_unit",
		"2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule",
		"2.0.0/configdb/22_create_resource_overlay_value",
	} {
		migrationharness.AssertOperationCompleted(t, db, operationID)
	}

	for _, key := range []struct {
		table   string
		columns string
	}{
		{table: "RESOURCE_SHARING_POLICY", columns: "DEPLOYMENT_ID,ID"},
		{table: "RESOURCE_SHARING_POLICY_UNIT", columns: "DEPLOYMENT_ID,POLICY_ID,OU_ID"},
		{table: "RESOURCE_SHARING_POLICY_OVERLAY_RULE", columns: "DEPLOYMENT_ID,ID"},
	} {
		migrationharness.AssertQueryValue(t, db,
			`SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
				FROM pg_constraint c
				INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
				INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
				CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1 AND c.contype = 'p'`,
			key.columns, key.table)
	}

	for _, table := range []string{
		"RESOURCE_SHARING_POLICY_UNIT",
		"RESOURCE_SHARING_POLICY_OVERLAY_RULE",
	} {
		migrationharness.AssertQueryValue(t, db,
			`SELECT
				(SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
				 FROM unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				 INNER JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = key_position.attnum)
				|| '->' ||
				(SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
				 FROM unnest(c.confkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				 INNER JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = key_position.attnum)
				FROM pg_constraint c
				INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
				INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
				WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1 AND c.contype = 'f'`,
			"DEPLOYMENT_ID,POLICY_ID->DEPLOYMENT_ID,ID", table)
	}

	for _, index := range []struct {
		name     string
		expected string
	}{
		{name: "idx_rsp_parent", expected: "false:DEPLOYMENT_ID,PARENT_POLICY_ID:false"},
		{name: "idx_rspu_ou", expected: "false:DEPLOYMENT_ID,OU_ID:false"},
		{name: "idx_rspor_policy", expected: "false:DEPLOYMENT_ID,POLICY_ID:false"},
		{name: "idx_rspor_blanket_once", expected: "true:DEPLOYMENT_ID,POLICY_ID,SECTION,FIELD_KEY:true"},
	} {
		migrationharness.AssertQueryValue(t, db,
			`SELECT i.indisunique::TEXT || ':' ||
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) || ':' ||
				(i.indpred IS NOT NULL)::TEXT
				FROM pg_class index_class
				INNER JOIN pg_namespace n ON n.oid = index_class.relnamespace
				INNER JOIN pg_index i ON i.indexrelid = index_class.oid
				INNER JOIN pg_class table_class ON table_class.oid = i.indrelid
				CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND index_class.relname = $1
				GROUP BY i.indisunique, i.indpred`,
			index.expected, index.name)
	}
	t.Log("verified: additional configuration tables match the required schemas")
}
