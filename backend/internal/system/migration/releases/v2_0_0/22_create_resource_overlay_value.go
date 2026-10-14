// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var resourceOverlayValueTable = configTableSpec{
	name: "RESOURCE_OVERLAY_VALUE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "resource_type", nullable: "NO", dataType: "character varying", maximumLength: "64", defaultValue: "none"},
		{name: "resource_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "field_key", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "value", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{
		typeCode: "p", columns: "deployment_id,resource_type,resource_id,ou_id,field_key",
	}},
}

func newCreateResourceOverlayValueOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/22_create_resource_overlay_value",
		"Create the resource overlay value table",
		resourceOverlayValueTable,
		[]engine.SQLStatement{{ID: "create-resource-overlay-value-table", Query: postgresCreateResourceOverlayValueTableSQL}},
	)
}

const postgresCreateResourceOverlayValueTableSQL = `CREATE TABLE "RESOURCE_OVERLAY_VALUE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    RESOURCE_TYPE VARCHAR(64) NOT NULL,
    RESOURCE_ID   VARCHAR(36) NOT NULL,
    OU_ID         VARCHAR(36) NOT NULL,
    FIELD_KEY     VARCHAR(255) NOT NULL,
    VALUE         JSONB NOT NULL,
    CREATED_AT    TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT    TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, RESOURCE_TYPE, RESOURCE_ID, OU_ID, FIELD_KEY)
)`
