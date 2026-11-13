// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	widenRoleAssignmentPrimaryKeyOperationID engine.OperationID = "configdb/" +
		"61_widen_role_assignment_primary_key"

	roleAssignmentLegacyPrimaryKey = "ROLE_ID,DEPLOYMENT_ID,ASSIGNEE_TYPE,ASSIGNEE_ID"
	roleAssignmentTargetPrimaryKey = "ROLE_ID,DEPLOYMENT_ID,ASSIGNMENT_OU_ID,ASSIGNEE_TYPE,ASSIGNEE_ID," +
		"SHARING_POLICY_ID"
	// roleAssignmentPrimaryKeyIndex is built concurrently and then promoted to the primary key, which takes
	// over its name.
	roleAssignmentPrimaryKeyIndex     = "idx_role_assignment_pk_new"
	roleAssignmentSharingPolicyIndex  = "idx_role_assignment_sharing_policy"
	roleAssignmentSharingPolicyColumn = "DEPLOYMENT_ID,SHARING_POLICY_ID"
)

var (
	queryRoleAssignmentPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-USM-04-01",
		PostgresQuery: `SELECT c.conname AS constraint_name,
			string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ROLE_ASSIGNMENT' AND c.contype = 'p'
			GROUP BY c.oid, c.conname`,
	}
	queryRoleAssignmentIndex = dbmodel.DBQuery{
		ID: "MIG-USM-04-02",
		PostgresQuery: `SELECT i.indisunique AS is_unique, i.indisvalid AS is_valid,
				i.indisready AS is_ready, (i.indpred IS NOT NULL) AS has_predicate,
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_class idx
			INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
			INNER JOIN pg_index i ON i.indexrelid = idx.oid
			INNER JOIN pg_class t ON t.oid = i.indrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ROLE_ASSIGNMENT' AND idx.relname = $1
			GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready, i.indpred`,
	}
)

// roleAssignmentIndex describes one index the operation builds concurrently.
type roleAssignmentIndex struct {
	name    string
	unique  bool
	columns string
	create  dbmodel.DBQuery
	drop    dbmodel.DBQuery
}

var (
	roleAssignmentPrimaryKeyIndexSpec = roleAssignmentIndex{
		name: roleAssignmentPrimaryKeyIndex, unique: true, columns: roleAssignmentTargetPrimaryKey,
		create: dbmodel.DBQuery{
			ID: "MIG-USM-04-03",
			PostgresQuery: `CREATE UNIQUE INDEX CONCURRENTLY "idx_role_assignment_pk_new" ON "ROLE_ASSIGNMENT"
				(ROLE_ID, DEPLOYMENT_ID, ASSIGNMENT_OU_ID, ASSIGNEE_TYPE, ASSIGNEE_ID, SHARING_POLICY_ID)`,
		},
		drop: dbmodel.DBQuery{
			ID: "MIG-USM-04-04", PostgresQuery: `DROP INDEX CONCURRENTLY "idx_role_assignment_pk_new"`,
		},
	}
	roleAssignmentSharingPolicyIndexSpec = roleAssignmentIndex{
		name: roleAssignmentSharingPolicyIndex, unique: false, columns: roleAssignmentSharingPolicyColumn,
		create: dbmodel.DBQuery{
			ID: "MIG-USM-04-05",
			PostgresQuery: `CREATE INDEX CONCURRENTLY "idx_role_assignment_sharing_policy"
				ON "ROLE_ASSIGNMENT" (DEPLOYMENT_ID, SHARING_POLICY_ID)`,
		},
		drop: dbmodel.DBQuery{
			ID: "MIG-USM-04-06", PostgresQuery: `DROP INDEX CONCURRENTLY "idx_role_assignment_sharing_policy"`,
		},
	}
	queryRoleAssignmentLockTimeout = dbmodel.DBQuery{
		ID: "MIG-USM-04-07", PostgresQuery: "SET LOCAL lock_timeout = '2s'",
	}
)

// widenRoleAssignmentPrimaryKeyOperation adds ASSIGNMENT_OU_ID and SHARING_POLICY_ID to the ROLE_ASSIGNMENT
// primary key and adds the index used to remove the assignments a sharing policy mandates.
//
// Every existing row, and every row the old binary writes, has an empty ASSIGNMENT_OU_ID and an empty
// SHARING_POLICY_ID. Two rows distinct on the old key are therefore distinct on the new key, and two rows that
// collide on the old key still collide, so uniqueness behaves for the old binary exactly as before. The old
// binary inserts without a conflict target, so it does not depend on the shape of the key.
//
// Execute builds both indexes concurrently, outside a transaction. Finalize swaps the primary key onto the
// prebuilt unique index in one short transaction.
type widenRoleAssignmentPrimaryKeyOperation struct {
	definition engine.OperationDefinition
}

type roleAssignmentKeyInspection struct {
	state                engine.VerificationState
	summary              string
	primaryKeyName       string
	legacyPrimaryKey     bool
	primaryKeyIndex      roleAssignmentIndexState
	sharingPolicyIndex   roleAssignmentIndexState
	sharingPolicyColumns bool
}

type roleAssignmentIndexState struct {
	present bool
	valid   bool
}

// NewWidenRoleAssignmentPrimaryKeyOperation returns the widen role assignment primary key operation.
func NewWidenRoleAssignmentPrimaryKeyOperation() engine.Operation {
	return &widenRoleAssignmentPrimaryKeyOperation{
		definition: engine.OperationDefinition{
			ID:               widenRoleAssignmentPrimaryKeyOperationID,
			Description:      "Add the sharing policy ID to the role assignment primary key",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
	}
}

func (o *widenRoleAssignmentPrimaryKeyOperation) Definition() engine.OperationDefinition {
	return o.definition
}

func (*widenRoleAssignmentPrimaryKeyOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectRoleAssignmentKey(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if inspection.state == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: "unexpected_role_assignment_key", Message: inspection.summary,
				Remediation: "Restore the expected ROLE_ASSIGNMENT primary key or remove the conflicting index",
			}},
		}, nil
	}
	if !inspection.sharingPolicyColumns {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "The ROLE_ASSIGNMENT primary key waits for the SHARING_POLICY_ID column",
			DeferredUntil: []engine.OperationID{addRoleAssignmentSharingPolicyIDOperationID},
		}, nil
	}
	return engine.ReadinessResult{State: engine.ReadinessReady, Summary: inspection.summary}, nil
}

func (*widenRoleAssignmentPrimaryKeyOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (*widenRoleAssignmentPrimaryKeyOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyRoleAssignmentKey(ctx, access)
}

func (*widenRoleAssignmentPrimaryKeyOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyRoleAssignmentKey(ctx, access)
}

func (*widenRoleAssignmentPrimaryKeyOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	return buildRoleAssignmentIndexes(ctx, execution.Access())
}

func (*widenRoleAssignmentPrimaryKeyOperation) Finalize(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	return swapRoleAssignmentPrimaryKey(ctx, execution.Access())
}

// buildRoleAssignmentIndexes builds, outside a transaction, the unique index that becomes the new primary key
// and the sharing policy index.
func buildRoleAssignmentIndexes(ctx context.Context, access engine.ExecutionAccess) error {
	inspection, err := inspectRoleAssignmentKey(ctx, access)
	if err != nil {
		return err
	}
	if inspection.state == engine.VerificationInvalid {
		return fmt.Errorf("prepare ROLE_ASSIGNMENT primary key: %s", inspection.summary)
	}
	if inspection.legacyPrimaryKey {
		if err := ensureRoleAssignmentIndex(ctx, access, roleAssignmentPrimaryKeyIndexSpec,
			inspection.primaryKeyIndex); err != nil {
			return err
		}
	}
	return ensureRoleAssignmentIndex(ctx, access, roleAssignmentSharingPolicyIndexSpec,
		inspection.sharingPolicyIndex)
}

// swapRoleAssignmentPrimaryKey replaces the legacy primary key with the prebuilt unique index, keeping the
// constraint name.
func swapRoleAssignmentPrimaryKey(ctx context.Context, access engine.ExecutionAccess) error {
	inspection, err := inspectRoleAssignmentKey(ctx, access)
	if err != nil {
		return err
	}
	if !inspection.legacyPrimaryKey {
		return nil
	}
	if !inspection.primaryKeyIndex.valid {
		return fmt.Errorf("finalize ROLE_ASSIGNMENT primary key: index %s is not valid", roleAssignmentPrimaryKeyIndex)
	}
	if _, err := access.Execute(ctx, engine.StoreIDConfigDB, queryRoleAssignmentLockTimeout); err != nil {
		return fmt.Errorf("set ROLE_ASSIGNMENT lock timeout: %w", err)
	}
	swap := dbmodel.DBQuery{
		ID: "MIG-USM-04-08",
		PostgresQuery: fmt.Sprintf(`ALTER TABLE "ROLE_ASSIGNMENT" DROP CONSTRAINT %s,
			ADD CONSTRAINT %s PRIMARY KEY USING INDEX "idx_role_assignment_pk_new"`,
			quoteIdentifier(inspection.primaryKeyName), quoteIdentifier(inspection.primaryKeyName)),
	}
	if _, err := access.Execute(ctx, engine.StoreIDConfigDB, swap); err != nil {
		return fmt.Errorf("swap ROLE_ASSIGNMENT primary key: %w", err)
	}
	return nil
}

// ensureRoleAssignmentIndex leaves a valid index in place, rebuilds an index left invalid by an interrupted
// concurrent build, and creates a missing one.
func ensureRoleAssignmentIndex(
	ctx context.Context,
	access engine.ExecutionAccess,
	index roleAssignmentIndex,
	state roleAssignmentIndexState,
) error {
	if state.present && state.valid {
		return nil
	}
	if state.present {
		if _, err := access.Execute(ctx, engine.StoreIDConfigDB, index.drop); err != nil {
			return fmt.Errorf("drop invalid ROLE_ASSIGNMENT index %s: %w", index.name, err)
		}
	}
	if _, err := access.Execute(ctx, engine.StoreIDConfigDB, index.create); err != nil {
		return fmt.Errorf("create ROLE_ASSIGNMENT index %s concurrently: %w", index.name, err)
	}
	return nil
}

func verifyRoleAssignmentKey(ctx context.Context, access engine.ReadAccess) (engine.VerificationResult, error) {
	inspection, err := inspectRoleAssignmentKey(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{Code: "unexpected_role_assignment_key", Message: inspection.summary}}
	}
	return result, nil
}

func inspectRoleAssignmentKey(ctx context.Context, access engine.ReadAccess) (roleAssignmentKeyInspection, error) {
	columnVerification, err := verifyRoleAssignmentSharingPolicyID(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return roleAssignmentKeyInspection{}, err
	}
	if columnVerification.State == engine.VerificationInvalid {
		return invalidRoleAssignmentKey(columnVerification.Summary), nil
	}
	inspection := roleAssignmentKeyInspection{
		sharingPolicyColumns: columnVerification.State == engine.VerificationTarget,
	}

	keyRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryRoleAssignmentPrimaryKey)
	if err != nil {
		return roleAssignmentKeyInspection{}, fmt.Errorf("inspect ROLE_ASSIGNMENT primary key: %w", err)
	}
	if len(keyRows) != 1 {
		return invalidRoleAssignmentKey(fmt.Sprintf("Expected one ROLE_ASSIGNMENT primary key, found %d",
			len(keyRows))), nil
	}
	name, nameErr := keyRows[0].String("constraint_name")
	columns, columnsErr := keyRows[0].String("column_names")
	if err := errors.Join(nameErr, columnsErr); err != nil {
		return roleAssignmentKeyInspection{}, err
	}
	inspection.primaryKeyName = name
	switch columns {
	case roleAssignmentLegacyPrimaryKey:
		inspection.legacyPrimaryKey = true
	case roleAssignmentTargetPrimaryKey:
	default:
		return invalidRoleAssignmentKey(fmt.Sprintf("Unexpected ROLE_ASSIGNMENT primary key columns %q",
			columns)), nil
	}

	var invalidSummary string
	inspection.primaryKeyIndex, invalidSummary, err = inspectRoleAssignmentIndex(ctx, access,
		roleAssignmentPrimaryKeyIndexSpec)
	if err != nil {
		return roleAssignmentKeyInspection{}, err
	}
	if invalidSummary != "" {
		return invalidRoleAssignmentKey(invalidSummary), nil
	}
	if !inspection.legacyPrimaryKey && inspection.primaryKeyIndex.present {
		return invalidRoleAssignmentKey(fmt.Sprintf(
			"Index %s remains after the ROLE_ASSIGNMENT primary key was widened", roleAssignmentPrimaryKeyIndex)), nil
	}
	inspection.sharingPolicyIndex, invalidSummary, err = inspectRoleAssignmentIndex(ctx, access,
		roleAssignmentSharingPolicyIndexSpec)
	if err != nil {
		return roleAssignmentKeyInspection{}, err
	}
	if invalidSummary != "" {
		return invalidRoleAssignmentKey(invalidSummary), nil
	}

	if !inspection.legacyPrimaryKey && inspection.sharingPolicyIndex.present && inspection.sharingPolicyIndex.valid {
		inspection.state = engine.VerificationTarget
		inspection.summary = "The ROLE_ASSIGNMENT primary key includes SHARING_POLICY_ID and the sharing policy " +
			"index is valid"
		return inspection, nil
	}
	inspection.state = engine.VerificationSource
	if inspection.legacyPrimaryKey {
		inspection.summary = "The ROLE_ASSIGNMENT primary key is ready to include SHARING_POLICY_ID"
	} else {
		inspection.summary = "The ROLE_ASSIGNMENT sharing policy index is ready to be built"
	}
	return inspection, nil
}

func inspectRoleAssignmentIndex(
	ctx context.Context,
	access engine.ReadAccess,
	index roleAssignmentIndex,
) (roleAssignmentIndexState, string, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryRoleAssignmentIndex, index.name)
	if err != nil {
		return roleAssignmentIndexState{}, "", fmt.Errorf("inspect ROLE_ASSIGNMENT index %s: %w", index.name, err)
	}
	if len(rows) == 0 {
		return roleAssignmentIndexState{}, "", nil
	}
	if len(rows) != 1 {
		return roleAssignmentIndexState{}, fmt.Sprintf("Expected at most one index %s, found %d",
			index.name, len(rows)), nil
	}
	isUnique, uniqueErr := rows[0].Bool("is_unique")
	isValid, validErr := rows[0].Bool("is_valid")
	isReady, readyErr := rows[0].Bool("is_ready")
	hasPredicate, predicateErr := rows[0].Bool("has_predicate")
	columns, columnsErr := rows[0].String("column_names")
	if err := errors.Join(uniqueErr, validErr, readyErr, predicateErr, columnsErr); err != nil {
		return roleAssignmentIndexState{}, "", err
	}
	if isUnique != index.unique || hasPredicate || columns != index.columns {
		return roleAssignmentIndexState{}, fmt.Sprintf(
			"Unexpected %s definition: unique=%t, predicate=%t, columns=%q",
			index.name, isUnique, hasPredicate, columns), nil
	}
	return roleAssignmentIndexState{present: true, valid: isValid && isReady}, "", nil
}

func invalidRoleAssignmentKey(summary string) roleAssignmentKeyInspection {
	return roleAssignmentKeyInspection{state: engine.VerificationInvalid, summary: summary}
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
