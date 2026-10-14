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
	t.Log("verified: additional configuration tables match the required schemas")
}
