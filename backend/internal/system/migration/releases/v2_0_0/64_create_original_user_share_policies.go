// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	createOriginalUserSharePoliciesOperationID engine.OperationID = "2.0.0/configdb/" +
		"64_create_original_user_share_policies"
	createOriginalUserSharePoliciesRollbackID engine.OperationID = "2.0.0/configdb/" +
		"64_create_original_user_share_policies_rollback"

	// createOriginalUserSharePoliciesBatchSize is the number of originals shared per batch.
	createOriginalUserSharePoliciesBatchSize = 100

	userSharingPolicySections = `{"childUnits":{"mode":"selected"}}`
)

var (
	queryInsertUserSharingPolicy = dbmodel.DBQuery{
		ID: "MIG-USM-09-01",
		PostgresQuery: `INSERT INTO "RESOURCE_SHARING_POLICY"
			(ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, INITIATING_OU_ID, POLICY_STAGE, PARENT_POLICY_ID,
				SECTIONS, VERSION, DEPLOYMENT_ID)
			VALUES ($1, 'user', $2, $3, $3, 'share', NULL, $4::JSONB, 1, $5)
			ON CONFLICT (DEPLOYMENT_ID, RESOURCE_TYPE, RESOURCE_ID, INITIATING_OU_ID) DO NOTHING`,
	}
	queryInsertUserSharingPolicyUnit = dbmodel.DBQuery{
		ID: "MIG-USM-09-02",
		PostgresQuery: `INSERT INTO "RESOURCE_SHARING_POLICY_UNIT" (POLICY_ID, SECTION, OU_ID, REACH, DEPLOYMENT_ID)
			VALUES ($1, 'childUnits', $2, 'unit', $3)
			ON CONFLICT (DEPLOYMENT_ID, POLICY_ID, OU_ID) DO NOTHING`,
	}
	queryUserSharingPolicies = dbmodel.DBQuery{
		ID: "MIG-USM-09-03",
		PostgresQuery: `SELECT ID, RESOURCE_ID, OWNING_OU_ID, INITIATING_OU_ID, POLICY_STAGE, PARENT_POLICY_ID,
				SECTIONS::TEXT AS SECTIONS, DEPLOYMENT_ID
			FROM "RESOURCE_SHARING_POLICY"
			WHERE RESOURCE_TYPE = 'user' AND RESOURCE_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`,
	}
	queryUserSharingPolicyUnits = dbmodel.DBQuery{
		ID: "MIG-USM-09-04",
		PostgresQuery: `SELECT POLICY_ID, SECTION, OU_ID, REACH FROM "RESOURCE_SHARING_POLICY_UNIT"
			WHERE POLICY_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`,
	}
	queryUserSharingPolicyOverlayRules = dbmodel.DBQuery{
		ID: "MIG-USM-09-05",
		PostgresQuery: `SELECT POLICY_ID FROM "RESOURCE_SHARING_POLICY_OVERLAY_RULE"
			WHERE POLICY_ID = ANY($1::TEXT[]) AND ($2 = '' OR DEPLOYMENT_ID = $2)`,
	}
	queryDeleteUserSharingPolicy = dbmodel.DBQuery{
		ID:            "MIG-USM-09-06",
		PostgresQuery: `DELETE FROM "RESOURCE_SHARING_POLICY" WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}
)

// newCreateOriginalUserSharePoliciesOperation shares each original from its owner organization unit to its
// own unit and to the unit of every duplicate. The policy names each unit as a direct child with unit reach
// and carries no overlay rules, because users get their roles through the groups of each unit.
func newCreateOriginalUserSharePoliciesOperation() engine.Operation {
	return newBatchOperation(createOriginalUserSharePoliciesOperationID,
		"Share each original user to its own organization unit and to those of its duplicates",
		engine.StoreIDConfigDB, []engine.StoreID{engine.StoreIDEntityDB},
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyOriginalUserSharePolicies),
		verifyOriginalUserSharePolicies, createOriginalUserSharePoliciesBatch)
}

func newRollbackCreateOriginalUserSharePoliciesOperation() engine.RollbackOperation {
	return &rollbackBatchOperation{
		batchOperation: newBatchOperation(createOriginalUserSharePoliciesRollbackID,
			"Remove the sharing policies created for original users",
			engine.StoreIDConfigDB, []engine.StoreID{engine.StoreIDEntityDB}, nil,
			verifyOriginalUserSharePoliciesRemoved, removeOriginalUserSharePoliciesBatch),
		forwardID: createOriginalUserSharePoliciesOperationID,
	}
}

func createOriginalUserSharePoliciesBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatch(ctx, access, target, checkpoint, createOriginalUserSharePoliciesBatchSize,
		func(group migrationGroup) error {
			original := group.original
			if original.sharingPolicyID == "" || original.ownerOUID == "" {
				return fmt.Errorf("original %q has no recorded sharing policy or owner", original.userID)
			}
			if _, err := access.Execute(ctx, engine.StoreIDConfigDB, queryInsertUserSharingPolicy,
				original.sharingPolicyID, original.userID, original.ownerOUID, userSharingPolicySections,
				original.deploymentID); err != nil {
				return fmt.Errorf("create sharing policy for user %q: %w", original.userID, err)
			}
			for _, ouID := range group.sharedOUs() {
				if _, err := access.Execute(ctx, engine.StoreIDConfigDB, queryInsertUserSharingPolicyUnit,
					original.sharingPolicyID, ouID, original.deploymentID); err != nil {
					return fmt.Errorf("share user %q to organization unit %q: %w", original.userID, ouID, err)
				}
			}
			return nil
		})
}

func removeOriginalUserSharePoliciesBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatch(ctx, access, target, checkpoint, createOriginalUserSharePoliciesBatchSize,
		func(group migrationGroup) error {
			if group.original.sharingPolicyID == "" {
				return nil
			}
			if _, err := access.Execute(ctx, engine.StoreIDConfigDB, queryDeleteUserSharingPolicy,
				group.original.sharingPolicyID, group.original.deploymentID); err != nil {
				return fmt.Errorf("remove sharing policy of user %q: %w", group.original.userID, err)
			}
			return nil
		})
}

// sharedOUs returns the organization units the original is shared to: its own and each duplicate's.
func (g migrationGroup) sharedOUs() []string {
	ous := make([]string, 0, 1+len(g.duplicates))
	ous = append(ous, g.original.ouID)
	for _, duplicate := range g.duplicates {
		ous = append(ous, duplicate.ouID)
	}
	sort.Strings(ous)
	return ous
}

// userSharingPolicy is one user sharing policy and its named units.
type userSharingPolicy struct {
	id             string
	resourceID     string
	owningOUID     string
	initiatingOUID string
	stage          string
	parentPolicyID string
	sections       string
	units          []string
}

func verifyOriginalUserSharePolicies(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	groups, err := readMigrationGroups(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	policies, err := readUserSharingPolicies(ctx, access, groups, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	shared := 0
	for _, group := range groups {
		original := group.original
		var matching *userSharingPolicy
		for index := range policies[original.userID] {
			policy := &policies[original.userID][index]
			if policy.id != original.sharingPolicyID {
				return invalidVerification("unexpected_user_sharing_policy", fmt.Sprintf(
					"User %q already has sharing policy %q", original.userID, policy.id)), nil
			}
			matching = policy
		}
		if matching == nil {
			continue
		}
		if finding := matching.mismatch(original.ownerOUID, group.sharedOUs()); finding != "" {
			return invalidVerification("unexpected_user_sharing_policy", fmt.Sprintf(
				"Sharing policy %q of user %q %s", matching.id, original.userID, finding)), nil
		}
		if len(matching.units) == len(group.sharedOUs()) {
			shared++
		}
	}
	if shared == len(groups) {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: fmt.Sprintf(
			"All %d original users are shared to their organization units", shared)}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d of %d original users are shared to their organization units", shared, len(groups))}, nil
}

func verifyOriginalUserSharePoliciesRemoved(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	groups, err := readMigrationGroups(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	policies, err := readUserSharingPolicies(ctx, access, groups, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	remaining := 0
	for _, group := range groups {
		for _, policy := range policies[group.original.userID] {
			if policy.id == group.original.sharingPolicyID {
				remaining++
			}
		}
	}
	if remaining == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No sharing policy created for an original user remains",
		}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d sharing policies created for original users remain", remaining)}, nil
}

// mismatch describes how the policy differs from the expected share, or returns an empty string. Named units
// that are still missing are not a mismatch, because a batch adds them.
func (p userSharingPolicy) mismatch(ownerOUID string, sharedOUs []string) string {
	if p.owningOUID != ownerOUID || p.initiatingOUID != ownerOUID {
		return fmt.Sprintf("is not owned and initiated by organization unit %q", ownerOUID)
	}
	if p.stage != "share" || p.parentPolicyID != "" {
		return "is not a first-stage share"
	}
	var actual, expected interface{}
	if json.Unmarshal([]byte(p.sections), &actual) != nil ||
		json.Unmarshal([]byte(userSharingPolicySections), &expected) != nil || !reflect.DeepEqual(actual, expected) {
		return fmt.Sprintf("has sections %s", p.sections)
	}
	allowed := map[string]bool{}
	for _, ouID := range sharedOUs {
		allowed[ouID] = true
	}
	for _, unit := range p.units {
		if !allowed[unit] {
			return fmt.Sprintf("names unexpected unit %s", unit)
		}
	}
	return ""
}

// readUserSharingPolicies returns the user sharing policies of the originals, keyed by user ID, with their
// named units. A unit that is not a child unit with unit reach, or any overlay rule, is reported as a unit
// the share does not expect.
func readUserSharingPolicies(
	ctx context.Context,
	access engine.ReadAccess,
	groups []migrationGroup,
	deploymentID string,
) (map[string][]userSharingPolicy, error) {
	originalIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		originalIDs = append(originalIDs, group.original.userID)
	}
	policies := map[string][]userSharingPolicy{}
	if len(originalIDs) == 0 {
		return policies, nil
	}
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryUserSharingPolicies,
		textArray(originalIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read user sharing policies: %w", err)
	}
	byID := map[string]*userSharingPolicy{}
	var policyIDs []string
	for _, row := range rows {
		id, idErr := row.String("id")
		resourceID, resourceErr := row.String("resource_id")
		owning, owningErr := row.String("owning_ou_id")
		initiating, initiatingErr := row.String("initiating_ou_id")
		stage, stageErr := row.String("policy_stage")
		parent, parentErr := optionalString(row, "parent_policy_id")
		sections, sectionsErr := row.String("sections")
		if err := errors.Join(idErr, resourceErr, owningErr, initiatingErr, stageErr, parentErr,
			sectionsErr); err != nil {
			return nil, fmt.Errorf("read user sharing policy: %w", err)
		}
		policies[resourceID] = append(policies[resourceID], userSharingPolicy{
			id: id, resourceID: resourceID, owningOUID: owning, initiatingOUID: initiating, stage: stage,
			parentPolicyID: parent, sections: sections,
		})
		policyIDs = append(policyIDs, id)
	}
	if len(policyIDs) == 0 {
		return policies, nil
	}
	for resourceID := range policies {
		for index := range policies[resourceID] {
			byID[policies[resourceID][index].id] = &policies[resourceID][index]
		}
	}
	unitRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryUserSharingPolicyUnits,
		textArray(policyIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read user sharing policy units: %w", err)
	}
	for _, row := range unitRows {
		policyID, policyErr := row.String("policy_id")
		section, sectionErr := row.String("section")
		ouID, ouErr := row.String("ou_id")
		reach, reachErr := row.String("reach")
		if err := errors.Join(policyErr, sectionErr, ouErr, reachErr); err != nil {
			return nil, fmt.Errorf("read user sharing policy unit: %w", err)
		}
		if policy, exists := byID[policyID]; exists {
			unit := ouID
			if section != "childUnits" || reach != "unit" {
				unit = fmt.Sprintf("%s (%s, %s)", ouID, section, reach)
			}
			policy.units = append(policy.units, unit)
		}
	}
	ruleRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryUserSharingPolicyOverlayRules,
		textArray(policyIDs), deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read user sharing policy overlay rules: %w", err)
	}
	for _, row := range ruleRows {
		policyID, err := row.String("policy_id")
		if err != nil {
			return nil, fmt.Errorf("read user sharing policy overlay rule: %w", err)
		}
		if policy, exists := byID[policyID]; exists {
			policy.units = append(policy.units, "overlay rule")
		}
	}
	return policies, nil
}
