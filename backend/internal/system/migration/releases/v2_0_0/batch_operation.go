// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const userShareMigrationTableName = "USER_SHARE_MIGRATION"

var queryMigrationUsers = dbmodel.DBQuery{
	ID: "MIG-USM-BAT-01",
	PostgresQuery: `SELECT DEPLOYMENT_ID, USER_ID, IS_ORIGINAL, ORIGINAL_USER_ID, EMAIL, OU_ID,
			SHARING_POLICY_ID, OWNER_OU_ID, ADDED_GROUP_IDS
		FROM "USER_SHARE_MIGRATION"
		WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
		ORDER BY DEPLOYMENT_ID, ORIGINAL_USER_ID, IS_ORIGINAL DESC, USER_ID`,
}

// batchCheckpoint is the durable position of a resumable operation: the last deployment and key it
// committed, and for operations that do not page by key, the number of rows processed so far.
type batchCheckpoint struct {
	DeploymentID string `json:"deploymentId,omitempty"`
	Key          string `json:"key,omitempty"`
	Processed    int64  `json:"processed,omitempty"`
}

// batchStep processes one bounded batch after the checkpoint and reports the new checkpoint and whether the
// operation has completed for the target.
type batchStep func(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error)

// targetVerifier classifies one deployment, or the complete store when deploymentID is empty.
type targetVerifier func(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error)

// readinessCheck performs an operation's plan-wide readiness check.
type readinessCheck func(ctx context.Context, access engine.ReadAccess) (engine.ReadinessResult, error)

// batchOperation is a resumable data-change operation that runs one deployment-scoped target in bounded,
// independently committed batches.
type batchOperation struct {
	definition engine.OperationDefinition
	readiness  readinessCheck
	verify     targetVerifier
	step       batchStep
}

// rollbackBatchOperation reverts the forward operation it names.
type rollbackBatchOperation struct {
	*batchOperation
	forwardID engine.OperationID
}

func newBatchOperation(
	id engine.OperationID,
	description string,
	storeID engine.StoreID,
	readStoreIDs []engine.StoreID,
	readiness readinessCheck,
	verify targetVerifier,
	step batchStep,
) *batchOperation {
	return &batchOperation{
		definition: engine.OperationDefinition{
			ID: id, Description: description, StoreID: storeID, ReadStoreIDs: readStoreIDs,
			Kind: engine.OperationKindDataChange, InterruptionMode: engine.InterruptionModeResumable,
		},
		readiness: readiness, verify: verify, step: step,
	}
}

func (o *batchOperation) Definition() engine.OperationDefinition {
	definition := o.definition
	definition.ReadStoreIDs = append([]engine.StoreID(nil), o.definition.ReadStoreIDs...)
	return definition
}

func (*batchOperation) ResolveTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	if len(selection.DeploymentIDs) == 0 {
		return []engine.ExecutionTarget{{}}, nil
	}
	targets := make([]engine.ExecutionTarget, 0, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		targets = append(targets, engine.ExecutionTarget{DeploymentID: deploymentID})
	}
	return targets, nil
}

func (o *batchOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	if o.readiness != nil {
		return o.readiness(ctx, access)
	}
	return readinessFromVerification(ctx, access, o.verify)
}

func (o *batchOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target.DeploymentID)
}

func (o *batchOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target.DeploymentID)
}

func (o *batchOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := batchCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode %s checkpoint: %w", o.definition.ID, err)
		}
	}
	next, completed, err := o.step(ctx, execution.Access(), execution.Target(), checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("encode %s checkpoint: %w", o.definition.ID, err)
	}
	return engine.ResumableBatchResult{CheckpointData: encoded, Completed: completed}, nil
}

func (o *rollbackBatchOperation) ForwardOperationID() engine.OperationID {
	return o.forwardID
}

// readinessFromVerification blocks on an invalid store and is otherwise ready.
func readinessFromVerification(
	ctx context.Context,
	access engine.ReadAccess,
	verify targetVerifier,
) (engine.ReadinessResult, error) {
	verification, err := verify(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary, Findings: verification.Findings,
		}, nil
	}
	return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
}

// readinessAfterMigrationTable defers an operation that reads the working table until the operation that
// fills it, and otherwise applies the verification-based readiness.
func readinessAfterMigrationTable(
	until engine.OperationID,
	verify targetVerifier,
) readinessCheck {
	return func(ctx context.Context, access engine.ReadAccess) (engine.ReadinessResult, error) {
		exists, err := relationExists(ctx, access, engine.StoreIDEntityDB, userShareMigrationTableName)
		if err != nil {
			return engine.ReadinessResult{}, err
		}
		if !exists {
			return engine.ReadinessResult{
				State:         engine.ReadinessDeferred,
				Summary:       "The operation waits for the user share migration table",
				DeferredUntil: []engine.OperationID{until},
			}, nil
		}
		return readinessFromVerification(ctx, access, verify)
	}
}

func relationExists(ctx context.Context, access engine.ReadAccess, storeID engine.StoreID, name string) (
	bool, error) {
	rows, err := access.Query(ctx, storeID, queryTableRelation, name)
	if err != nil {
		return false, fmt.Errorf("inspect relation %s: %w", name, err)
	}
	return len(rows) > 0, nil
}

// migrationUser is one row of the working table.
type migrationUser struct {
	deploymentID    string
	userID          string
	isOriginal      bool
	originalUserID  string
	email           string
	ouID            string
	sharingPolicyID string
	ownerOUID       string
	addedGroupIDs   []string
}

// migrationGroup is an original and its duplicates.
type migrationGroup struct {
	original   migrationUser
	duplicates []migrationUser
}

func readMigrationUsers(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	[]migrationUser, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryMigrationUsers, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("read user share migration table: %w", err)
	}
	users := make([]migrationUser, 0, len(rows))
	for _, row := range rows {
		user, err := readMigrationUser(row)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func readMigrationUser(row engine.QueryRow) (migrationUser, error) {
	deploymentID, deploymentErr := row.String("deployment_id")
	userID, userErr := row.String("user_id")
	isOriginal, originalErr := row.Bool("is_original")
	originalUserID, originalUserErr := row.String("original_user_id")
	email, emailErr := row.String("email")
	ouID, ouErr := row.String("ou_id")
	sharingPolicyID, policyErr := optionalString(row, "sharing_policy_id")
	ownerOUID, ownerErr := optionalString(row, "owner_ou_id")
	added, addedErr := optionalString(row, "added_group_ids")
	if err := errors.Join(deploymentErr, userErr, originalErr, originalUserErr, emailErr, ouErr,
		policyErr, ownerErr, addedErr); err != nil {
		return migrationUser{}, fmt.Errorf("read user share migration row: %w", err)
	}
	var addedGroupIDs []string
	if added != "" {
		if err := json.Unmarshal([]byte(added), &addedGroupIDs); err != nil {
			return migrationUser{}, fmt.Errorf("user %q has invalid added group IDs: %w", userID, err)
		}
	}
	return migrationUser{
		deploymentID: deploymentID, userID: userID, isOriginal: isOriginal, originalUserID: originalUserID,
		email: email, ouID: ouID, sharingPolicyID: sharingPolicyID,
		ownerOUID: ownerOUID, addedGroupIDs: addedGroupIDs,
	}, nil
}

// groupMigrationUsers pairs each original with its duplicates. The rows must be ordered by deployment and
// original, with the original first.
func groupMigrationUsers(users []migrationUser) ([]migrationGroup, error) {
	var groups []migrationGroup
	for _, user := range users {
		if user.isOriginal {
			if user.originalUserID != user.userID {
				return nil, fmt.Errorf("original %q names another original %q", user.userID, user.originalUserID)
			}
			groups = append(groups, migrationGroup{original: user})
			continue
		}
		if len(groups) == 0 {
			return nil, fmt.Errorf("duplicate %q has no original", user.userID)
		}
		current := &groups[len(groups)-1]
		if current.original.userID != user.originalUserID || current.original.deploymentID != user.deploymentID {
			return nil, fmt.Errorf("duplicate %q has no original", user.userID)
		}
		current.duplicates = append(current.duplicates, user)
	}
	return groups, nil
}

func optionalString(row engine.QueryRow, column string) (string, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return "", nil
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("column %q has unsupported type %T", column, value)
	}
}

// textArray renders identifiers as a PostgreSQL text array literal for a $n::TEXT[] parameter.
func textArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		escaped := strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
		quoted = append(quoted, `"`+escaped+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

func invalidVerification(code, summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: code, Message: summary}},
	}
}

// readMigrationGroups reads the working table and pairs each original with its duplicates, ordered by
// deployment and original user ID.
func readMigrationGroups(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	[]migrationGroup, error) {
	users, err := readMigrationUsers(ctx, access, deploymentID)
	if err != nil {
		return nil, err
	}
	groups, err := groupMigrationUsers(users)
	if err != nil {
		return nil, err
	}
	// The database collation may order IDs differently from Go; paging compares in Go.
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].original.deploymentID != groups[j].original.deploymentID {
			return groups[i].original.deploymentID < groups[j].original.deploymentID
		}
		return groups[i].original.userID < groups[j].original.userID
	})
	return groups, nil
}

// nextMigrationGroups returns up to size groups after the checkpoint, which names the last original
// processed. The working table holds only duplicate groups, so it is read whole and paged in memory.
func nextMigrationGroups(groups []migrationGroup, checkpoint batchCheckpoint, size int) []migrationGroup {
	start := sort.Search(len(groups), func(index int) bool {
		group := groups[index].original
		return group.deploymentID > checkpoint.DeploymentID ||
			(group.deploymentID == checkpoint.DeploymentID && group.userID > checkpoint.Key)
	})
	end := start + size
	if end > len(groups) {
		end = len(groups)
	}
	return groups[start:end]
}

// groupBatch runs one batch of groups after the checkpoint and advances it past the last group.
//
//nolint:unparam // each operation passes its own batch size constant, which may differ in future
func groupBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
	size int,
	apply func(group migrationGroup) error,
) (batchCheckpoint, bool, error) {
	groups, err := readMigrationGroups(ctx, access, target.DeploymentID)
	if err != nil {
		return checkpoint, false, err
	}
	batch := nextMigrationGroups(groups, checkpoint, size)
	for _, group := range batch {
		if err := apply(group); err != nil {
			return checkpoint, false, err
		}
		checkpoint = batchCheckpoint{DeploymentID: group.original.deploymentID, Key: group.original.userID}
	}
	return checkpoint, len(batch) < size, nil
}

// duplicateIDs returns the user IDs of the groups' duplicates.
func duplicateIDs(groups []migrationGroup) []string {
	var ids []string
	for _, group := range groups {
		for _, duplicate := range group.duplicates {
			ids = append(ids, duplicate.userID)
		}
	}
	return ids
}

// groupBatchWithIDs runs one batch of groups after the checkpoint with a single call that receives the IDs of
// the batch's originals, and advances the checkpoint past the last group. It is for operations that change
// the database with one set-based statement joined to the working table.
//
//nolint:unparam // each operation passes its own batch size constant, which may differ in future
func groupBatchWithIDs(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
	size int,
	apply func(originalIDs []string) error,
) (batchCheckpoint, bool, error) {
	groups, err := readMigrationGroups(ctx, access, target.DeploymentID)
	if err != nil {
		return checkpoint, false, err
	}
	batch := nextMigrationGroups(groups, checkpoint, size)
	if len(batch) == 0 {
		return checkpoint, true, nil
	}
	ids := make([]string, 0, len(batch))
	for _, group := range batch {
		ids = append(ids, group.original.userID)
	}
	if err := apply(ids); err != nil {
		return checkpoint, false, err
	}
	last := batch[len(batch)-1].original
	return batchCheckpoint{DeploymentID: last.deploymentID, Key: last.userID}, len(batch) < size, nil
}
