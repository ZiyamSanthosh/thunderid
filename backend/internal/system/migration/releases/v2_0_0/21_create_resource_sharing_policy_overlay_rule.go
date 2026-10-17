// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var resourceSharingPolicyOverlayRuleTable = configTableSpec{
	name: "RESOURCE_SHARING_POLICY_OVERLAY_RULE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "policy_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "section", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
		{name: "ou_id", nullable: "YES", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "field_key", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "resolved", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "requested", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
	},
	keys: []configKeySpec{
		{
			typeCode: "f", columns: "deployment_id,policy_id", referencedTable: "resource_sharing_policy",
			referencedColumns: "deployment_id,id", deleteAction: "c",
		},
		{typeCode: "p", columns: "deployment_id,id"},
		{typeCode: "u", columns: "deployment_id,policy_id,section,ou_id,field_key"},
	},
	checks: []configCheckSpec{{requiredFragments: []string{"section", "allUnits", "childUnits", "otherTreeRoots"}}},
	indexes: []configIndexSpec{
		{
			name: "idx_rspor_blanket_once", unique: "true",
			columns: "deployment_id,policy_id,section,field_key", predicateFragments: []string{"ou_id", "is null"},
		},
		{name: "idx_rspor_policy", unique: "false", columns: "deployment_id,policy_id"},
	},
	dependencies: []configTableDependency{{
		table: "RESOURCE_SHARING_POLICY", operationID: "2.0.0/configdb/19_create_resource_sharing_policy",
	}},
}

func newCreateResourceSharingPolicyOverlayRuleOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule",
		"Create the resource sharing policy overlay rule table",
		resourceSharingPolicyOverlayRuleTable,
		[]engine.SQLStatement{
			{ID: "create-resource-sharing-overlay-rule-table", Query: postgresCreateResourceSharingOverlayRuleTableSQL},
			{ID: "create-resource-sharing-overlay-rule-policy-index", Query: postgresCreateResourceSharingOverlayRulePolicyIndexSQL},
			{ID: "create-resource-sharing-overlay-rule-blanket-index", Query: postgresCreateResourceSharingOverlayRuleBlanketIndexSQL},
		},
	)
}

const postgresCreateResourceSharingOverlayRuleTableSQL = `CREATE TABLE "RESOURCE_SHARING_POLICY_OVERLAY_RULE" (
    DEPLOYMENT_ID       VARCHAR(255) NOT NULL,
    ID                  VARCHAR(36) NOT NULL,
    POLICY_ID           VARCHAR(36) NOT NULL,
    SECTION             VARCHAR(16) NOT NULL CHECK (SECTION IN ('allUnits', 'childUnits', 'otherTreeRoots')),
    OU_ID               VARCHAR(36),
    FIELD_KEY           VARCHAR(255) NOT NULL,
    RESOLVED            JSONB NOT NULL,
    REQUESTED           JSONB NOT NULL,
    PRIMARY KEY (DEPLOYMENT_ID, ID),
    FOREIGN KEY (DEPLOYMENT_ID, POLICY_ID)
        REFERENCES "RESOURCE_SHARING_POLICY" (DEPLOYMENT_ID, ID) ON DELETE CASCADE,
    UNIQUE (DEPLOYMENT_ID, POLICY_ID, SECTION, OU_ID, FIELD_KEY)
)`

const postgresCreateResourceSharingOverlayRulePolicyIndexSQL = `CREATE INDEX idx_rspor_policy
    ON "RESOURCE_SHARING_POLICY_OVERLAY_RULE" (DEPLOYMENT_ID, POLICY_ID)`

const postgresCreateResourceSharingOverlayRuleBlanketIndexSQL = `CREATE UNIQUE INDEX idx_rspor_blanket_once
    ON "RESOURCE_SHARING_POLICY_OVERLAY_RULE" (DEPLOYMENT_ID, POLICY_ID, SECTION, FIELD_KEY)
    WHERE OU_ID IS NULL`
