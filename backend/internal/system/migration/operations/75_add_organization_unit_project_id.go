// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const addOrganizationUnitProjectIDOperationID engine.OperationID = "entitydb/" +
	"75_add_organization_unit_project_id"

// NewAddOrganizationUnitProjectIDOperation adds the project an organization unit belongs to. The
// column is nullable with no default, like the entity column.
func NewAddOrganizationUnitProjectIDOperation() (engine.Operation, error) {
	return newAddColumnOperation(addColumnSpec{
		id:          addOrganizationUnitProjectIDOperationID,
		description: "Add the project ID to organization units",
		storeID:     engine.StoreIDEntityDB,
		table:       "ORGANIZATION_UNIT",
		columns: []columnSpec{
			{"project_id", "YES", "character varying", "36", "none"},
		},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-project-id", Query: `ALTER TABLE "ORGANIZATION_UNIT" ADD COLUMN PROJECT_ID VARCHAR(36)`},
		},
	})
}
