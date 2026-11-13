// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const (
	createHTTPConnectionOperationID          engine.OperationID = "configdb/76_create_http_connection"
	createAuthorizationDetailTypeOperationID engine.OperationID = "configdb/" +
		"77_create_authorization_detail_type"
	createGatewayApplyLeaseOperationID         engine.OperationID = "configdb/78_create_gateway_apply_lease"
	httpConnectionNameIndex                                       = "idx_http_connection_name_deployment"
	authorizationDetailTypeUniqueTypeIndexName                    = "uq_authorization_detail_type"
)

var httpConnectionTable = configTableSpec{
	name: "HTTP_CONNECTION",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "description", nullable: "YES", dataType: "character varying",
			maximumLength: "500", defaultValue: "none"},
		{name: "ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "properties", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone",
			maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone",
			maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,id"}},
	indexes: []configIndexSpec{{
		name: httpConnectionNameIndex, unique: "true", columns: "deployment_id,name",
	}},
}

var authorizationDetailTypeTable = configTableSpec{
	name: "AUTHORIZATION_DETAIL_TYPE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "type", nullable: "NO", dataType: "character varying", maximumLength: "512", defaultValue: "none"},
		{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "100", defaultValue: "none"},
		{name: "description", nullable: "YES", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "version", nullable: "NO", dataType: "integer", maximumLength: "none", defaultValue: "1"},
		{name: "schema_definition", nullable: "NO", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "project_id", nullable: "YES", dataType: "character varying", maximumLength: "36",
			defaultValue: "none"},
		{name: "properties", nullable: "YES", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone",
			maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone",
			maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,id"}},
	indexes: []configIndexSpec{
		{name: authorizationDetailTypeUniqueTypeIndexName, unique: "true", columns: "deployment_id,type"},
		{name: "idx_authorization_detail_type_project", unique: "false", columns: "deployment_id,project_id"},
	},
}

var gatewayApplyLeaseTable = configTableSpec{
	name: "GATEWAY_APPLY_LEASE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
		{name: "gateway_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "token", nullable: "YES", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "applying_since", nullable: "YES", dataType: "timestamp with time zone",
			maximumLength: "none", defaultValue: "none"},
		{name: "pending_latest", nullable: "NO", dataType: "boolean", maximumLength: "none", defaultValue: "false"},
		{name: "pending_by", nullable: "YES", dataType: "character varying",
			maximumLength: "255", defaultValue: "none"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,gateway_id"}},
}

// NewCreateHTTPConnectionOperation returns the create http connection operation.
func NewCreateHTTPConnectionOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		createHTTPConnectionOperationID,
		"Create the HTTP connection table",
		httpConnectionTable,
		[]engine.SQLStatement{
			{ID: "create-http-connection-table", Query: `CREATE TABLE "HTTP_CONNECTION" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID            VARCHAR(36) NOT NULL,
    NAME          VARCHAR(255) NOT NULL,
    DESCRIPTION   VARCHAR(500),
    OU_ID         VARCHAR(36) NOT NULL,
    PROPERTIES    JSONB NOT NULL,
    CREATED_AT    TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT    TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, ID)
)`},
			{ID: "create-http-connection-index", Query: `CREATE UNIQUE INDEX idx_http_connection_name_deployment
    ON "HTTP_CONNECTION" (DEPLOYMENT_ID, NAME)`},
		},
	)
}

// NewCreateAuthorizationDetailTypeOperation returns the create authorization detail type operation.
func NewCreateAuthorizationDetailTypeOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		createAuthorizationDetailTypeOperationID,
		"Create the authorization detail type table",
		authorizationDetailTypeTable,
		[]engine.SQLStatement{
			{ID: "create-authorization-detail-type-table", Query: `CREATE TABLE "AUTHORIZATION_DETAIL_TYPE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) NOT NULL,
    TYPE VARCHAR(512) NOT NULL,
    NAME VARCHAR(100) NOT NULL,
    DESCRIPTION TEXT,
    VERSION INTEGER NOT NULL DEFAULT 1,
    SCHEMA_DEFINITION TEXT NOT NULL,
    PROJECT_ID VARCHAR(36),
    PROPERTIES JSONB,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, ID)
)`},
			{ID: "create-authorization-detail-type-index", Query: `CREATE UNIQUE INDEX uq_authorization_detail_type
    ON "AUTHORIZATION_DETAIL_TYPE" (DEPLOYMENT_ID, TYPE)`},
			{ID: "create-authorization-detail-type-project-index", Query: `CREATE INDEX
    idx_authorization_detail_type_project ON "AUTHORIZATION_DETAIL_TYPE" (DEPLOYMENT_ID, PROJECT_ID)`},
		},
	)
}

// NewCreateGatewayApplyLeaseOperation returns the create gateway apply lease operation.
func NewCreateGatewayApplyLeaseOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		createGatewayApplyLeaseOperationID,
		"Create the gateway apply lease table",
		gatewayApplyLeaseTable,
		[]engine.SQLStatement{
			{ID: "create-gateway-apply-lease-table", Query: `CREATE TABLE "GATEWAY_APPLY_LEASE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    GATEWAY_ID VARCHAR(36) NOT NULL,
    TOKEN VARCHAR(36),
    APPLYING_SINCE TIMESTAMPTZ,
    PENDING_LATEST BOOLEAN NOT NULL DEFAULT FALSE,
    PENDING_BY VARCHAR(255),
    PRIMARY KEY (DEPLOYMENT_ID, GATEWAY_ID)
)`},
		},
	)
}
