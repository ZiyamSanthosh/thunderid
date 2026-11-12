// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const testRootOU = "root-ou"

func emailKeyRow(emailKey string) engine.QueryRow {
	return engine.QueryRow{"deployment_id": "dep", "email_key": emailKey}
}

// snapshotCountsRow describes a database with 71 users.
func snapshotCountsRow(recorded, unmatched int64) engine.QueryRow {
	return engine.QueryRow{"users": int64(71), "recorded": recorded, "unmatched": unmatched}
}

func TestSnapshotDuplicateUsersDefinition(t *testing.T) {
	operation := newSnapshotDuplicateUsersOperation()
	definition := operation.Definition()
	assert.Equal(t, snapshotDuplicateUsersOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)

	rollback := newRollbackSnapshotDuplicateUsersOperation()
	assert.Equal(t, snapshotDuplicateUsersOperationID, rollback.ForwardOperationID())
	assert.Equal(t, engine.StoreIDEntityDB, rollback.Definition().StoreID)
}

func TestSnapshotDuplicateUsersBatchRecordsEveryUserWithOneStatement(t *testing.T) {
	access := newFakeAccess().on(queryUserEmailKeys, emailKeyRow("a@example.com"), emailKeyRow("b@example.com"))

	checkpoint, completed, err := snapshotDuplicateUsersBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{})

	require.NoError(t, err)
	assert.True(t, completed, "fewer groups than the batch size means nothing is left")
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "b@example.com"}, checkpoint)
	require.Len(t, access.executed, 1, "one INSERT ... SELECT records the whole batch")
	statement := access.executed[0]
	assert.Equal(t, queryInsertMigrationUsers.GetID(), statement.queryID)
	require.Len(t, statement.args, 3)
	assert.Equal(t, `{"dep","dep"}`, statement.args[0])
	assert.Equal(t, `{"a@example.com","b@example.com"}`, statement.args[1])
	policyIDs, ok := statement.args[2].(string)
	require.True(t, ok)
	assert.Regexp(t, `^\{"[0-9a-f-]{36}","[0-9a-f-]{36}"\}$`, policyIDs, "one new policy ID per person")
}

func TestSnapshotDuplicateUsersBatchPagesByEmail(t *testing.T) {
	var seenArgs []interface{}
	access := newFakeAccess().onArgs(queryUserEmailKeys, func(args []interface{}) []engine.QueryRow {
		seenArgs = args
		return nil
	})

	checkpoint, completed, err := snapshotDuplicateUsersBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{DeploymentID: "dep", Key: "a@example.com"})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, batchCheckpoint{DeploymentID: "dep", Key: "a@example.com"}, checkpoint)
	assert.Equal(t, []interface{}{"dep", "dep", "a@example.com", snapshotDuplicateUsersBatchSize}, seenArgs)
	assert.Empty(t, access.executed)
}

func TestSnapshotDuplicateUsersBatchIsNotCompleteAfterAFullPage(t *testing.T) {
	rows := make([]engine.QueryRow, 0, snapshotDuplicateUsersBatchSize)
	for index := 0; index < snapshotDuplicateUsersBatchSize; index++ {
		rows = append(rows, emailKeyRow(fmt.Sprintf("user%03d@example.com", index)))
	}
	access := newFakeAccess().on(queryUserEmailKeys, rows...)

	checkpoint, completed, err := snapshotDuplicateUsersBatch(context.Background(), access,
		engine.ExecutionTarget{}, batchCheckpoint{})

	require.NoError(t, err)
	assert.False(t, completed)
	assert.Equal(t, "user099@example.com", checkpoint.Key)
}

func TestSnapshotDuplicateUsersStatementShape(t *testing.T) {
	query := queryInsertMigrationUsers.PostgresQuery
	assert.Contains(t, query, `ROW_NUMBER() OVER w`)
	assert.Contains(t, query, `LEFT JOIN "ORGANIZATION_UNIT"`)
	assert.Contains(t, query, `ON CONFLICT (DEPLOYMENT_ID, USER_ID) DO NOTHING`)
	assert.Contains(t, query, `CASE WHEN u.RN = 1 THEN u.POLICY_ID END`)
	assert.Contains(t, query, `CASE WHEN u.RN = 1 THEN u.PARENT_ID END`)
}

func TestVerifySnapshotDuplicateUsers(t *testing.T) {
	tests := []struct {
		name     string
		problems []engine.QueryRow
		counts   engine.QueryRow
		state    engine.VerificationState
		summary  string
	}{
		{name: "nothing recorded", counts: snapshotCountsRow(0, 0), state: engine.VerificationSource},
		{name: "partly recorded", counts: snapshotCountsRow(30, 0), state: engine.VerificationSource,
			summary: "30 of 71"},
		{name: "every user recorded", counts: snapshotCountsRow(71, 0), state: engine.VerificationTarget},
		{name: "a recorded user is gone", counts: snapshotCountsRow(71, 2), state: engine.VerificationInvalid,
			summary: "no longer users"},
		{name: "a group cannot be shared from one parent", problems: []engine.QueryRow{{
			"deployment_id": "dep", "email_key": "a@example.com",
			"reason": "organization units have different parents or no parent",
		}}, counts: snapshotCountsRow(0, 0), state: engine.VerificationInvalid,
			summary: `Users with email "a@example.com": organization units have different parents`},
		{name: "users linked to different subjects", problems: []engine.QueryRow{{
			"deployment_id": "dep", "email_key": "a@example.com",
			"reason": "users are linked to different subjects of connection google",
		}}, counts: snapshotCountsRow(0, 0), state: engine.VerificationInvalid,
			summary: "different subjects of connection google"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryUnmigratableGroups, test.problems...).
				on(querySnapshotCounts, test.counts)
			result, err := newSnapshotDuplicateUsersOperation().PreVerify(context.Background(), access,
				engine.ExecutionTarget{DeploymentID: "dep"})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)
			assert.Contains(t, result.Summary, test.summary)
		})
	}
}

func TestSnapshotDuplicateUsersReadiness(t *testing.T) {
	operation := newSnapshotDuplicateUsersOperation().(engine.ReadinessChecker)

	readiness, err := operation.CheckReadiness(context.Background(), newFakeAccess().relations(),
		engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, readiness.State)
	assert.Equal(t, []engine.OperationID{createUserShareMigrationTableOperationID}, readiness.DeferredUntil)

	blocked := newFakeAccess().relations(userShareMigrationTableName).on(queryUnmigratableGroups, engine.QueryRow{
		"deployment_id": "dep", "email_key": "a@example.com", "reason": "users have different types",
	}).on(querySnapshotCounts, snapshotCountsRow(0, 0))
	readiness, err = operation.CheckReadiness(context.Background(), blocked, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
	assert.Len(t, readiness.Findings, 1)

	ready := newFakeAccess().relations(userShareMigrationTableName).on(queryUnmigratableGroups).
		on(querySnapshotCounts, snapshotCountsRow(0, 0))
	readiness, err = operation.CheckReadiness(context.Background(), ready, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, readiness.State)
}

func TestClearMigrationTableBatch(t *testing.T) {
	access := newFakeAccess()
	access.affected[queryClearMigrationUsers.GetID()] = func([]interface{}) int64 { return 3 }

	checkpoint, completed, err := clearMigrationTableBatch(context.Background(), access,
		engine.ExecutionTarget{DeploymentID: "dep"}, batchCheckpoint{Processed: 500})

	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, int64(503), checkpoint.Processed)
	assert.Equal(t, []interface{}{"dep", clearMigrationTableBatchSize}, access.executed[0].args)

	rollback := newRollbackSnapshotDuplicateUsersOperation()
	result, err := rollback.PreVerify(context.Background(), newFakeAccess().on(queryMigrationUsers),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}
