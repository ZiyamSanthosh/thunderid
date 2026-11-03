// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var gatewayTable = configTableSpec{
	name: "GATEWAY",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "base_url", nullable: "NO", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "management_key", nullable: "NO", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "ca_certificate", nullable: "YES", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "is_default", nullable: "NO", dataType: "boolean", maximumLength: "none", defaultValue: "false"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{
		{typeCode: "p", columns: "id"},
		{typeCode: "u", columns: "base_url,deployment_id"},
		{typeCode: "u", columns: "name,deployment_id"},
	},
	indexes: []configIndexSpec{{
		name: "idx_gateway_default_deployment", unique: "true", columns: "deployment_id",
		predicateFragments: []string{"is_default", "true"},
	}},
}

func newCreateGatewayOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/15_create_gateway",
		"Create the gateway table",
		gatewayTable,
		[]engine.SQLStatement{
			{ID: "create-gateway-table", Query: postgresCreateGatewayTableSQL},
			{ID: "create-default-gateway-index", Query: postgresCreateDefaultGatewayIndexSQL},
		},
	)
}

const postgresCreateGatewayTableSQL = `CREATE TABLE "GATEWAY" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) PRIMARY KEY,
    NAME VARCHAR(255) NOT NULL,
    BASE_URL TEXT NOT NULL,
    MANAGEMENT_KEY TEXT NOT NULL,
    CA_CERTIFICATE TEXT,
    IS_DEFAULT BOOLEAN NOT NULL DEFAULT FALSE,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (NAME, DEPLOYMENT_ID),
    UNIQUE (BASE_URL, DEPLOYMENT_ID)
)`

const postgresCreateDefaultGatewayIndexSQL = `CREATE UNIQUE INDEX idx_gateway_default_deployment
    ON "GATEWAY" (DEPLOYMENT_ID) WHERE IS_DEFAULT = TRUE`
