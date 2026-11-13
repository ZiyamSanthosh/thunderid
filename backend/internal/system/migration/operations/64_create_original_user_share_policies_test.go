// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// recordedGroupRows returns working-table rows for original a1 (ou-1, policy p-a) with duplicates in ou-2 and
// ou-3, and original b1 (ou-1, policy p-b) with a duplicate in ou-3.
func recordedGroupRows() []engine.QueryRow {
	a1 := migrationUserRow("a1", "a1", "ou-1", true)
	a1["sharing_policy_id"], a1["owner_ou_id"] = "p-a", testRootOU
	b1 := migrationUserRow("b1", "b1", "ou-1", true)
	b1["sharing_policy_id"], b1["owner_ou_id"] = "p-b", testRootOU
	return []engine.QueryRow{
		a1, migrationUserRow("a2", "a1", "ou-2", false), migrationUserRow("a3", "a1", "ou-3", false),
		b1, migrationUserRow("b2", "b1", "ou-3", false),
	}
}

func userPolicyRow(id, resourceID string) engine.QueryRow {
	return engine.QueryRow{
		"id": id, "resource_id": resourceID, "owning_ou_id": testRootOU, "initiating_ou_id": testRootOU,
		"policy_stage": "share", "parent_policy_id": nil, "sections": `{"childUnits": {"mode": "selected"}}`,
		"deployment_id": "dep",
	}
}

func unitRow(policyID, ouID string) engine.QueryRow {
	return engine.QueryRow{"policy_id": policyID, "section": "childUnits", "ou_id": ouID, "reach": "unit"}
}

func TestCreateOriginalUserSharePoliciesDefinition(t *testing.T) {
	definition := NewCreateOriginalUserSharePoliciesOperation().Definition()
	assert.Equal(t, createOriginalUserSharePoliciesOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, []engine.StoreID{engine.StoreIDEntityDB}, definition.ReadStoreIDs)
	assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)

	rollback := NewRollbackCreateOriginalUserSharePoliciesOperation()
	assert.Equal(t, createOriginalUserSharePoliciesOperationID, rollback.ForwardOperationID())
}

func TestCreateOriginalUserSharePoliciesBatch(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	checkpoint, completed, err := createOriginalUserSharePoliciesBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	assert.Equal(t, []string{
		queryInsertUserSharingPolicy.GetID(), queryInsertUserSharingPolicyUnit.GetID(),
		queryInsertUserSharingPolicyUnit.GetID(), queryInsertUserSharingPolicyUnit.GetID(),
		queryInsertUserSharingPolicy.GetID(), queryInsertUserSharingPolicyUnit.GetID(),
		queryInsertUserSharingPolicyUnit.GetID(),
	}, access.executedIDs())
	assert.Equal(t, []interface{}{"p-a", "a1", testRootOU, userSharingPolicySections, "dep"}, access.executed[0].args)
	assert.Equal(t, []interface{}{"p-a", "ou-1", "dep"}, access.executed[1].args)
	assert.Equal(t, []interface{}{"p-a", "ou-3", "dep"}, access.executed[3].args)
}

func TestCreateOriginalUserSharePoliciesBatchResumesAfterCheckpoint(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	checkpoint, completed, err := createOriginalUserSharePoliciesBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{DeploymentID: "dep", Key: "a1"})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, "b1", checkpoint.Key)
	assert.Equal(t, "b1", access.executed[0].args[1])
	assert.Len(t, access.executed, 3)
}

func TestCreateOriginalUserSharePoliciesSharesAPersonWithOneUserToItsOwnUnit(t *testing.T) {
	solo := recordedGroupRows()[:1]
	access := newFakeAccess().on(queryMigrationUsers, solo...)

	_, _, err := createOriginalUserSharePoliciesBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.Equal(t, []string{queryInsertUserSharingPolicy.GetID(), queryInsertUserSharingPolicyUnit.GetID()},
		access.executedIDs())
	assert.Equal(t, []interface{}{"p-a", "ou-1", "dep"}, access.executed[1].args)
}

func TestNextMigrationGroupsPagesBySize(t *testing.T) {
	groups, err := groupMigrationUsers([]migrationUser{
		{deploymentID: "dep", userID: "a1", originalUserID: "a1", isOriginal: true},
		{deploymentID: "dep", userID: "b1", originalUserID: "b1", isOriginal: true},
		{deploymentID: "dep", userID: "c1", originalUserID: "c1", isOriginal: true},
	})
	require.NoError(t, err)

	first := nextMigrationGroups(groups, batchCheckpoint{}, 2)
	require.Len(t, first, 2)
	second := nextMigrationGroups(groups, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, 2)
	require.Len(t, second, 1)
	assert.Equal(t, "c1", second[0].original.userID)
	assert.Empty(t, nextMigrationGroups(groups, batchCheckpoint{DeploymentID: "dep", Key: "c1"}, 2))
}

func TestVerifyOriginalUserSharePolicies(t *testing.T) {
	fullUnits := []engine.QueryRow{
		unitRow("p-a", "ou-1"), unitRow("p-a", "ou-2"), unitRow("p-a", "ou-3"),
		unitRow("p-b", "ou-1"), unitRow("p-b", "ou-3"),
	}
	wrongOwner := userPolicyRow("p-a", "a1")
	wrongOwner["owning_ou_id"] = "ou-1"
	tests := []struct {
		name     string
		policies []engine.QueryRow
		units    []engine.QueryRow
		rules    []engine.QueryRow
		state    engine.VerificationState
	}{
		{name: "no policy yet", state: engine.VerificationSource},
		{name: "one original shared", policies: []engine.QueryRow{userPolicyRow("p-a", "a1")},
			units: fullUnits[:3], state: engine.VerificationSource},
		{name: "every original shared", policies: []engine.QueryRow{
			userPolicyRow("p-a", "a1"), userPolicyRow("p-b", "b1"),
		}, units: fullUnits, state: engine.VerificationTarget},
		{name: "foreign policy on an original", policies: []engine.QueryRow{userPolicyRow("other", "a1")},
			state: engine.VerificationInvalid},
		{name: "wrong owner", policies: []engine.QueryRow{wrongOwner}, state: engine.VerificationInvalid},
		{name: "unexpected unit", policies: []engine.QueryRow{userPolicyRow("p-a", "a1")},
			units: []engine.QueryRow{unitRow("p-a", "ou-9")}, state: engine.VerificationInvalid},
		{name: "overlay rule", policies: []engine.QueryRow{userPolicyRow("p-a", "a1")},
			units: fullUnits[:3], rules: []engine.QueryRow{{"policy_id": "p-a"}},
			state: engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
				on(queryUserSharingPolicies, test.policies...).on(queryUserSharingPolicyUnits, test.units...).
				on(queryUserSharingPolicyOverlayRules, test.rules...)
			result, err := NewCreateOriginalUserSharePoliciesOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)
		})
	}
}

func TestRemoveOriginalUserSharePolicies(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	_, completed, err := removeOriginalUserSharePoliciesBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	require.Len(t, access.executed, 2)
	assert.Equal(t, []interface{}{"p-a", "dep"}, access.executed[0].args)

	rollback := NewRollbackCreateOriginalUserSharePoliciesOperation()
	remaining := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
		on(queryUserSharingPolicies, userPolicyRow("p-a", "a1")).on(queryUserSharingPolicyUnits).
		on(queryUserSharingPolicyOverlayRules)
	result, err := rollback.PreVerify(context.Background(), remaining, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)

	removed := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).on(queryUserSharingPolicies)
	result, err = rollback.PostVerify(context.Background(), removed, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}
