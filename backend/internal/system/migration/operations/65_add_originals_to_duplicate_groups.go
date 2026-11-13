// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	addOriginalsToDuplicateGroupsOperationID engine.OperationID = "entitydb/" +
		"65_add_originals_to_duplicate_groups"
	addOriginalsToDuplicateGroupsRollbackID engine.OperationID = "entitydb/" +
		"65_add_originals_to_duplicate_groups_rollback"

	// addOriginalsToDuplicateGroupsBatchSize is the number of originals whose duplicates' memberships are
	// copied per batch.
	addOriginalsToDuplicateGroupsBatchSize = 100
)

var (
	queryUserGroupMemberships = dbmodel.DBQuery{
		ID: "MIG-USM-10-01",
		PostgresQuery: `SELECT m.MEMBER_ID, m.GROUP_ID, g.OU_ID
			FROM "GROUP_MEMBER_REFERENCE" m
			INNER JOIN "GROUP" g ON g.ID = m.GROUP_ID AND g.DEPLOYMENT_ID = m.DEPLOYMENT_ID
			WHERE m.MEMBER_TYPE = 'entity' AND m.MEMBER_ID = ANY($1::TEXT[]) AND ($2 = '' OR m.DEPLOYMENT_ID = $2)
			ORDER BY m.MEMBER_ID, m.GROUP_ID`,
	}
	// queryAddOriginalMemberships gives each original, in one statement, every group its duplicates are in, and
	// records on the original's row the groups that were actually added. Memberships the original already had
	// conflict and are neither added nor recorded, so a rollback removes only what this operation added.
	queryAddOriginalMemberships = dbmodel.DBQuery{
		ID: "MIG-USM-10-02",
		PostgresQuery: `WITH added AS (
				INSERT INTO "GROUP_MEMBER_REFERENCE"
					(GROUP_ID, MEMBER_TYPE, MEMBER_ID, CREATED_AT, UPDATED_AT, DEPLOYMENT_ID)
				SELECT DISTINCT g.GROUP_ID, 'entity', m.ORIGINAL_USER_ID, NOW(), NOW(), g.DEPLOYMENT_ID
				FROM "GROUP_MEMBER_REFERENCE" g
				INNER JOIN "USER_SHARE_MIGRATION" m
					ON m.USER_ID = g.MEMBER_ID AND m.DEPLOYMENT_ID = g.DEPLOYMENT_ID
				WHERE g.MEMBER_TYPE = 'entity' AND NOT m.IS_ORIGINAL AND m.ORIGINAL_USER_ID = ANY($1::TEXT[])
					AND ($2 = '' OR g.DEPLOYMENT_ID = $2)
				ON CONFLICT DO NOTHING
				RETURNING MEMBER_ID, GROUP_ID, DEPLOYMENT_ID
			)
			UPDATE "USER_SHARE_MIGRATION" t
			SET ADDED_GROUP_IDS = t.ADDED_GROUP_IDS || a.GROUPS
			FROM (SELECT MEMBER_ID, DEPLOYMENT_ID, JSONB_AGG(GROUP_ID ORDER BY GROUP_ID) AS GROUPS
				FROM added GROUP BY MEMBER_ID, DEPLOYMENT_ID) a
			WHERE t.USER_ID = a.MEMBER_ID AND t.DEPLOYMENT_ID = a.DEPLOYMENT_ID`,
	}
	queryDeleteOriginalMembership = dbmodel.DBQuery{
		ID: "MIG-USM-10-04",
		PostgresQuery: `DELETE FROM "GROUP_MEMBER_REFERENCE"
			WHERE GROUP_ID = $1 AND MEMBER_TYPE = 'entity' AND MEMBER_ID = $2 AND DEPLOYMENT_ID = $3`,
	}
	queryClearAddedGroups = dbmodel.DBQuery{
		ID: "MIG-USM-10-05",
		PostgresQuery: `UPDATE "USER_SHARE_MIGRATION" SET ADDED_GROUP_IDS = '[]'::JSONB
			WHERE USER_ID = $1 AND DEPLOYMENT_ID = $2`,
	}
	queryDirectUserRoleAssignments = dbmodel.DBQuery{
		ID: "MIG-USM-10-06",
		PostgresQuery: `SELECT DISTINCT ASSIGNEE_ID FROM "ROLE_ASSIGNMENT"
			WHERE ASSIGNEE_TYPE = 'entity' AND ASSIGNEE_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)
			ORDER BY ASSIGNEE_ID`,
	}
	queryGroupRoleAssignmentUnits = dbmodel.DBQuery{
		ID: "MIG-USM-10-07",
		PostgresQuery: `SELECT DISTINCT ASSIGNEE_ID, ASSIGNMENT_OU_ID FROM "ROLE_ASSIGNMENT"
			WHERE ASSIGNEE_TYPE = 'group' AND ASSIGNEE_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)
			ORDER BY ASSIGNEE_ID, ASSIGNMENT_OU_ID`,
	}
)

// groupMembership is one entity's membership in a group of an organization unit.
type groupMembership struct {
	memberID string
	groupID  string
	groupOU  string
}

// NewAddOriginalsToDuplicateGroupsOperation makes each original a member of every group a duplicate belongs
// to. Groups are never shared: a group belongs to one organization unit and its role assignments are made in
// that unit. The original reaches the duplicate's unit through its sharing policy, so the membership is valid
// there, and it gains that unit's roles only through that unit's groups.
func NewAddOriginalsToDuplicateGroupsOperation() engine.Operation {
	return newBatchOperation(addOriginalsToDuplicateGroupsOperationID,
		"Add each original user to the groups of its duplicates",
		engine.StoreIDEntityDB, []engine.StoreID{engine.StoreIDConfigDB},
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyOriginalsInDuplicateGroups),
		verifyOriginalsInDuplicateGroups, addOriginalsToDuplicateGroupsBatch)
}

// NewRollbackAddOriginalsToDuplicateGroupsOperation returns the rollback of the matching forward operation.
func NewRollbackAddOriginalsToDuplicateGroupsOperation() engine.RollbackOperation {
	return &rollbackBatchOperation{
		batchOperation: newBatchOperation(addOriginalsToDuplicateGroupsRollbackID,
			"Remove the group memberships added to original users",
			engine.StoreIDEntityDB, []engine.StoreID{engine.StoreIDConfigDB}, nil,
			verifyAddedMembershipsRemoved, removeAddedMembershipsBatch),
		forwardID: addOriginalsToDuplicateGroupsOperationID,
	}
}

func addOriginalsToDuplicateGroupsBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, addOriginalsToDuplicateGroupsBatchSize,
		func(originalIDs []string) error {
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryAddOriginalMemberships,
				textArray(originalIDs), target.DeploymentID); err != nil {
				return fmt.Errorf("add originals to the groups of their duplicates: %w", err)
			}
			return nil
		})
}

func removeAddedMembershipsBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatch(ctx, access, target, checkpoint, addOriginalsToDuplicateGroupsBatchSize,
		func(group migrationGroup) error {
			original := group.original
			for _, groupID := range original.addedGroupIDs {
				if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryDeleteOriginalMembership,
					groupID, original.userID, original.deploymentID); err != nil {
					return fmt.Errorf("remove user %q from group %q: %w", original.userID, groupID, err)
				}
			}
			if len(original.addedGroupIDs) == 0 {
				return nil
			}
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryClearAddedGroups,
				original.userID, original.deploymentID); err != nil {
				return fmt.Errorf("clear groups recorded for user %q: %w", original.userID, err)
			}
			return nil
		})
}

// verifyOriginalsInDuplicateGroups is at target once every original belongs to every group of its duplicates.
// It is invalid when the data breaks the rules the migration relies on: a duplicate in a group of another
// organization unit, a direct role assignment to a user being merged, or a group role assignment made outside
// the group's own unit.
func verifyOriginalsInDuplicateGroups(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	groups, err := readMigrationGroups(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	var userIDs []string
	ouByUser := map[string]string{}
	for _, group := range groups {
		userIDs = append(userIDs, group.original.userID)
		ouByUser[group.original.userID] = group.original.ouID
		for _, duplicate := range group.duplicates {
			userIDs = append(userIDs, duplicate.userID)
			ouByUser[duplicate.userID] = duplicate.ouID
		}
	}
	if len(groups) == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No original user needs group memberships",
		}, nil
	}
	memberships, err := readGroupMemberships(ctx, access, userIDs, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	findings, err := groupRuleFindings(ctx, access, userIDs, memberships, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	groupsOfUser := map[string]map[string]bool{}
	for _, membership := range memberships {
		if groupsOfUser[membership.memberID] == nil {
			groupsOfUser[membership.memberID] = map[string]bool{}
		}
		groupsOfUser[membership.memberID][membership.groupID] = true
	}
	missing := 0
	for _, group := range groups {
		for _, duplicate := range group.duplicates {
			for _, membership := range memberships {
				if membership.memberID != duplicate.userID {
					continue
				}
				if membership.groupOU != ouByUser[duplicate.userID] {
					findings = append(findings, fmt.Sprintf("Duplicate %q is in group %q of organization unit %q",
						duplicate.userID, membership.groupID, membership.groupOU))
				}
				if !groupsOfUser[group.original.userID][membership.groupID] {
					missing++
				}
			}
		}
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		return invalidVerification("duplicate_user_groups_not_migratable", strings.Join(findings, "; ")), nil
	}
	if missing == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "Every original user is in the groups of its duplicates",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d memberships of duplicates are still missing on their original users", missing)}, nil
}

// groupRuleFindings reports direct role assignments to the users and group role assignments made outside the
// group's organization unit, for every group the users belong to.
func groupRuleFindings(
	ctx context.Context,
	access engine.ReadAccess,
	userIDs []string,
	memberships []groupMembership,
	deploymentID string,
) ([]string, error) {
	var findings []string
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryDirectUserRoleAssignments,
		textArray(userIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read direct user role assignments: %w", err)
	}
	for _, row := range rows {
		userID, err := row.String("assignee_id")
		if err != nil {
			return nil, err
		}
		findings = append(findings, fmt.Sprintf("User %q has a direct role assignment", userID))
	}

	groupOUs := map[string]string{}
	var groupIDs []string
	for _, membership := range memberships {
		if _, seen := groupOUs[membership.groupID]; !seen {
			groupIDs = append(groupIDs, membership.groupID)
		}
		groupOUs[membership.groupID] = membership.groupOU
	}
	if len(groupIDs) == 0 {
		return findings, nil
	}
	rows, err = access.Query(ctx, engine.StoreIDConfigDB, queryGroupRoleAssignmentUnits,
		textArray(groupIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read group role assignments: %w", err)
	}
	for _, row := range rows {
		groupID, groupErr := row.String("assignee_id")
		assignmentOU, ouErr := row.String("assignment_ou_id")
		if err := errors.Join(groupErr, ouErr); err != nil {
			return nil, err
		}
		if assignmentOU != groupOUs[groupID] {
			findings = append(findings, fmt.Sprintf(
				"Group %q has a role assignment in organization unit %q instead of its own", groupID, assignmentOU))
		}
	}
	return findings, nil
}

func verifyAddedMembershipsRemoved(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	groups, err := readMigrationGroups(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	remaining := 0
	for _, group := range groups {
		remaining += len(group.original.addedGroupIDs)
	}
	if remaining == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No group membership added to an original user remains",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d group memberships added to original users remain", remaining)}, nil
}

func readGroupMemberships(
	ctx context.Context,
	access engine.ReadAccess,
	userIDs []string,
	deploymentID string,
) ([]groupMembership, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryUserGroupMemberships,
		textArray(userIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read group memberships: %w", err)
	}
	memberships := make([]groupMembership, 0, len(rows))
	for _, row := range rows {
		memberID, memberErr := row.String("member_id")
		groupID, groupErr := row.String("group_id")
		groupOU, ouErr := row.String("ou_id")
		if err := errors.Join(memberErr, groupErr, ouErr); err != nil {
			return nil, fmt.Errorf("read group membership: %w", err)
		}
		memberships = append(memberships, groupMembership{memberID: memberID, groupID: groupID, groupOU: groupOU})
	}
	return memberships, nil
}
