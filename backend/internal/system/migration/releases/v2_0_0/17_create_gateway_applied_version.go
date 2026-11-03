// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var gatewayAppliedVersionTable = configTableSpec{
	name: "GATEWAY_APPLIED_VERSION",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "gateway_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "applied_seq", nullable: "NO", dataType: "integer", maximumLength: "none", defaultValue: "none"},
		{name: "previous_seq", nullable: "YES", dataType: "integer", maximumLength: "none", defaultValue: "none"},
		{name: "applied_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "gateway_id,deployment_id"}},
}

func newCreateGatewayAppliedVersionOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/17_create_gateway_applied_version",
		"Create the gateway applied version table",
		gatewayAppliedVersionTable,
		[]engine.SQLStatement{{ID: "create-gateway-applied-version-table", Query: postgresCreateGatewayAppliedVersionTableSQL}},
	)
}

const postgresCreateGatewayAppliedVersionTableSQL = `CREATE TABLE "GATEWAY_APPLIED_VERSION" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    GATEWAY_ID VARCHAR(36) NOT NULL,
    APPLIED_SEQ INTEGER NOT NULL,
    PREVIOUS_SEQ INTEGER,
    APPLIED_AT TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (GATEWAY_ID, DEPLOYMENT_ID)
)`
