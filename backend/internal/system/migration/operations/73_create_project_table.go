// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const createProjectTableOperationID engine.OperationID = "entitydb/73_create_project_table"

var projectTable = tableSpec{
	storeID: engine.StoreIDEntityDB,
	name:    "PROJECT",
	columns: []columnSpec{
		{"deployment_id", "NO", "character varying", "255", "none"},
		{"id", "NO", "character varying", "36", "none"},
		{"handle", "NO", "character varying", "100", "none"},
		{"display_name", "NO", "character varying", "100", "none"},
		{"description", "YES", "character varying", "255", "none"},
		{"created_at", "YES", "timestamp with time zone", "none", "now()"},
	},
	keys: []keySpec{{typeCode: "p", columns: "deployment_id,id"}},
	indexes: []indexSpec{
		{name: "idx_project_handle", unique: "true", columns: "deployment_id,handle"},
	},
}

// NewCreateProjectTableOperation returns the create project table operation.
func NewCreateProjectTableOperation() (engine.Operation, error) {
	return newCreateTableOperation(
		createProjectTableOperationID,
		"Create the project table",
		projectTable,
		[]engine.SQLStatement{
			{ID: "create-project-table", Query: `CREATE TABLE "PROJECT" (
    DEPLOYMENT_ID   VARCHAR(255) NOT NULL,
    ID              VARCHAR(36)  NOT NULL,
    HANDLE          VARCHAR(100) NOT NULL,
    DISPLAY_NAME    VARCHAR(100) NOT NULL,
    DESCRIPTION     VARCHAR(255),
    CREATED_AT      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, ID)
)`},
			{ID: "create-project-handle-index", Query: `CREATE UNIQUE INDEX idx_project_handle
    ON "PROJECT" (DEPLOYMENT_ID, HANDLE)`},
		},
	)
}
