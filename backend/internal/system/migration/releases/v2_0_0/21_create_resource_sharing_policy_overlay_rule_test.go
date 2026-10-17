// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceSharingPolicyOverlayRuleOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceSharingPolicyOverlayRuleOperation,
		engine.OperationID("2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule"),
		resourceSharingPolicyOverlayRuleTable, 3)
}

func TestResourceSharingPolicyOverlayRuleUsesDeploymentScopedKeys(t *testing.T) {
	assert.Contains(t, resourceSharingPolicyOverlayRuleTable.keys,
		configKeySpec{typeCode: "p", columns: "deployment_id,id"})
	assert.Contains(t, resourceSharingPolicyOverlayRuleTable.keys, configKeySpec{
		typeCode: "f", columns: "deployment_id,policy_id", referencedTable: "resource_sharing_policy",
		referencedColumns: "deployment_id,id", deleteAction: "c",
	})
	assert.Contains(t, resourceSharingPolicyOverlayRuleTable.keys,
		configKeySpec{typeCode: "u", columns: "deployment_id,policy_id,section,ou_id,field_key"})
	assert.Contains(t, resourceSharingPolicyOverlayRuleTable.indexes, configIndexSpec{
		name: "idx_rspor_blanket_once", unique: "true",
		columns: "deployment_id,policy_id,section,field_key", predicateFragments: []string{"ou_id", "is null"},
	})
	assert.Contains(t, resourceSharingPolicyOverlayRuleTable.indexes,
		configIndexSpec{name: "idx_rspor_policy", unique: "false", columns: "deployment_id,policy_id"})
	assert.Contains(t, postgresCreateResourceSharingOverlayRuleTableSQL,
		"PRIMARY KEY (DEPLOYMENT_ID, ID)")
	assert.Contains(t, postgresCreateResourceSharingOverlayRuleTableSQL,
		"FOREIGN KEY (DEPLOYMENT_ID, POLICY_ID)")
	assert.Contains(t, postgresCreateResourceSharingOverlayRuleBlanketIndexSQL,
		"(DEPLOYMENT_ID, POLICY_ID, SECTION, FIELD_KEY)")
}
