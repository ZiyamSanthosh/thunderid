// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	rehomeOriginalUsersOperationID engine.OperationID = "2.0.0/entitydb/66_rehome_original_users"
	rehomeOriginalUsersRollbackID  engine.OperationID = "2.0.0/entitydb/" +
		"66_rehome_original_users_rollback"

	// rehomeOriginalUsersBatchSize is the number of originals moved per batch.
	rehomeOriginalUsersBatchSize = 100
)

var (
	// The statements leave UPDATED_AT alone, as every data operation of the previous release does: the column
	// records when a user was last edited, and moving the owner is not an edit. It also lets the rollback
	// restore the rows exactly.
	//
	// queryMoveOriginals moves the batch's originals from the organization unit the snapshot recorded to the
	// owner organization unit, taking both from the working table. An original that is anywhere else was moved
	// by someone else and is left alone; the verification then reports it.
	queryMoveOriginals = dbmodel.DBQuery{
		ID: "MIG-USM-12-01",
		PostgresQuery: `UPDATE "ENTITY" e SET OU_ID = m.OWNER_OU_ID
			FROM "USER_SHARE_MIGRATION" m
			WHERE m.USER_ID = ANY($1::TEXT[]) AND m.IS_ORIGINAL AND ($2 = '' OR m.DEPLOYMENT_ID = $2)
				AND e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND e.CATEGORY = 'user'
				AND e.OU_ID = m.OU_ID`,
	}
	queryReturnOriginals = dbmodel.DBQuery{
		ID: "MIG-USM-12-02",
		PostgresQuery: `UPDATE "ENTITY" e SET OU_ID = m.OU_ID
			FROM "USER_SHARE_MIGRATION" m
			WHERE m.USER_ID = ANY($1::TEXT[]) AND m.IS_ORIGINAL AND ($2 = '' OR m.DEPLOYMENT_ID = $2)
				AND e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND e.CATEGORY = 'user'
				AND e.OU_ID = m.OWNER_OU_ID`,
	}
	// queryOriginalOUCounts counts the originals by where they are now compared with the working table.
	queryOriginalOUCounts = dbmodel.DBQuery{
		ID: "MIG-USM-12-03",
		PostgresQuery: `SELECT
				COUNT(*) AS originals,
				COUNT(*) FILTER (WHERE e.OU_ID = m.OWNER_OU_ID) AS at_owner,
				COUNT(*) FILTER (WHERE e.OU_ID = m.OU_ID) AS at_origin,
				COUNT(*) FILTER (WHERE e.ID IS NULL) AS missing,
				COUNT(*) FILTER (WHERE e.ID IS NOT NULL AND e.OU_ID <> m.OWNER_OU_ID
					AND e.OU_ID <> m.OU_ID) AS elsewhere
			FROM "USER_SHARE_MIGRATION" m
			LEFT JOIN "ENTITY" e ON e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID
				AND e.CATEGORY = 'user'
			WHERE m.IS_ORIGINAL AND ($1 = '' OR m.DEPLOYMENT_ID = $1)`,
	}
)

// newRehomeOriginalUsersOperation moves each original to its owner organization unit, the common parent that
// shares it to its former unit and to its duplicates' units. It runs in migrate, because the previous release
// lists and manages users by their own organization unit only.
func newRehomeOriginalUsersOperation() engine.Operation {
	return newBatchOperation(rehomeOriginalUsersOperationID,
		"Move each original user to the organization unit that shares it",
		engine.StoreIDEntityDB, nil,
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyOriginalsRehomed),
		verifyOriginalsRehomed, rehomeOriginalUsersBatch)
}

func newRollbackRehomeOriginalUsersOperation() engine.RollbackOperation {
	return &rollbackBatchOperation{
		batchOperation: newBatchOperation(rehomeOriginalUsersRollbackID,
			"Move each original user back to its former organization unit",
			engine.StoreIDEntityDB, nil, nil, verifyOriginalsReturned, returnOriginalUsersBatch),
		forwardID: rehomeOriginalUsersOperationID,
	}
}

func rehomeOriginalUsersBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, rehomeOriginalUsersBatchSize,
		func(originalIDs []string) error {
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryMoveOriginals,
				textArray(originalIDs), target.DeploymentID); err != nil {
				return fmt.Errorf("move original users to their owner organization unit: %w", err)
			}
			return nil
		})
}

func returnOriginalUsersBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, rehomeOriginalUsersBatchSize,
		func(originalIDs []string) error {
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryReturnOriginals,
				textArray(originalIDs), target.DeploymentID); err != nil {
				return fmt.Errorf("move original users back to their former organization unit: %w", err)
			}
			return nil
		})
}

func verifyOriginalsRehomed(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	return verifyOriginalOUs(ctx, access, deploymentID, true)
}

func verifyOriginalsReturned(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	return verifyOriginalOUs(ctx, access, deploymentID, false)
}

// verifyOriginalOUs is at target when every original is in its owner unit (or, when returning, its former
// unit), at source while some are still in the other one, and invalid when an original is missing or anywhere
// else.
func verifyOriginalOUs(ctx context.Context, access engine.ReadAccess, deploymentID string, toOwner bool) (
	engine.VerificationResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryOriginalOUCounts, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("count original users by organization unit: %w", err)
	}
	if len(rows) != 1 {
		return engine.VerificationResult{}, fmt.Errorf("count original users: expected one row, found %d", len(rows))
	}
	originals, originalsErr := rows[0].Int64("originals")
	atOwner, ownerErr := rows[0].Int64("at_owner")
	atOrigin, originErr := rows[0].Int64("at_origin")
	missing, missingErr := rows[0].Int64("missing")
	elsewhere, elsewhereErr := rows[0].Int64("elsewhere")
	if err := errors.Join(originalsErr, ownerErr, originErr, missingErr, elsewhereErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if missing > 0 || elsewhere > 0 {
		return invalidVerification("original_user_moved", fmt.Sprintf(
			"%d original users are missing and %d are in an organization unit that is neither their former nor "+
				"their owner unit", missing, elsewhere)), nil
	}
	pending, done := atOwner, atOrigin
	if toOwner {
		pending, done = atOrigin, atOwner
	}
	if pending == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: fmt.Sprintf(
			"All %d original users are in their expected organization unit", originals)}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d of %d original users are still to be moved", pending, done+pending)}, nil
}
