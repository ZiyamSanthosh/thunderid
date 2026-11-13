// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const createManagedResourceOperationID engine.OperationID = "configdb/113_create_managed_resource"

var managedResourceTable = configTableSpec{
	name: "MANAGED_RESOURCE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255",
			defaultValue: "none"},
		{name: "resource_type", nullable: "NO", dataType: "character varying", maximumLength: "64",
			defaultValue: "none"},
		{name: "resource_id", nullable: "NO", dataType: "character varying", maximumLength: "255",
			defaultValue: "none"},
		{name: "part", nullable: "NO", dataType: "character varying", maximumLength: "64",
			defaultValue: emptyStringDefault},
		{name: "part_key", nullable: "NO", dataType: "character varying", maximumLength: "512",
			defaultValue: emptyStringDefault},
		{name: "marked_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none",
			defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,resource_id,resource_type,part,part_key"}},
}

// NewCreateManagedResourceOperation returns the create managed resource operation.
func NewCreateManagedResourceOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		createManagedResourceOperationID,
		"Create the table that records what a control plane deployed",
		managedResourceTable,
		[]engine.SQLStatement{{ID: "create-managed-resource-table", Query: `CREATE TABLE "MANAGED_RESOURCE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    RESOURCE_TYPE VARCHAR(64)  NOT NULL,
    RESOURCE_ID   VARCHAR(255) NOT NULL,
    PART          VARCHAR(64)  NOT NULL DEFAULT '',
    PART_KEY      VARCHAR(512) NOT NULL DEFAULT '',
    MARKED_AT     TIMESTAMPTZ  DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, RESOURCE_ID, RESOURCE_TYPE, PART, PART_KEY)
)`}},
	)
}
