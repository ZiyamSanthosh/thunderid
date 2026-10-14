// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var resourceSharingPolicyTable = configTableSpec{
	name: "RESOURCE_SHARING_POLICY",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "resource_type", nullable: "NO", dataType: "character varying", maximumLength: "64", defaultValue: "none"},
		{name: "resource_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "owning_ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "initiating_ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "policy_stage", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
		{name: "parent_policy_id", nullable: "YES", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "sections", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "version", nullable: "NO", dataType: "integer", maximumLength: "none", defaultValue: "1"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{
		{typeCode: "p", columns: "id"},
		{typeCode: "u", columns: "deployment_id,resource_type,resource_id,initiating_ou_id"},
	},
	checks:  []configCheckSpec{{requiredFragments: []string{"policy_stage", "share", "reshare"}}},
	indexes: []configIndexSpec{{name: "idx_rsp_parent", unique: "false", columns: "parent_policy_id"}},
}

func newCreateResourceSharingPolicyOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/19_create_resource_sharing_policy",
		"Create the resource sharing policy table",
		resourceSharingPolicyTable,
		[]engine.SQLStatement{
			{ID: "create-resource-sharing-policy-table", Query: postgresCreateResourceSharingPolicyTableSQL},
			{ID: "create-resource-sharing-policy-parent-index", Query: postgresCreateResourceSharingPolicyParentIndexSQL},
		},
	)
}

const postgresCreateResourceSharingPolicyTableSQL = `CREATE TABLE "RESOURCE_SHARING_POLICY" (
    DEPLOYMENT_ID    VARCHAR(255) NOT NULL,
    ID               VARCHAR(36) PRIMARY KEY,
    RESOURCE_TYPE    VARCHAR(64) NOT NULL,
    RESOURCE_ID      VARCHAR(36) NOT NULL,
    OWNING_OU_ID     VARCHAR(36) NOT NULL,
    INITIATING_OU_ID VARCHAR(36) NOT NULL,
    POLICY_STAGE     VARCHAR(16) NOT NULL CHECK (POLICY_STAGE IN ('share', 'reshare')),
    PARENT_POLICY_ID VARCHAR(36),
    SECTIONS         JSONB NOT NULL,
    VERSION          INTEGER NOT NULL DEFAULT 1,
    CREATED_AT       TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (DEPLOYMENT_ID, RESOURCE_TYPE, RESOURCE_ID, INITIATING_OU_ID)
)`

const postgresCreateResourceSharingPolicyParentIndexSQL = `CREATE INDEX idx_rsp_parent
    ON "RESOURCE_SHARING_POLICY" (PARENT_POLICY_ID)`
