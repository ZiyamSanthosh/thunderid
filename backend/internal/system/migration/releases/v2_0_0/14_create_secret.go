// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var secretTable = configTableSpec{
	name: "SECRET",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "value", nullable: "NO", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "description", nullable: "YES", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{{typeCode: "p", columns: "deployment_id,name"}},
	indexes: []configIndexSpec{{
		name: "idx_secret_deployment_name", unique: "false", columns: "deployment_id,name",
	}},
}

func newCreateSecretOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/14_create_secret",
		"Create the secret table",
		secretTable,
		[]engine.SQLStatement{
			{ID: "create-secret-table", Query: postgresCreateSecretTableSQL},
			{ID: "create-secret-index", Query: postgresCreateSecretIndexSQL},
		},
	)
}

const postgresCreateSecretTableSQL = `CREATE TABLE "SECRET" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    NAME          VARCHAR(255) NOT NULL,
    VALUE         TEXT         NOT NULL,
    DESCRIPTION   TEXT,
    CREATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    UPDATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, NAME)
)`

const postgresCreateSecretIndexSQL = `CREATE INDEX idx_secret_deployment_name
    ON "SECRET" (DEPLOYMENT_ID, NAME)`
