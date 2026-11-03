// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const authorizationPDPConnectionIndex = "idx_authorization_pdp_connection_type_name_deployment"

var authorizationPDPConnectionTable = configTableSpec{
	name: "AUTHORIZATION_PDP_CONNECTION",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "description", nullable: "YES", dataType: "character varying", maximumLength: "500", defaultValue: "none"},
		{name: "type", nullable: "NO", dataType: "character varying", maximumLength: "50", defaultValue: "none"},
		{name: "properties", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "id"}},
	indexes: []configIndexSpec{{
		name: authorizationPDPConnectionIndex, unique: "true", columns: "deployment_id,type,name",
	}},
}

func newCreateAuthorizationPDPConnectionOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/01_create_authorization_pdp_connection",
		"Create the authorization PDP connection table",
		authorizationPDPConnectionTable,
		[]engine.SQLStatement{
			{ID: "create-table", Query: postgresCreateAuthorizationPDPConnectionTableSQL},
			{ID: "create-index", Query: postgresCreateAuthorizationPDPConnectionIndexSQL},
		},
	)
}

const postgresCreateAuthorizationPDPConnectionTableSQL = `CREATE TABLE "AUTHORIZATION_PDP_CONNECTION" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) PRIMARY KEY,
    NAME VARCHAR(255) NOT NULL,
    DESCRIPTION VARCHAR(500),
    TYPE VARCHAR(50) NOT NULL,
    PROPERTIES JSONB NOT NULL,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT TIMESTAMPTZ DEFAULT NOW()
)`

const postgresCreateAuthorizationPDPConnectionIndexSQL = `CREATE UNIQUE INDEX
    idx_authorization_pdp_connection_type_name_deployment
    ON "AUTHORIZATION_PDP_CONNECTION" (DEPLOYMENT_ID, TYPE, NAME)`
