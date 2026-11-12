// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const addRoleAssignmentSharingPolicyIDOperationID engine.OperationID = "2.0.0/configdb/" +
	"60_add_role_assignment_sharing_policy_id"

var roleAssignmentAddedColumns = addColumnSpec{
	id:          addRoleAssignmentSharingPolicyIDOperationID,
	description: "Add the assignment organization unit and the sharing policy ID to role assignments",
	storeID:     engine.StoreIDConfigDB,
	table:       "ROLE_ASSIGNMENT",
	columns: []columnSpec{
		{"assignment_ou_id", "NO", "character varying", "36", emptyStringDefault},
		{"sharing_policy_id", "NO", "character varying", "36", emptyStringDefault},
	},
	statements: []engine.SQLStatement{
		{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
		{ID: "add-assignment-columns", Query: `ALTER TABLE "ROLE_ASSIGNMENT"
    ADD COLUMN ASSIGNMENT_OU_ID VARCHAR(36) NOT NULL DEFAULT '',
    ADD COLUMN SHARING_POLICY_ID VARCHAR(36) NOT NULL DEFAULT ''`},
	},
}

// newAddRoleAssignmentSharingPolicyIDOperation adds the organization unit an assignment was made in and the
// sharing policy that mandates it. Both default to the empty string, which the catalog stores as a constant, so
// the table is not rewritten. The previous release never names the columns, and its inserts get the defaults:
// a direct assignment with no recorded organization unit, which the migrate phase fills from the role's owner.
func newAddRoleAssignmentSharingPolicyIDOperation() (engine.Operation, error) {
	return newAddColumnOperation(roleAssignmentAddedColumns)
}

func verifyRoleAssignmentSharingPolicyID(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyAddedColumns(ctx, access, roleAssignmentAddedColumns)
}
