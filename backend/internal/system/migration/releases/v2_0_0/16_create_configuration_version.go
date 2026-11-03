// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

var configurationVersionTable = configTableSpec{
	name: "CONFIGURATION_VERSION",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "seq", nullable: "NO", dataType: "integer", maximumLength: "none", defaultValue: "none"},
		{name: "hash", nullable: "NO", dataType: "character varying", maximumLength: "64", defaultValue: "none"},
		{name: "name", nullable: "YES", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "resources", nullable: "NO", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "variables", nullable: "YES", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "note", nullable: "YES", dataType: "text", maximumLength: "none", defaultValue: "none"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{
		{typeCode: "p", columns: "id"},
		{typeCode: "u", columns: "deployment_id,hash"},
		{typeCode: "u", columns: "deployment_id,seq"},
	},
}

func newCreateConfigurationVersionOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/16_create_configuration_version",
		"Create the configuration version table",
		configurationVersionTable,
		[]engine.SQLStatement{{ID: "create-configuration-version-table", Query: postgresCreateConfigurationVersionTableSQL}},
	)
}

const postgresCreateConfigurationVersionTableSQL = `CREATE TABLE "CONFIGURATION_VERSION" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) PRIMARY KEY,
    SEQ INTEGER NOT NULL,
    HASH VARCHAR(64) NOT NULL,
    NAME VARCHAR(255),
    RESOURCES TEXT NOT NULL,
    VARIABLES TEXT,
    NOTE TEXT,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (DEPLOYMENT_ID, SEQ),
    UNIQUE (DEPLOYMENT_ID, HASH)
)`
