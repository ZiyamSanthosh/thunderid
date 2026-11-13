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

const testDuplicateIDs = `{"a2","a3","b2"}`

func TestDeleteDuplicateUserSessionsBatch(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
		relations("SSO_SESSION_CONTEXT", "SESSION_PARTICIPANT", "SESSION", "CONSENT_AUTHORIZATION")

	checkpoint, completed, err := deleteDuplicateUserSessionsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	assert.Equal(t, []string{"MIG-USM-14-01", "MIG-USM-14-03", "MIG-USM-14-05", "MIG-USM-14-09"},
		access.executedIDs(), "context and participants go before the session; absent ENTITY_RUNTIME_DATA is skipped")
	for _, statement := range access.executed {
		assert.Equal(t, engine.StoreIDRuntimePersistent, statement.storeID)
		assert.Equal(t, []interface{}{testDuplicateIDs, "dep"}, statement.args)
	}
}

func TestDeleteDuplicateUserSessionsRequiresSessionTables(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).relations("SSO_SESSION_CONTEXT")

	_, _, err := deleteDuplicateUserSessionsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.ErrorContains(t, err, "SESSION_PARTICIPANT does not exist")
}

func TestVerifyDuplicateRuntimeStateDeleted(t *testing.T) {
	counts := func(count int64) *fakeAccess {
		access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
			relations("SSO_SESSION_CONTEXT", "SESSION_PARTICIPANT", "SESSION")
		for _, table := range duplicateRuntimeTables {
			access.on(table.count, engine.QueryRow{"row_count": count})
		}
		return access
	}
	operation := NewDeleteDuplicateUserSessionsOperation()
	result, err := operation.PreVerify(context.Background(), counts(2), engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)
	result, err = operation.PostVerify(context.Background(), counts(0), engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}

func TestDeleteDuplicateUserAssignments(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	_, completed, err := deleteDuplicateUserAssignmentsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, []string{queryDeleteDuplicateRoleAssignments.GetID(), queryDeleteDuplicateSharingPolicies.GetID()},
		access.executedIDs())
	assert.Equal(t, []interface{}{testDuplicateIDs, "dep"}, access.executed[0].args)

	operation := NewDeleteDuplicateUserAssignmentsOperation()
	assert.Equal(t, []engine.StoreID{engine.StoreIDEntityDB}, operation.Definition().ReadStoreIDs)
	remaining := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
		on(queryDirectUserRoleAssignments, engine.QueryRow{"assignee_id": "a2"}).on(queryUserSharingPolicies)
	result, err := operation.PreVerify(context.Background(), remaining, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)
	clean := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
		on(queryDirectUserRoleAssignments).on(queryUserSharingPolicies)
	result, err = operation.PostVerify(context.Background(), clean, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}

func duplicateCoverageRow(remaining, missingOriginals, uncovered int64) engine.QueryRow {
	return engine.QueryRow{
		"remaining": remaining, "missing_originals": missingOriginals, "uncovered_memberships": uncovered,
	}
}

func TestDeleteDuplicateUsersBatchDeletesThroughTheWorkingTable(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
		on(queryDuplicateCoverage, duplicateCoverageRow(10, 0, 0)).on(queryMissingLinks, missingLinksRow(12, 0, 0))

	checkpoint, completed, err := deleteDuplicateUsersBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	assert.Equal(t, []string{queryDeleteDuplicateMemberships.GetID(), queryDeleteDuplicateEntities.GetID()},
		access.executedIDs())
	for _, statement := range access.executed {
		assert.Equal(t, []interface{}{`{"a1","b1"}`, "dep"}, statement.args)
	}
	assert.Contains(t, queryDeleteDuplicateEntities.PostgresQuery, `NOT d.IS_ORIGINAL`)
}

func TestDeleteDuplicateUsersBatchKeepsDuplicatesOfAnIncompleteOriginal(t *testing.T) {
	tests := []struct {
		name     string
		coverage engine.QueryRow
		links    engine.QueryRow
		finding  string
	}{
		{name: "original is gone", coverage: duplicateCoverageRow(10, 1, 0), links: missingLinksRow(12, 0, 0),
			finding: "originals no longer exist"},
		{name: "links not merged", coverage: duplicateCoverageRow(10, 0, 0), links: missingLinksRow(12, 1, 0),
			finding: "links of duplicates are missing"},
		{name: "group not copied", coverage: duplicateCoverageRow(10, 0, 3), links: missingLinksRow(12, 0, 0),
			finding: "group memberships of duplicates are missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).
				on(queryDuplicateCoverage, test.coverage).on(queryMissingLinks, test.links)

			_, _, err := deleteDuplicateUsersBatch(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})
			require.ErrorContains(t, err, test.finding)
			assert.Empty(t, access.executed)

			result, err := NewDeleteDuplicateUsersOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, engine.VerificationInvalid, result.State)
		})
	}
}

func TestVerifyDuplicateUsersDeleted(t *testing.T) {
	operation := NewDeleteDuplicateUsersOperation()
	deleted := newFakeAccess().on(queryDuplicateCoverage, duplicateCoverageRow(0, 0, 0)).
		on(queryMissingLinks, missingLinksRow(12, 0, 0))
	result, err := operation.PostVerify(context.Background(), deleted, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)

	pending := newFakeAccess().on(queryDuplicateCoverage, duplicateCoverageRow(10, 0, 0)).
		on(queryMissingLinks, missingLinksRow(12, 0, 0))
	result, err = operation.PreVerify(context.Background(), pending, engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)
}

func TestDropUserShareMigrationTable(t *testing.T) {
	operation, err := NewDropUserShareMigrationTableOperation()
	require.NoError(t, err)
	assert.Equal(t, engine.OperationKindSchemaChange, operation.Definition().Kind)

	result, err := operation.PreVerify(context.Background(), newFakeAccess().relations(userShareMigrationTableName),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, result.State)
	result, err = operation.PostVerify(context.Background(), newFakeAccess().relations(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}
