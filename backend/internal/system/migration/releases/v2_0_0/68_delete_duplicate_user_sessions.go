// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	deleteDuplicateUserSessionsOperationID engine.OperationID = "2.0.0/runtime_persistent/" +
		"68_delete_duplicate_user_sessions"

	// deleteDuplicateUserSessionsBatchSize is the number of originals whose duplicates' runtime state is
	// removed per batch.
	deleteDuplicateUserSessionsBatchSize = 100
)

// duplicateRuntimeTable is a runtime_persistent table that holds rows for a user. Tables that a deployment
// may not have are skipped when absent.
type duplicateRuntimeTable struct {
	name     string
	optional bool
	remove   dbmodel.DBQuery
	count    dbmodel.DBQuery
}

// The session's context and participants are removed before the session that identifies them.
var duplicateRuntimeTables = []duplicateRuntimeTable{
	{
		name: "SSO_SESSION_CONTEXT",
		remove: dbmodel.DBQuery{ID: "MIG-USM-14-01", PostgresQuery: `DELETE FROM "SSO_SESSION_CONTEXT" c
			USING "SESSION" s
			WHERE c.SESSION_ID = s.SESSION_ID AND c.DEPLOYMENT_ID = s.DEPLOYMENT_ID
				AND s.SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR s.DEPLOYMENT_ID = $2)`},
		count: dbmodel.DBQuery{ID: "MIG-USM-14-02", PostgresQuery: `SELECT COUNT(*) AS row_count
			FROM "SSO_SESSION_CONTEXT" c INNER JOIN "SESSION" s
				ON c.SESSION_ID = s.SESSION_ID AND c.DEPLOYMENT_ID = s.DEPLOYMENT_ID
			WHERE s.SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR s.DEPLOYMENT_ID = $2)`},
	},
	{
		name: "SESSION_PARTICIPANT",
		remove: dbmodel.DBQuery{ID: "MIG-USM-14-03", PostgresQuery: `DELETE FROM "SESSION_PARTICIPANT" p
			USING "SESSION" s
			WHERE p.SESSION_ID = s.SESSION_ID AND p.DEPLOYMENT_ID = s.DEPLOYMENT_ID
				AND s.SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR s.DEPLOYMENT_ID = $2)`},
		count: dbmodel.DBQuery{ID: "MIG-USM-14-04", PostgresQuery: `SELECT COUNT(*) AS row_count
			FROM "SESSION_PARTICIPANT" p INNER JOIN "SESSION" s
				ON p.SESSION_ID = s.SESSION_ID AND p.DEPLOYMENT_ID = s.DEPLOYMENT_ID
			WHERE s.SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR s.DEPLOYMENT_ID = $2)`},
	},
	{
		name: "SESSION",
		remove: dbmodel.DBQuery{ID: "MIG-USM-14-05", PostgresQuery: `DELETE FROM "SESSION"
			WHERE SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
		count: dbmodel.DBQuery{ID: "MIG-USM-14-06", PostgresQuery: `SELECT COUNT(*) AS row_count FROM "SESSION"
			WHERE SUBJECT_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
	},
	{
		name: "ENTITY_RUNTIME_DATA", optional: true,
		remove: dbmodel.DBQuery{ID: "MIG-USM-14-07", PostgresQuery: `DELETE FROM "ENTITY_RUNTIME_DATA"
			WHERE ENTITY_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
		count: dbmodel.DBQuery{ID: "MIG-USM-14-08", PostgresQuery: `SELECT COUNT(*) AS row_count
			FROM "ENTITY_RUNTIME_DATA" WHERE ENTITY_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
	},
	{
		name: "CONSENT_AUTHORIZATION", optional: true,
		remove: dbmodel.DBQuery{ID: "MIG-USM-14-09", PostgresQuery: `DELETE FROM "CONSENT_AUTHORIZATION"
			WHERE USER_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
		count: dbmodel.DBQuery{ID: "MIG-USM-14-10", PostgresQuery: `SELECT COUNT(*) AS row_count
			FROM "CONSENT_AUTHORIZATION" WHERE USER_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`},
	},
}

// newDeleteDuplicateUserSessionsOperation removes the runtime state of the duplicate users before the users
// themselves are deleted: their sessions with each session's participants and context, their entity runtime
// data and their consent authorizations.
func newDeleteDuplicateUserSessionsOperation() engine.Operation {
	return newBatchOperation(deleteDuplicateUserSessionsOperationID,
		"Delete the sessions and runtime data of duplicate users",
		engine.StoreIDRuntimePersistent, []engine.StoreID{engine.StoreIDEntityDB},
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyDuplicateRuntimeStateDeleted),
		verifyDuplicateRuntimeStateDeleted, deleteDuplicateUserSessionsBatch)
}

func deleteDuplicateUserSessionsBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	groups, err := readMigrationGroups(ctx, access, target.DeploymentID)
	if err != nil {
		return checkpoint, false, err
	}
	batch := nextMigrationGroups(groups, checkpoint, deleteDuplicateUserSessionsBatchSize)
	if ids := duplicateIDs(batch); len(ids) > 0 {
		tables, err := presentRuntimeTables(ctx, access)
		if err != nil {
			return checkpoint, false, err
		}
		for _, table := range tables {
			if _, err := access.Execute(ctx, engine.StoreIDRuntimePersistent, table.remove,
				textArray(ids), target.DeploymentID); err != nil {
				return checkpoint, false, fmt.Errorf("delete duplicate users' rows from %s: %w", table.name, err)
			}
		}
	}
	if len(batch) > 0 {
		last := batch[len(batch)-1].original
		checkpoint = batchCheckpoint{DeploymentID: last.deploymentID, Key: last.userID}
	}
	return checkpoint, len(batch) < deleteDuplicateUserSessionsBatchSize, nil
}

func verifyDuplicateRuntimeStateDeleted(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	groups, err := readMigrationGroups(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	ids := duplicateIDs(groups)
	if len(ids) == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: "There are no duplicate users"},
			nil
	}
	tables, err := presentRuntimeTables(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	remaining := int64(0)
	for _, table := range tables {
		rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, table.count, textArray(ids), deploymentID)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("count duplicate users' rows in %s: %w", table.name, err)
		}
		if len(rows) != 1 {
			return engine.VerificationResult{}, fmt.Errorf("count duplicate users' rows in %s: expected one row",
				table.name)
		}
		count, err := rows[0].Int64("row_count")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		remaining += count
	}
	if remaining == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No session or runtime data of a duplicate user remains",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d session and runtime rows of duplicate users remain", remaining)}, nil
}

// presentRuntimeTables returns the runtime tables to clean, leaving out optional tables the store lacks.
func presentRuntimeTables(ctx context.Context, access engine.ReadAccess) ([]duplicateRuntimeTable, error) {
	tables := make([]duplicateRuntimeTable, 0, len(duplicateRuntimeTables))
	for _, table := range duplicateRuntimeTables {
		exists, err := relationExists(ctx, access, engine.StoreIDRuntimePersistent, table.name)
		if err != nil {
			return nil, err
		}
		if !exists {
			if table.optional {
				continue
			}
			return nil, fmt.Errorf("table %s does not exist", table.name)
		}
		tables = append(tables, table)
	}
	return tables, nil
}
