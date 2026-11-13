// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const createGatewayMasterOperationID engine.OperationID = "configdb/72_create_gateway_master"

// gatewayMasterTable records what each gateway holds, resource by resource. The previous release has no such
// table, so applying a gateway fails without it. The primary key and the unique constraint carry their own
// indexes, so the table has no explicit index.
var gatewayMasterTable = tableSpec{
	storeID: engine.StoreIDConfigDB,
	name:    "GATEWAY_MASTER",
	columns: []columnSpec{
		{"deployment_id", "NO", "character varying", "255", "none"},
		{"id", "NO", "character varying", "36", "none"},
		{"gateway_id", "NO", "character varying", "36", "none"},
		{"seq", "NO", "integer", "none", "none"},
		{"hash", "NO", "character varying", "64", "none"},
		{"resources", "NO", "text", "none", "none"},
		{"manifest", "NO", "text", "none", "none"},
		{"created_at", "YES", "timestamp with time zone", "none", "now()"},
		{"created_by", "YES", "character varying", "255", "none"},
		{"project_id", "YES", "character varying", "36", "none"},
		{"project_revert", "NO", "boolean", "none", "false"},
	},
	keys: []keySpec{
		{typeCode: "p", columns: "id"},
		{typeCode: "u", columns: "deployment_id,gateway_id,seq"},
	},
}

// NewCreateGatewayMasterOperation returns the create gateway master operation.
func NewCreateGatewayMasterOperation() (engine.Operation, error) {
	return newCreateTableOperation(
		createGatewayMasterOperationID,
		"Create the table that records what each gateway holds",
		gatewayMasterTable,
		[]engine.SQLStatement{
			{ID: "create-gateway-master-table", Query: postgresCreateGatewayMasterTableSQL},
		},
	)
}

const postgresCreateGatewayMasterTableSQL = `CREATE TABLE "GATEWAY_MASTER" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) PRIMARY KEY,
    GATEWAY_ID VARCHAR(36) NOT NULL,
    SEQ INTEGER NOT NULL,
    HASH VARCHAR(64) NOT NULL,
    RESOURCES TEXT NOT NULL,
    MANIFEST TEXT NOT NULL,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    CREATED_BY VARCHAR(255),
    PROJECT_ID VARCHAR(36),
    PROJECT_REVERT BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (DEPLOYMENT_ID, GATEWAY_ID, SEQ)
)`
