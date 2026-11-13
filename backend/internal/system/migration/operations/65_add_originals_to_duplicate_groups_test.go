// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func membershipRow(memberID, groupID, ouID string) engine.QueryRow {
	return engine.QueryRow{"member_id": memberID, "group_id": groupID, "ou_id": ouID}
}

// membershipsOf answers the membership query for the requested users only.
func membershipsOf(rows ...engine.QueryRow) func(args []interface{}) []engine.QueryRow {
	return func(args []interface{}) []engine.QueryRow {
		requested, _ := args[0].(string)
		var result []engine.QueryRow
		for _, row := range rows {
			if member, _ := row["member_id"].(string); strings.Contains(requested, `"`+member+`"`) {
				result = append(result, row)
			}
		}
		return result
	}
}

// cloudLikeMemberships: each user is in one group of its own unit.
func cloudLikeMemberships() []engine.QueryRow {
	return []engine.QueryRow{
		membershipRow("a1", "admin-ou-1", "ou-1"),
		membershipRow("a2", "admin-ou-2", "ou-2"),
		membershipRow("a3", "engineer-ou-3", "ou-3"),
		membershipRow("b1", "engineer-ou-1", "ou-1"),
		membershipRow("b2", "engineer-ou-3", "ou-3"),
	}
}

func TestAddOriginalsToDuplicateGroupsDefinition(t *testing.T) {
	definition := NewAddOriginalsToDuplicateGroupsOperation().Definition()
	assert.Equal(t, addOriginalsToDuplicateGroupsOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, []engine.StoreID{engine.StoreIDConfigDB}, definition.ReadStoreIDs)
	assert.Equal(t, addOriginalsToDuplicateGroupsOperationID,
		NewRollbackAddOriginalsToDuplicateGroupsOperation().ForwardOperationID())
}

func TestAddOriginalsToDuplicateGroupsBatchUsesOneStatementPerBatch(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	checkpoint, completed, err := addOriginalsToDuplicateGroupsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	require.Len(t, access.executed, 1)
	assert.Equal(t, queryAddOriginalMemberships.GetID(), access.executed[0].queryID)
	assert.Equal(t, []interface{}{`{"a1","b1"}`, "dep"}, access.executed[0].args)
}

func TestAddOriginalMembershipsStatementShape(t *testing.T) {
	query := queryAddOriginalMemberships.PostgresQuery
	assert.Contains(t, query, `ON CONFLICT DO NOTHING`)
	assert.Contains(t, query, `RETURNING MEMBER_ID, GROUP_ID, DEPLOYMENT_ID`)
	assert.Contains(t, query, `NOT m.IS_ORIGINAL`)
	assert.Contains(t, query, `ADDED_GROUP_IDS = t.ADDED_GROUP_IDS || a.GROUPS`)
}

func TestVerifyOriginalsInDuplicateGroups(t *testing.T) {
	copied := append(cloudLikeMemberships(),
		membershipRow("a1", "admin-ou-2", "ou-2"), membershipRow("a1", "engineer-ou-3", "ou-3"),
		membershipRow("b1", "engineer-ou-3", "ou-3"))
	tests := []struct {
		name        string
		memberships []engine.QueryRow
		direct      []engine.QueryRow
		groupUnits  []engine.QueryRow
		state       engine.VerificationState
		summary     string
	}{
		{name: "memberships not copied", memberships: cloudLikeMemberships(), state: engine.VerificationSource},
		{name: "memberships copied", memberships: copied, state: engine.VerificationTarget},
		{name: "direct role assignment", memberships: copied,
			direct: []engine.QueryRow{{"assignee_id": "a2"}}, state: engine.VerificationInvalid,
			summary: "direct role assignment"},
		{name: "group role outside its unit", memberships: copied,
			groupUnits: []engine.QueryRow{{"assignee_id": "admin-ou-2", "assignment_ou_id": "ou-1"}},
			state:      engine.VerificationInvalid, summary: "instead of its own"},
		{name: "group role in its unit", memberships: copied,
			groupUnits: []engine.QueryRow{{"assignee_id": "admin-ou-2", "assignment_ou_id": "ou-2"}},
			state:      engine.VerificationTarget},
		{name: "duplicate in a group of another unit", memberships: append(cloudLikeMemberships(),
			membershipRow("a2", "admin-ou-9", "ou-9")), state: engine.VerificationInvalid,
			summary: "of organization unit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
				onArgs(queryUserGroupMemberships, membershipsOf(test.memberships...)).
				on(queryDirectUserRoleAssignments, test.direct...).
				on(queryGroupRoleAssignmentUnits, test.groupUnits...)
			result, err := NewAddOriginalsToDuplicateGroupsOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)
			assert.Contains(t, result.Summary, test.summary)
		})
	}
}

func TestRemoveAddedMemberships(t *testing.T) {
	rows := recordedGroupRows()
	rows[0]["added_group_ids"] = `["admin-ou-2","engineer-ou-3"]`
	access := newFakeAccess().on(queryMigrationUsers, rows...)

	_, completed, err := removeAddedMembershipsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, []string{
		queryDeleteOriginalMembership.GetID(), queryDeleteOriginalMembership.GetID(), queryClearAddedGroups.GetID(),
	}, access.executedIDs())
	assert.Equal(t, []interface{}{"engineer-ou-3", "a1", "dep"}, access.executed[1].args)

	rollback := NewRollbackAddOriginalsToDuplicateGroupsOperation()
	result, err := rollback.PreVerify(context.Background(), newFakeAccess().on(queryMigrationUsers, rows...),
		engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)
	result, err = rollback.PostVerify(context.Background(),
		newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...), engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}
