// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const addEntityProjectIDOperationID engine.OperationID = "entitydb/74_add_entity_project_id"

// NewAddEntityProjectIDOperation adds the project an entity belongs to. The column is nullable with no default,
// so existing entities belong to no project and need no data migration.
func NewAddEntityProjectIDOperation() (engine.Operation, error) {
	return newAddColumnOperation(addColumnSpec{
		id:          addEntityProjectIDOperationID,
		description: "Add the project ID to entities",
		storeID:     engine.StoreIDEntityDB,
		table:       "ENTITY",
		columns: []columnSpec{
			{"project_id", "YES", "character varying", "36", "none"},
		},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-project-id", Query: `ALTER TABLE "ENTITY" ADD COLUMN PROJECT_ID VARCHAR(36)`},
		},
	})
}
