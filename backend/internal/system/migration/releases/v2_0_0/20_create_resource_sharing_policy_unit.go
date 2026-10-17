// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var resourceSharingPolicyUnitTable = configTableSpec{
	name: "RESOURCE_SHARING_POLICY_UNIT",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "policy_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "section", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
		{name: "ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "reach", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
	},
	keys: []configKeySpec{
		{
			typeCode: "f", columns: "deployment_id,policy_id", referencedTable: "resource_sharing_policy",
			referencedColumns: "deployment_id,id", deleteAction: "c",
		},
		{typeCode: "p", columns: "deployment_id,policy_id,ou_id"},
	},
	checks: []configCheckSpec{
		{requiredFragments: []string{"section", "childUnits", "otherTreeRoots"}},
		{requiredFragments: []string{"reach", "unit", "subtree"}},
		{requiredFragments: []string{"section", "childUnits", "reach", "unit"}},
	},
	indexes: []configIndexSpec{{name: "idx_rspu_ou", unique: "false", columns: "deployment_id,ou_id"}},
	dependencies: []configTableDependency{{
		table: "RESOURCE_SHARING_POLICY", operationID: "2.0.0/configdb/19_create_resource_sharing_policy",
	}},
}

func newCreateResourceSharingPolicyUnitOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/20_create_resource_sharing_policy_unit",
		"Create the resource sharing policy unit table",
		resourceSharingPolicyUnitTable,
		[]engine.SQLStatement{
			{ID: "create-resource-sharing-policy-unit-table", Query: postgresCreateResourceSharingPolicyUnitTableSQL},
			{ID: "create-resource-sharing-policy-unit-ou-index", Query: postgresCreateResourceSharingPolicyUnitOUIndexSQL},
		},
	)
}

const postgresCreateResourceSharingPolicyUnitTableSQL = `CREATE TABLE "RESOURCE_SHARING_POLICY_UNIT" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    POLICY_ID     VARCHAR(36) NOT NULL,
    SECTION       VARCHAR(16) NOT NULL CHECK (SECTION IN ('childUnits', 'otherTreeRoots')),
    OU_ID         VARCHAR(36) NOT NULL,
    REACH         VARCHAR(16) NOT NULL CHECK (REACH IN ('unit', 'subtree')),
    PRIMARY KEY (DEPLOYMENT_ID, POLICY_ID, OU_ID),
    FOREIGN KEY (DEPLOYMENT_ID, POLICY_ID)
        REFERENCES "RESOURCE_SHARING_POLICY" (DEPLOYMENT_ID, ID) ON DELETE CASCADE,
    CHECK (SECTION = 'childUnits' OR REACH = 'unit')
)`

const postgresCreateResourceSharingPolicyUnitOUIndexSQL = `CREATE INDEX idx_rspu_ou
    ON "RESOURCE_SHARING_POLICY_UNIT" (DEPLOYMENT_ID, OU_ID)`
