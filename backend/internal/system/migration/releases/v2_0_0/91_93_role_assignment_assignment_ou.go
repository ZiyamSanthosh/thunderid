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
	addRoleAssignmentAssigneeIndexOperationID engine.OperationID = "2.0.0/configdb/" +
		"91_add_role_assignment_assignee_index"
	backfillRoleAssignmentOUOperationID engine.OperationID = "2.0.0/configdb/92_backfill_role_assignment_ou_id"
	finalizeRoleAssignmentOperationID   engine.OperationID = "2.0.0/configdb/93_finalize_role_assignment"

	roleAssignmentAssigneeIndex     = "idx_role_assignment_assignee"
	roleAssignmentAssigneeIndexNext = "idx_role_assignment_assignee_ou"
	roleAssignmentLegacyAssignee    = "(deployment_id, assignee_type, assignee_id)"
	roleAssignmentTargetAssignee    = "(deployment_id, assignee_type, assignee_id, assignment_ou_id)"
)

var (
	queryRoleAssignmentUnassigned = dbmodel.DBQuery{
		ID: "MIG-RAO-01",
		PostgresQuery: `SELECT COUNT(*) AS missing
			FROM "ROLE_ASSIGNMENT" ra
			INNER JOIN "ROLE" r ON r.ID = ra.ROLE_ID AND r.DEPLOYMENT_ID = ra.DEPLOYMENT_ID
			WHERE ra.ASSIGNMENT_OU_ID = '' AND ($1 = '' OR ra.DEPLOYMENT_ID = $1)`,
	}
)

// newAddRoleAssignmentAssigneeIndexOperation builds the assignee lookup index that also names the assignment
// organization unit, beside the previous one. The contract phase drops the previous index and renames this one.
func newAddRoleAssignmentAssigneeIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addRoleAssignmentAssigneeIndexOperationID,
		description: "Add the role assignment lookup index that names the assignment organization unit",
		storeID:     engine.StoreIDConfigDB,
		table:       "ROLE_ASSIGNMENT",
		name:        roleAssignmentAssigneeIndexNext,
		definition:  roleAssignmentTargetAssignee,
		createSQL: `CREATE INDEX CONCURRENTLY "idx_role_assignment_assignee_ou"
			ON "ROLE_ASSIGNMENT" (DEPLOYMENT_ID, ASSIGNEE_TYPE, ASSIGNEE_ID, ASSIGNMENT_OU_ID)`,
	}, "MIG-OIX-RA-ASSIGNEE")
}

// newBackfillRoleAssignmentOperation records, on each assignment made before organization units were tracked, the
// organization unit that owns its role. It runs in migrate, after the previous release is retired, so no
// assignment can still be written without one. An assignment whose role no longer exists is left as it is.
func newBackfillRoleAssignmentOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		if verification, ok, err := verifyRoleAssignmentColumns(ctx, access); err != nil || !ok {
			return verification, err
		}
		rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryRoleAssignmentUnassigned, target.DeploymentID)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("count unassigned role assignments: %w", err)
		}
		if len(rows) != 1 {
			return engine.VerificationResult{}, errors.New("count unassigned role assignments: no result")
		}
		count, err := rows[0].Int64("missing")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if count == 0 {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "Every role assignment records its organization unit"}, nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("%d role assignments have no organization unit", count)}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               backfillRoleAssignmentOUOperationID,
			Description:      "Record the role's organization unit on assignments made before it was tracked",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			verification, ok, err := verifyRoleAssignmentColumns(ctx, access)
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			if !ok {
				return engine.ReadinessResult{State: engine.ReadinessDeferred, Summary: verification.Summary,
					DeferredUntil: []engine.OperationID{addRoleAssignmentSharingPolicyIDOperationID}}, nil
			}
			return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
		},
		TargetResolver: func(_ context.Context, _ engine.ReadAccess, selection engine.MigrationSelection) (
			[]engine.ExecutionTarget, error) {
			return deploymentExecutionTargets(selection), nil
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{{
				ID: "backfill-assignment-ou-id",
				Query: `UPDATE "ROLE_ASSIGNMENT" ra SET ASSIGNMENT_OU_ID = r.OU_ID
					FROM "ROLE" r
					WHERE r.ID = ra.ROLE_ID AND r.DEPLOYMENT_ID = ra.DEPLOYMENT_ID
					AND ra.ASSIGNMENT_OU_ID = '' AND ($1 = '' OR ra.DEPLOYMENT_ID = $1)`,
				Args: func(target engine.ExecutionTarget) ([]interface{}, error) {
					return []interface{}{target.DeploymentID}, nil
				},
			}}},
		},
		PostVerifier: verify,
	})
}

// verifyRoleAssignmentColumns reports false, with a summary, when the columns the backfill needs are absent.
func verifyRoleAssignmentColumns(ctx context.Context, access engine.ReadAccess) (
	engine.VerificationResult, bool, error) {
	verification, err := verifyAddedColumns(ctx, access, roleAssignmentAddedColumns)
	if err != nil {
		return engine.VerificationResult{}, false, err
	}
	if verification.State != engine.VerificationTarget {
		// A finalized table has dropped the default and is also a valid state for the backfill.
		finalized, err := roleAssignmentFinalColumn(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, false, err
		}
		if finalized {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "The role assignment columns are finalized"}, true, nil
		}
		return invalidVerification("unexpected_role_assignment_columns", verification.Summary), false, nil
	}
	return verification, true, nil
}

func roleAssignmentFinalColumn(ctx context.Context, access engine.ReadAccess) (bool, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryTableColumns, "ROLE_ASSIGNMENT")
	if err != nil {
		return false, fmt.Errorf("inspect ROLE_ASSIGNMENT columns: %w", err)
	}
	for _, row := range rows {
		column, err := readColumn(row)
		if err != nil {
			return false, err
		}
		if column.name == "assignment_ou_id" {
			return column == columnSpec{"assignment_ou_id", "NO", "character varying", "36", "none"}, nil
		}
	}
	return false, nil
}

// newFinalizeRoleAssignmentOperation removes the default from the assignment organization unit, so every new
// assignment has to name one, and puts the assignee lookup index that names it in place of the previous index.
func newFinalizeRoleAssignmentOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		finalColumn, err := roleAssignmentFinalColumn(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		verification, ok, err := verifyRoleAssignmentColumns(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if !ok {
			return verification, nil
		}
		rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryOnlineIndex, roleAssignmentAssigneeIndex)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("inspect index %s: %w", roleAssignmentAssigneeIndex, err)
		}
		if len(rows) != 1 {
			return invalidVerification("unexpected_index_definition", fmt.Sprintf(
				"Index %s is missing", roleAssignmentAssigneeIndex)), nil
		}
		definition, err := rows[0].String("definition")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		normalized := normalizeIndexDefinition(definition)
		legacyIndex := strings.HasSuffix(normalized,
			normalizeIndexDefinition("USING btree "+roleAssignmentLegacyAssignee))
		targetIndex := strings.HasSuffix(normalized,
			normalizeIndexDefinition("USING btree "+roleAssignmentTargetAssignee))
		switch {
		case finalColumn && targetIndex:
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "The role assignment columns and lookup index are final"}, nil
		case !finalColumn && legacyIndex:
			return engine.VerificationResult{State: engine.VerificationSource,
				Summary: "The role assignment columns and lookup index are ready to be finalized"}, nil
		default:
			return invalidVerification("unexpected_role_assignment_schema", fmt.Sprintf(
				"The role assignment default and the %s index disagree: %s", roleAssignmentAssigneeIndex,
				definition)), nil
		}
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               finalizeRoleAssignmentOperationID,
			Description:      "Finalize the role assignment organization unit column and lookup index",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			return readinessFromVerification(ctx, access,
				func(ctx context.Context, access engine.ReadAccess, _ string) (engine.VerificationResult, error) {
					return verify(ctx, access, engine.ExecutionTarget{})
				})
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "drop-assignment-ou-default", Query: `ALTER TABLE "ROLE_ASSIGNMENT"
    ALTER COLUMN ASSIGNMENT_OU_ID DROP DEFAULT`},
				{ID: "drop-legacy-assignee-index", Query: `DROP INDEX "idx_role_assignment_assignee"`},
				{ID: "rename-assignee-index", Query: `ALTER INDEX "idx_role_assignment_assignee_ou"
    RENAME TO "idx_role_assignment_assignee"`},
			}},
		},
		PostVerifier: verify,
	})
}
