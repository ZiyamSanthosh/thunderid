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
	deleteDuplicateUserAssignmentsOperationID engine.OperationID = "2.0.0/configdb/" +
		"69_delete_duplicate_user_assignments"

	// deleteDuplicateUserAssignmentsBatchSize is the number of originals whose duplicates' configuration rows
	// are removed per batch.
	deleteDuplicateUserAssignmentsBatchSize = 100
)

var (
	queryDeleteDuplicateRoleAssignments = dbmodel.DBQuery{
		ID: "MIG-USM-15-01",
		PostgresQuery: `DELETE FROM "ROLE_ASSIGNMENT"
			WHERE ASSIGNEE_TYPE = 'entity' AND ASSIGNEE_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`,
	}
	queryDeleteDuplicateSharingPolicies = dbmodel.DBQuery{
		ID: "MIG-USM-15-02",
		PostgresQuery: `DELETE FROM "RESOURCE_SHARING_POLICY"
			WHERE RESOURCE_TYPE = 'user' AND RESOURCE_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`,
	}
)

// newDeleteDuplicateUserAssignmentsOperation removes the configuration that names a duplicate user: direct
// role assignments and sharing policies of the user. Operation 10's checks keep the first at zero; both are
// removed so no reference to a deleted user remains. A policy's units and overlay rules cascade.
func newDeleteDuplicateUserAssignmentsOperation() engine.Operation {
	return newBatchOperation(deleteDuplicateUserAssignmentsOperationID,
		"Delete the role assignments and sharing policies of duplicate users",
		engine.StoreIDConfigDB, []engine.StoreID{engine.StoreIDEntityDB},
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyDuplicateAssignmentsDeleted),
		verifyDuplicateAssignmentsDeleted, deleteDuplicateUserAssignmentsBatch)
}

func deleteDuplicateUserAssignmentsBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	groups, err := readMigrationGroups(ctx, access, target.DeploymentID)
	if err != nil {
		return checkpoint, false, err
	}
	batch := nextMigrationGroups(groups, checkpoint, deleteDuplicateUserAssignmentsBatchSize)
	if ids := duplicateIDs(batch); len(ids) > 0 {
		for _, query := range []dbmodel.DBQuery{
			queryDeleteDuplicateRoleAssignments, queryDeleteDuplicateSharingPolicies,
		} {
			if _, err := access.Execute(ctx, engine.StoreIDConfigDB, query, textArray(ids),
				target.DeploymentID); err != nil {
				return checkpoint, false, fmt.Errorf("delete duplicate users' configuration: %w", err)
			}
		}
	}
	if len(batch) > 0 {
		last := batch[len(batch)-1].original
		checkpoint = batchCheckpoint{DeploymentID: last.deploymentID, Key: last.userID}
	}
	return checkpoint, len(batch) < deleteDuplicateUserAssignmentsBatchSize, nil
}

func verifyDuplicateAssignmentsDeleted(ctx context.Context, access engine.ReadAccess, deploymentID string) (
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
	remaining := 0
	for _, query := range []dbmodel.DBQuery{queryDirectUserRoleAssignments, queryUserSharingPolicies} {
		rows, err := access.Query(ctx, engine.StoreIDConfigDB, query, textArray(ids), deploymentID)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("read duplicate users' configuration: %w", err)
		}
		remaining += len(rows)
	}
	if remaining == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No role assignment or sharing policy names a duplicate user",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d role assignments or sharing policies name duplicate users", remaining)}, nil
}
