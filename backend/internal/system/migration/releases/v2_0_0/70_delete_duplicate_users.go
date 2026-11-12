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
)

const (
	deleteDuplicateUsersOperationID engine.OperationID = "2.0.0/entitydb/70_delete_duplicate_users"

	// deleteDuplicateUsersBatchSize is the number of originals whose duplicates are deleted per batch.
	deleteDuplicateUsersBatchSize = 100
)

var (
	queryDeleteDuplicateMemberships = dbmodel.DBQuery{
		ID: "MIG-USM-16-01",
		PostgresQuery: `DELETE FROM "GROUP_MEMBER_REFERENCE" g
			USING "USER_SHARE_MIGRATION" d
			WHERE g.MEMBER_TYPE = 'entity' AND g.MEMBER_ID = d.USER_ID AND g.DEPLOYMENT_ID = d.DEPLOYMENT_ID
				AND NOT d.IS_ORIGINAL AND d.ORIGINAL_USER_ID = ANY($1::TEXT[]) AND ($2 = '' OR d.DEPLOYMENT_ID = $2)`,
	}
	// ENTITY_IDENTIFIER rows cascade with the entity.
	queryDeleteDuplicateEntities = dbmodel.DBQuery{
		ID: "MIG-USM-16-02",
		PostgresQuery: `DELETE FROM "ENTITY" e
			USING "USER_SHARE_MIGRATION" d
			WHERE e.ID = d.USER_ID AND e.DEPLOYMENT_ID = d.DEPLOYMENT_ID AND e.CATEGORY = 'user'
				AND NOT d.IS_ORIGINAL AND d.ORIGINAL_USER_ID = ANY($1::TEXT[]) AND ($2 = '' OR d.DEPLOYMENT_ID = $2)`,
	}
	// queryDuplicateCoverage counts, for the duplicates of the given originals (or of every original when NULL),
	// the duplicates still present, the originals that no longer exist, and the group memberships of a duplicate
	// that its original does not have.
	queryDuplicateCoverage = dbmodel.DBQuery{
		ID: "MIG-USM-16-03",
		PostgresQuery: `SELECT
				(SELECT COUNT(*) FROM "USER_SHARE_MIGRATION" d
					INNER JOIN "ENTITY" e ON e.ID = d.USER_ID AND e.DEPLOYMENT_ID = d.DEPLOYMENT_ID
						AND e.CATEGORY = 'user'
					WHERE NOT d.IS_ORIGINAL AND ($1::TEXT[] IS NULL OR d.ORIGINAL_USER_ID = ANY($1::TEXT[]))
						AND ($2 = '' OR d.DEPLOYMENT_ID = $2)) AS remaining,
				(SELECT COUNT(*) FROM "USER_SHARE_MIGRATION" o
					WHERE o.IS_ORIGINAL AND ($1::TEXT[] IS NULL OR o.USER_ID = ANY($1::TEXT[]))
						AND ($2 = '' OR o.DEPLOYMENT_ID = $2)
						AND EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" d
							WHERE d.DEPLOYMENT_ID = o.DEPLOYMENT_ID AND d.ORIGINAL_USER_ID = o.USER_ID
								AND NOT d.IS_ORIGINAL)
						AND NOT EXISTS (SELECT 1 FROM "ENTITY" e WHERE e.ID = o.USER_ID
							AND e.DEPLOYMENT_ID = o.DEPLOYMENT_ID AND e.CATEGORY = 'user')) AS missing_originals,
				(SELECT COUNT(*) FROM "GROUP_MEMBER_REFERENCE" g
					INNER JOIN "USER_SHARE_MIGRATION" d ON d.USER_ID = g.MEMBER_ID
						AND d.DEPLOYMENT_ID = g.DEPLOYMENT_ID AND NOT d.IS_ORIGINAL
					WHERE g.MEMBER_TYPE = 'entity'
						AND ($1::TEXT[] IS NULL OR d.ORIGINAL_USER_ID = ANY($1::TEXT[]))
						AND ($2 = '' OR d.DEPLOYMENT_ID = $2)
						AND NOT EXISTS (SELECT 1 FROM "GROUP_MEMBER_REFERENCE" o
							WHERE o.GROUP_ID = g.GROUP_ID AND o.MEMBER_TYPE = 'entity'
								AND o.MEMBER_ID = d.ORIGINAL_USER_ID AND o.DEPLOYMENT_ID = g.DEPLOYMENT_ID)
				) AS uncovered_memberships`,
	}
)

// duplicateCoverage is what queryDuplicateCoverage counts.
type duplicateCoverage struct {
	remaining            int64
	missingOriginals     int64
	uncoveredMemberships int64
	missingLinks         int64
}

// newDeleteDuplicateUsersOperation deletes the duplicate users and their group memberships. Before a batch
// is deleted, every original in it must still exist, hold every link of its duplicates and belong to every
// group of its duplicates; otherwise the batch stops rather than delete the only record of a person's access.
func newDeleteDuplicateUsersOperation() engine.Operation {
	return newBatchOperation(deleteDuplicateUsersOperationID,
		"Delete the duplicate users and their group memberships",
		engine.StoreIDEntityDB, nil,
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyDuplicateUsersDeleted),
		verifyDuplicateUsersDeleted, deleteDuplicateUsersBatch)
}

func deleteDuplicateUsersBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, deleteDuplicateUsersBatchSize,
		func(originalIDs []string) error {
			coverage, err := readDuplicateCoverage(ctx, access, originalIDs, target.DeploymentID)
			if err != nil {
				return err
			}
			if finding := coverage.finding(); finding != "" {
				return fmt.Errorf("keep the duplicates of the users in this batch: %s", finding)
			}
			ids := textArray(originalIDs)
			for _, query := range []dbmodel.DBQuery{queryDeleteDuplicateMemberships, queryDeleteDuplicateEntities} {
				if _, err := access.Execute(ctx, engine.StoreIDEntityDB, query, ids, target.DeploymentID); err != nil {
					return fmt.Errorf("delete duplicate users: %w", err)
				}
			}
			return nil
		})
}

// readDuplicateCoverage counts the duplicates of the given originals, or of every original when ids is nil.
func readDuplicateCoverage(ctx context.Context, access engine.ReadAccess, originalIDs []string, deploymentID string) (
	duplicateCoverage, error) {
	var ids interface{}
	if originalIDs != nil {
		ids = textArray(originalIDs)
	}
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryDuplicateCoverage, ids, deploymentID)
	if err != nil {
		return duplicateCoverage{}, fmt.Errorf("check the originals of duplicate users: %w", err)
	}
	if len(rows) != 1 {
		return duplicateCoverage{}, fmt.Errorf("check the originals of duplicate users: expected one row, found %d",
			len(rows))
	}
	remaining, remainingErr := rows[0].Int64("remaining")
	missing, missingErr := rows[0].Int64("missing_originals")
	uncovered, uncoveredErr := rows[0].Int64("uncovered_memberships")
	if err := errors.Join(remainingErr, missingErr, uncoveredErr); err != nil {
		return duplicateCoverage{}, err
	}
	_, linksInAttributes, linksInIndex, err := countMissingLinks(ctx, access, originalIDs, deploymentID)
	if err != nil {
		return duplicateCoverage{}, err
	}
	return duplicateCoverage{
		remaining: remaining, missingOriginals: missing, uncoveredMemberships: uncovered,
		missingLinks: linksInAttributes + linksInIndex,
	}, nil
}

// finding describes why the duplicates cannot be deleted yet, or returns an empty string.
func (c duplicateCoverage) finding() string {
	var findings []string
	if c.missingOriginals > 0 {
		findings = append(findings, fmt.Sprintf("%d originals no longer exist", c.missingOriginals))
	}
	if c.missingLinks > 0 {
		findings = append(findings, fmt.Sprintf("%d links of duplicates are missing on their original", c.missingLinks))
	}
	if c.uncoveredMemberships > 0 {
		findings = append(findings, fmt.Sprintf("%d group memberships of duplicates are missing on their original",
			c.uncoveredMemberships))
	}
	return strings.Join(findings, "; ")
}

// verifyDuplicateUsersDeleted is at target once no duplicate user remains. It is invalid while a remaining
// duplicate's original cannot stand in for it.
func verifyDuplicateUsersDeleted(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	coverage, err := readDuplicateCoverage(ctx, access, nil, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if coverage.remaining == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: "No duplicate user remains"}, nil
	}
	if finding := coverage.finding(); finding != "" {
		return invalidVerification("original_user_incomplete", fmt.Sprintf(
			"The originals cannot replace their duplicates: %s", finding)), nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d duplicate users remain", coverage.remaining)}, nil
}
