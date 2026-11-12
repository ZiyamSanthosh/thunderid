// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// originalOUCountsRow describes a database with 61 originals.
func originalOUCountsRow(atOwner, atOrigin, missing, elsewhere int64) engine.QueryRow {
	return engine.QueryRow{
		"originals": int64(61), "at_owner": atOwner, "at_origin": atOrigin, "missing": missing,
		"elsewhere": elsewhere,
	}
}

func TestRehomeOriginalUsersBatchMovesTheBatchWithOneStatement(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	checkpoint, completed, err := rehomeOriginalUsersBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	require.Len(t, access.executed, 1)
	assert.Equal(t, queryMoveOriginals.GetID(), access.executed[0].queryID)
	assert.Equal(t, []interface{}{`{"a1","b1"}`, "dep"}, access.executed[0].args)

	rollback := newRollbackRehomeOriginalUsersOperation()
	assert.Equal(t, rehomeOriginalUsersOperationID, rollback.ForwardOperationID())
	back := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)
	_, _, err = returnOriginalUsersBatch(context.Background(), back, engine.ExecutionTarget{DeploymentID: "dep"},
		batchCheckpoint{})
	require.NoError(t, err)
	assert.Equal(t, queryReturnOriginals.GetID(), back.executed[0].queryID)
}

// The previous release's data operations never set UPDATED_AT: it records when a user was last edited, and a
// migration is not an edit. Keeping that also lets the rollback restore the rows exactly.
func TestStatementsThatChangeEntitiesLeaveUpdatedAtAlone(t *testing.T) {
	for name, query := range map[string]string{
		"move originals":            queryMoveOriginals.PostgresQuery,
		"return originals":          queryReturnOriginals.PostgresQuery,
		"merge linked accounts":     queryMergeLinkedAccounts.PostgresQuery,
		"restore original links":    queryRestoreOriginalLinks.PostgresQuery,
		"delete duplicate entities": queryDeleteDuplicateEntities.PostgresQuery,
	} {
		assert.NotContains(t, query, "UPDATED_AT", name)
	}
}

func TestRehomeStatementsTakeBothOrganizationUnitsFromTheWorkingTable(t *testing.T) {
	forward := queryMoveOriginals.PostgresQuery
	assert.Contains(t, forward, `SET OU_ID = m.OWNER_OU_ID`)
	assert.Contains(t, forward, `e.OU_ID = m.OU_ID`, "only an original still in its recorded unit is moved")
	back := queryReturnOriginals.PostgresQuery
	assert.Contains(t, back, `SET OU_ID = m.OU_ID`)
	assert.Contains(t, back, `e.OU_ID = m.OWNER_OU_ID`)
}

func TestVerifyOriginalsRehomed(t *testing.T) {
	tests := []struct {
		name     string
		counts   engine.QueryRow
		forward  engine.VerificationState
		rollback engine.VerificationState
	}{
		{name: "not moved", counts: originalOUCountsRow(0, 61, 0, 0),
			forward: engine.VerificationSource, rollback: engine.VerificationTarget},
		{name: "partly moved", counts: originalOUCountsRow(30, 31, 0, 0),
			forward: engine.VerificationSource, rollback: engine.VerificationSource},
		{name: "moved", counts: originalOUCountsRow(61, 0, 0, 0),
			forward: engine.VerificationTarget, rollback: engine.VerificationSource},
		{name: "moved elsewhere", counts: originalOUCountsRow(30, 30, 0, 1),
			forward: engine.VerificationInvalid, rollback: engine.VerificationInvalid},
		{name: "an original is missing", counts: originalOUCountsRow(60, 0, 1, 0),
			forward: engine.VerificationInvalid, rollback: engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryOriginalOUCounts, test.counts)
			result, err := newRehomeOriginalUsersOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.forward, result.State, result.Summary)
			result, err = newRollbackRehomeOriginalUsersOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.rollback, result.State, result.Summary)
		})
	}
}

func missingLinksRow(links, inAttributes, inIndex int64) engine.QueryRow {
	return engine.QueryRow{"links": links, "missing_in_attributes": inAttributes, "missing_in_index": inIndex}
}

func TestMergeDuplicateLinkedAccountsBatchUsesSetStatements(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).on(queryLinkConflicts)

	checkpoint, completed, err := mergeDuplicateLinkedAccountsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b1"}, checkpoint)
	assert.Equal(t, []string{queryMergeLinkedAccounts.GetID(), queryIndexLinkedAccounts.GetID()},
		access.executedIDs())
	for _, statement := range access.executed {
		assert.Equal(t, []interface{}{`{"a1","b1"}`, "dep"}, statement.args)
	}
}

func TestMergeStatementsUnionLinksByIdpAndSubject(t *testing.T) {
	merge := queryMergeLinkedAccounts.PostgresQuery
	assert.Contains(t, merge, `SELECT DISTINCT m.DEPLOYMENT_ID AS DEP, m.ORIGINAL_USER_ID AS OID,`,
		"a subject held by several users is one row")
	assert.Contains(t, merge, `JSONB_OBJECT_AGG(SUB, '{}'::JSONB)`)
	assert.Contains(t, merge, `JSONB_OBJECT_AGG(IDP, SUBS)`, "one entry per IdP")
	assert.Contains(t, merge, `JSONB_SET(COALESCE(e.SYSTEM_ATTRIBUTES, '{}'::JSONB), '{linkedIds}'`,
		"other system attributes are kept")
	assert.Contains(t, merge, `NOT d.IS_ORIGINAL`, "only people with duplicates are merged")
	assert.Contains(t, queryIndexLinkedAccounts.PostgresQuery, `ON CONFLICT DO NOTHING`)
}

func TestMergeDuplicateLinkedAccountsRefusesDifferentSubjectsOfOneConnection(t *testing.T) {
	conflict := engine.QueryRow{"original_user_id": "a1", "idp": "google"}
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...).on(queryLinkConflicts, conflict)

	_, _, err := mergeDuplicateLinkedAccountsBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})
	require.ErrorContains(t, err, `different subjects of connection "google"`)
	assert.Empty(t, access.executed)

	verified := newFakeAccess().on(queryLinkConflicts, conflict).on(queryMissingLinks, missingLinksRow(2, 0, 0))
	result, err := newMergeDuplicateLinkedAccountsOperation().PreVerify(context.Background(), verified,
		engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, result.State)
	assert.Contains(t, result.Summary, `different subjects of connection "google"`)
}

func TestVerifyDuplicateLinksMerged(t *testing.T) {
	tests := []struct {
		name     string
		counts   engine.QueryRow
		residue  engine.QueryRow
		forward  engine.VerificationState
		rollback engine.VerificationState
	}{
		{name: "not merged", counts: missingLinksRow(12, 2, 2),
			residue: engine.QueryRow{"wrong_links": int64(0), "remaining_identifiers": int64(0)},
			forward: engine.VerificationSource, rollback: engine.VerificationTarget},
		{name: "merged but not indexed", counts: missingLinksRow(12, 0, 2),
			residue: engine.QueryRow{"wrong_links": int64(1), "remaining_identifiers": int64(0)},
			forward: engine.VerificationSource, rollback: engine.VerificationSource},
		{name: "merged and indexed", counts: missingLinksRow(12, 0, 0),
			residue: engine.QueryRow{"wrong_links": int64(1), "remaining_identifiers": int64(2)},
			forward: engine.VerificationTarget, rollback: engine.VerificationSource},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryLinkConflicts).on(queryMissingLinks, test.counts).
				on(queryMergeResidue, test.residue)
			result, err := newMergeDuplicateLinkedAccountsOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.forward, result.State, result.Summary)
			result, err = newRollbackMergeDuplicateLinkedAccountsOperation().PreVerify(context.Background(),
				access, engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.rollback, result.State, result.Summary)
		})
	}
}

func TestRestoreOriginalLinksBatch(t *testing.T) {
	access := newFakeAccess().on(queryMigrationUsers, recordedGroupRows()...)

	_, completed, err := restoreOriginalLinksBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, []string{queryRestoreOriginalLinks.GetID(), queryRemoveMergedIdentifiers.GetID()},
		access.executedIDs())
	assert.Contains(t, queryRemoveMergedIdentifiers.PostgresQuery, `NOT EXISTS`,
		"links the original held itself are kept")
}
