// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var gatewayExcludedResourceTable = configTableSpec{
	name: "GATEWAY_EXCLUDED_RESOURCE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "gateway_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "resource_key", nullable: "NO", dataType: "character varying",
			maximumLength: "512", defaultValue: "none"},
		{name: "resource_type", nullable: "NO", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "resource_id", nullable: "YES", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "resource_name", nullable: "YES", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "once", nullable: "NO", dataType: "boolean", maximumLength: "none", defaultValue: "false"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,gateway_id,resource_key"}},
}

// NewCreateGatewayExcludedResourceOperation returns the create gateway excluded resource operation.
func NewCreateGatewayExcludedResourceOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"configdb/17_create_gateway_excluded_resource",
		"Create the gateway excluded-resource table",
		gatewayExcludedResourceTable,
		[]engine.SQLStatement{{ID: "create-gateway-excluded-resource-table", Query: postgresCreateGatewayExcludedResourceTableSQL}},
	)
}

const postgresCreateGatewayExcludedResourceTableSQL = `CREATE TABLE "GATEWAY_EXCLUDED_RESOURCE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    GATEWAY_ID VARCHAR(36) NOT NULL,
    RESOURCE_KEY VARCHAR(512) NOT NULL,
    RESOURCE_TYPE VARCHAR(255) NOT NULL,
    RESOURCE_ID VARCHAR(255),
    RESOURCE_NAME VARCHAR(255),
    ONCE BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (DEPLOYMENT_ID, GATEWAY_ID, RESOURCE_KEY)
)`
