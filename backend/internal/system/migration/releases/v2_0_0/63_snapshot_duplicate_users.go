// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/internal/system/utils"
)

const (
	snapshotDuplicateUsersOperationID engine.OperationID = "2.0.0/entitydb/63_snapshot_duplicate_users"
	snapshotDuplicateUsersRollbackID  engine.OperationID = "2.0.0/entitydb/" +
		"63_snapshot_duplicate_users_rollback"

	// snapshotDuplicateUsersBatchSize is the number of email groups recorded per batch.
	snapshotDuplicateUsersBatchSize = 100
	// clearMigrationTableBatchSize is the number of working-table rows the rollback removes per batch.
	clearMigrationTableBatchSize = 500

	// emailKeySQL is the key that groups users into one person: the trimmed, case-folded email. A user without
	// an email is a person of its own. Only people with more than one user are converted.
	emailKeySQL = `COALESCE(NULLIF(LOWER(BTRIM(e.ATTRIBUTES ->> 'email')), ''), 'id:' || e.ID)`
)

var (
	// queryUserEmailKeys returns the next page of email groups after the checkpoint.
	queryUserEmailKeys = dbmodel.DBQuery{
		ID: "MIG-USM-08-01",
		PostgresQuery: `SELECT DEPLOYMENT_ID, EMAIL_KEY FROM (
				SELECT e.DEPLOYMENT_ID, ` + emailKeySQL + ` AS EMAIL_KEY
				FROM "ENTITY" e
				WHERE e.CATEGORY = 'user' AND ($1 = '' OR e.DEPLOYMENT_ID = $1)
				GROUP BY e.DEPLOYMENT_ID, ` + emailKeySQL + `
				HAVING COUNT(*) > 1
			) k
			WHERE DEPLOYMENT_ID > $2 OR (DEPLOYMENT_ID = $2 AND EMAIL_KEY > $3)
			ORDER BY DEPLOYMENT_ID, EMAIL_KEY
			LIMIT $4`,
	}
	// queryInsertMigrationUsers records every user of the given email groups in one statement: the oldest user
	// of a group is the original, and it alone carries the owner organization unit (the parent of its own
	// unit) and the ID of the sharing policy that will be created for it. $1 and $2 name the groups, $3 holds
	// one new policy ID per group.
	queryInsertMigrationUsers = dbmodel.DBQuery{
		ID: "MIG-USM-08-02",
		PostgresQuery: `INSERT INTO "USER_SHARE_MIGRATION"
				(DEPLOYMENT_ID, USER_ID, IS_ORIGINAL, ORIGINAL_USER_ID, EMAIL, OU_ID, LINKED_ACCOUNTS,
					SHARING_POLICY_ID, OWNER_OU_ID, CREATED_AT)
			SELECT u.DEPLOYMENT_ID, u.ID, u.RN = 1, u.FIRST_ID, u.EMAIL_KEY, u.OU_ID,
				COALESCE(u.LINKS, '{}'::JSONB), CASE WHEN u.RN = 1 THEN u.POLICY_ID END,
				CASE WHEN u.RN = 1 THEN u.PARENT_ID END, u.CREATED_AT
			FROM (
				SELECT e.DEPLOYMENT_ID, e.ID, e.OU_ID, e.CREATED_AT, p.EKEY AS EMAIL_KEY, p.POLICY_ID,
					o.PARENT_ID, e.SYSTEM_ATTRIBUTES -> 'linkedIds' AS LINKS,
					ROW_NUMBER() OVER w AS RN, FIRST_VALUE(e.ID) OVER w AS FIRST_ID
				FROM "ENTITY" e
				INNER JOIN UNNEST($1::TEXT[], $2::TEXT[], $3::TEXT[]) AS p(DEP, EKEY, POLICY_ID)
					ON p.DEP = e.DEPLOYMENT_ID AND p.EKEY = ` + emailKeySQL + `
				LEFT JOIN "ORGANIZATION_UNIT" o ON o.DEPLOYMENT_ID = e.DEPLOYMENT_ID AND o.OU_ID = e.OU_ID
				WHERE e.CATEGORY = 'user'
				WINDOW w AS (PARTITION BY e.DEPLOYMENT_ID, p.EKEY ORDER BY e.CREATED_AT, e.ID)
			) u
			ON CONFLICT (DEPLOYMENT_ID, USER_ID) DO NOTHING`,
	}
	queryClearMigrationUsers = dbmodel.DBQuery{
		ID: "MIG-USM-08-03",
		PostgresQuery: `DELETE FROM "USER_SHARE_MIGRATION"
			WHERE (DEPLOYMENT_ID, USER_ID) IN (
				SELECT DEPLOYMENT_ID, USER_ID FROM "USER_SHARE_MIGRATION"
				WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
				ORDER BY DEPLOYMENT_ID, USER_ID
				LIMIT $2)`,
	}
	// queryUnmigratableGroups lists the email groups the migration cannot share from one parent organization
	// unit, one row per group and reason.
	queryUnmigratableGroups = dbmodel.DBQuery{
		ID: "MIG-USM-08-04",
		PostgresQuery: `WITH users AS (
				SELECT e.DEPLOYMENT_ID, ` + emailKeySQL + ` AS EMAIL_KEY, e.ID, e.TYPE, e.OU_ID, o.PARENT_ID,
					e.SYSTEM_ATTRIBUTES -> 'linkedIds' AS LINKS
				FROM "ENTITY" e
				LEFT JOIN "ORGANIZATION_UNIT" o ON o.DEPLOYMENT_ID = e.DEPLOYMENT_ID AND o.OU_ID = e.OU_ID
				WHERE e.CATEGORY = 'user' AND ($1 = '' OR e.DEPLOYMENT_ID = $1)
			), subjects AS (
				SELECT u.DEPLOYMENT_ID, u.EMAIL_KEY, c.KEY AS IDP, COUNT(DISTINCT s.KEY) AS SUBJECTS
				FROM users u, JSONB_EACH(COALESCE(u.LINKS, '{}'::JSONB)) c, JSONB_OBJECT_KEYS(c.VALUE) s(KEY)
				WHERE (u.DEPLOYMENT_ID, u.EMAIL_KEY) IN (
					SELECT DEPLOYMENT_ID, EMAIL_KEY FROM users GROUP BY DEPLOYMENT_ID, EMAIL_KEY HAVING COUNT(*) > 1)
				GROUP BY u.DEPLOYMENT_ID, u.EMAIL_KEY, c.KEY
			)
			SELECT DEPLOYMENT_ID, EMAIL_KEY, REASON FROM (
				SELECT DEPLOYMENT_ID, EMAIL_KEY, 'organization units have different parents or no parent' AS REASON
				FROM users GROUP BY DEPLOYMENT_ID, EMAIL_KEY
				HAVING COUNT(*) > 1 AND (COUNT(DISTINCT PARENT_ID) > 1 OR BOOL_OR(PARENT_ID IS NULL))
				UNION ALL
				SELECT DEPLOYMENT_ID, EMAIL_KEY, 'two users are in the same organization unit'
				FROM users GROUP BY DEPLOYMENT_ID, EMAIL_KEY HAVING COUNT(DISTINCT OU_ID) < COUNT(*)
				UNION ALL
				SELECT DEPLOYMENT_ID, EMAIL_KEY, 'users have different types'
				FROM users GROUP BY DEPLOYMENT_ID, EMAIL_KEY HAVING COUNT(DISTINCT TYPE) > 1
				UNION ALL
				SELECT DEPLOYMENT_ID, EMAIL_KEY, 'users are linked to different subjects of connection ' || IDP
				FROM subjects WHERE SUBJECTS > 1
			) f
			ORDER BY DEPLOYMENT_ID, EMAIL_KEY, REASON`,
	}
	// querySnapshotCounts compares the working table with the users.
	querySnapshotCounts = dbmodel.DBQuery{
		ID: "MIG-USM-08-05",
		PostgresQuery: `SELECT
				(SELECT COALESCE(SUM(g.members), 0)::BIGINT FROM (
					SELECT COUNT(*) AS members FROM "ENTITY" e
					WHERE e.CATEGORY = 'user' AND ($1 = '' OR e.DEPLOYMENT_ID = $1)
					GROUP BY e.DEPLOYMENT_ID, ` + emailKeySQL + `
					HAVING COUNT(*) > 1) g) AS users,
				(SELECT COUNT(*) FROM "USER_SHARE_MIGRATION" m
					WHERE ($1 = '' OR m.DEPLOYMENT_ID = $1)) AS recorded,
				(SELECT COUNT(*) FROM "USER_SHARE_MIGRATION" m
					WHERE ($1 = '' OR m.DEPLOYMENT_ID = $1) AND NOT EXISTS (
						SELECT 1 FROM "ENTITY" e WHERE e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID
							AND e.CATEGORY = 'user')) AS unmatched`,
	}
)

// emailGroupKey names one group of users that share an email.
type emailGroupKey struct {
	deploymentID string
	emailKey     string
}

func newSnapshotDuplicateUsersOperation() engine.Operation {
	return newBatchOperation(snapshotDuplicateUsersOperationID,
		"Record every user by email, the original that survives and the organization unit that will own it",
		engine.StoreIDEntityDB, nil,
		readinessAfterMigrationTable(createUserShareMigrationTableOperationID, verifySnapshotDuplicateUsers),
		verifySnapshotDuplicateUsers, snapshotDuplicateUsersBatch)
}

func newRollbackSnapshotDuplicateUsersOperation() engine.RollbackOperation {
	return &rollbackBatchOperation{
		batchOperation: newBatchOperation(snapshotDuplicateUsersRollbackID,
			"Remove the recorded users", engine.StoreIDEntityDB, nil, nil,
			verifyMigrationTableCleared, clearMigrationTableBatch),
		forwardID: snapshotDuplicateUsersOperationID,
	}
}

// snapshotDuplicateUsersBatch records the next page of email groups with one INSERT ... SELECT over the
// entity and organization unit tables.
func snapshotDuplicateUsersBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	keys, err := readEmailKeys(ctx, access, target.DeploymentID, checkpoint, snapshotDuplicateUsersBatchSize)
	if err != nil {
		return checkpoint, false, err
	}
	if len(keys) == 0 {
		return checkpoint, true, nil
	}
	deployments := make([]string, 0, len(keys))
	emailKeys := make([]string, 0, len(keys))
	policyIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		policyID, err := utils.GenerateUUIDv7()
		if err != nil {
			return checkpoint, false, fmt.Errorf("generate sharing policy ID: %w", err)
		}
		deployments = append(deployments, key.deploymentID)
		emailKeys = append(emailKeys, key.emailKey)
		policyIDs = append(policyIDs, policyID)
	}
	if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryInsertMigrationUsers,
		textArray(deployments), textArray(emailKeys), textArray(policyIDs)); err != nil {
		return checkpoint, false, fmt.Errorf("record users: %w", err)
	}
	last := keys[len(keys)-1]
	next := batchCheckpoint{DeploymentID: last.deploymentID, Key: last.emailKey}
	return next, len(keys) < snapshotDuplicateUsersBatchSize, nil
}

func readEmailKeys(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
	checkpoint batchCheckpoint,
	limit int,
) ([]emailGroupKey, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryUserEmailKeys,
		deploymentID, checkpoint.DeploymentID, checkpoint.Key, limit)
	if err != nil {
		return nil, fmt.Errorf("read email groups: %w", err)
	}
	keys := make([]emailGroupKey, 0, len(rows))
	for _, row := range rows {
		deployment, deploymentErr := row.String("deployment_id")
		emailKey, emailErr := row.String("email_key")
		if err := errors.Join(deploymentErr, emailErr); err != nil {
			return nil, fmt.Errorf("read email group: %w", err)
		}
		keys = append(keys, emailGroupKey{deploymentID: deployment, emailKey: emailKey})
	}
	return keys, nil
}

func clearMigrationTableBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	deleted, err := access.Execute(ctx, engine.StoreIDEntityDB, queryClearMigrationUsers,
		target.DeploymentID, clearMigrationTableBatchSize)
	if err != nil {
		return checkpoint, false, fmt.Errorf("clear user share migration table: %w", err)
	}
	checkpoint.Processed += deleted
	return checkpoint, deleted < clearMigrationTableBatchSize, nil
}

// verifySnapshotDuplicateUsers is invalid when an email group cannot be shared from one parent organization
// unit, or when a recorded user is no longer a user. Otherwise it is at source until every user is recorded.
func verifySnapshotDuplicateUsers(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	problems, err := access.Query(ctx, engine.StoreIDEntityDB, queryUnmigratableGroups, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("check email groups: %w", err)
	}
	if len(problems) > 0 {
		findings := make([]string, 0, len(problems))
		for _, row := range problems {
			emailKey, emailErr := row.String("email_key")
			reason, reasonErr := row.String("reason")
			if err := errors.Join(emailErr, reasonErr); err != nil {
				return engine.VerificationResult{}, fmt.Errorf("read email group problem: %w", err)
			}
			findings = append(findings, fmt.Sprintf("Users with email %q: %s", emailKey, reason))
		}
		return invalidVerification("duplicate_users_not_migratable", strings.Join(findings, "; ")), nil
	}
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, querySnapshotCounts, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("count recorded users: %w", err)
	}
	if len(rows) != 1 {
		return engine.VerificationResult{}, fmt.Errorf("count recorded users: expected one row, found %d", len(rows))
	}
	users, usersErr := rows[0].Int64("users")
	recorded, recordedErr := rows[0].Int64("recorded")
	unmatched, unmatchedErr := rows[0].Int64("unmatched")
	if err := errors.Join(usersErr, recordedErr, unmatchedErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if unmatched > 0 {
		return invalidVerification("unexpected_duplicate_user_record", fmt.Sprintf(
			"%d recorded users are no longer users in the entity tables", unmatched)), nil
	}
	if recorded >= users {
		return engine.VerificationResult{State: engine.VerificationTarget,
			Summary: fmt.Sprintf("All %d users are recorded", recorded)}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource,
		Summary: fmt.Sprintf("%d of %d users are recorded", recorded, users)}, nil
}

func verifyMigrationTableCleared(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	users, err := readMigrationUsers(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if len(users) == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "The user share migration table is empty",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"The user share migration table holds %d users", len(users))}, nil
}
