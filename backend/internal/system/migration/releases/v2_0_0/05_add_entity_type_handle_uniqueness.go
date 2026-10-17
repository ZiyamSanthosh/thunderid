// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryEntityTypeUniqueConstraints = dbmodel.DBQuery{
	ID: "MIG-CFG_MGT-10",
	PostgresQuery: `SELECT c.CONNAME AS constraint_name,
			string_agg(UPPER(a.ATTNAME), ',' ORDER BY key_position.ORDINALITY) AS column_names
        FROM pg_constraint c
        INNER JOIN pg_class t ON t.oid = c.conrelid
        INNER JOIN pg_namespace n ON n.oid = t.relnamespace
        CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
        INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
        WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES' AND c.contype = 'u'
        GROUP BY c.oid, c.CONNAME`,
}

var queryEntityTypeHandleUniqueIndex = dbmodel.DBQuery{
	ID: "MIG-CFG_MGT-11",
	PostgresQuery: `SELECT i.indisunique AS is_unique, i.indisvalid AS is_valid,
		i.indisready AS is_ready,
		string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
		FROM pg_class idx
		INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
		INNER JOIN pg_index i ON i.indexrelid = idx.oid
		INNER JOIN pg_class t ON t.oid = i.indrelid
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
		INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
		WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES'
		AND idx.relname = 'idx_entity_types_handle_category_deployment_unique'
		GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready`,
}

const (
	legacyEntityTypeUniqueColumns = "NAME,CATEGORY,DEPLOYMENT_ID"
	targetEntityTypeUniqueColumns = "HANDLE,CATEGORY,DEPLOYMENT_ID"
	entityTypeHandleUniqueIndex   = "idx_entity_types_handle_category_deployment_unique"
)

type entityTypeHandleUniquenessOperation struct {
	definition       engine.OperationDefinition
	createIndexQuery dbmodel.DBQuery
	dropIndexQuery   dbmodel.DBQuery
	lockTimeoutQuery dbmodel.DBQuery
	finalizeQuery    dbmodel.DBQuery
}

type entityTypeHandleUniqueIndexInspection struct {
	present bool
	valid   bool
}

func newAddEntityTypeHandleUniquenessOperation() (engine.Operation, error) {
	return &entityTypeHandleUniquenessOperation{
		definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/05_add_entity_type_handle_uniqueness",
			Description:      "Enforce unique entity-type handles while retaining legacy name uniqueness",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		createIndexQuery: dbmodel.DBQuery{
			ID: "MIG-CFG_MGT-12",
			PostgresQuery: `CREATE UNIQUE INDEX CONCURRENTLY
				"idx_entity_types_handle_category_deployment_unique"
				ON "ENTITY_TYPES" ("handle", "category", "deployment_id")`,
		},
		dropIndexQuery: dbmodel.DBQuery{
			ID: "MIG-CFG_MGT-13",
			PostgresQuery: `DROP INDEX CONCURRENTLY
				"idx_entity_types_handle_category_deployment_unique"`,
		},
		lockTimeoutQuery: dbmodel.DBQuery{ID: "MIG-CFG_MGT-14", PostgresQuery: "SET LOCAL lock_timeout = '2s'"},
		finalizeQuery: dbmodel.DBQuery{
			ID: "MIG-CFG_MGT-15",
			PostgresQuery: `ALTER TABLE "ENTITY_TYPES"
				ADD CONSTRAINT "UQ_ENTITY_TYPES_HANDLE_CATEGORY_DEPLOYMENT"
				UNIQUE USING INDEX "idx_entity_types_handle_category_deployment_unique"`,
		},
	}, nil
}

func (o *entityTypeHandleUniquenessOperation) Definition() engine.OperationDefinition {
	return o.definition
}

func (o *entityTypeHandleUniquenessOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	planContext engine.PlanContext,
) (engine.ReadinessResult, error) {
	return checkAddEntityTypeHandleUniquenessReadiness(ctx, access, planContext)
}

func (o *entityTypeHandleUniquenessOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *entityTypeHandleUniquenessOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleUniqueness(ctx, access, target)
}

func (o *entityTypeHandleUniquenessOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	verification, err := verifyEntityTypeHandleUniqueness(
		ctx, execution.Access(), execution.Target())
	if err != nil {
		return err
	}
	if verification.State == engine.VerificationTarget {
		return nil
	}
	if verification.State != engine.VerificationSource {
		return fmt.Errorf("finalize entity-type handle uniqueness: %s", verification.Summary)
	}
	inspection, invalidSummary, err := inspectEntityTypeHandleUniqueIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if invalidSummary != "" {
		return fmt.Errorf("prepare entity-type handle uniqueness: %s", invalidSummary)
	}
	if inspection.present {
		if inspection.valid {
			return nil
		}
		if _, err := execution.Access().Execute(
			ctx, engine.StoreIDConfigDB, o.dropIndexQuery); err != nil {
			return fmt.Errorf("drop invalid entity-type handle unique index: %w", err)
		}
	}
	if _, err := execution.Access().Execute(
		ctx, engine.StoreIDConfigDB, o.createIndexQuery); err != nil {
		return fmt.Errorf("create entity-type handle unique index concurrently: %w", err)
	}
	return nil
}

func (o *entityTypeHandleUniquenessOperation) Finalize(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	inspection, invalidSummary, err := inspectEntityTypeHandleUniqueIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if invalidSummary != "" || !inspection.valid {
		return fmt.Errorf("finalize entity-type handle uniqueness: prepared unique index is unavailable")
	}
	if _, err := execution.Access().Execute(
		ctx, engine.StoreIDConfigDB, o.lockTimeoutQuery); err != nil {
		return fmt.Errorf("set entity-type handle uniqueness lock timeout: %w", err)
	}
	if _, err := execution.Access().Execute(
		ctx, engine.StoreIDConfigDB, o.finalizeQuery); err != nil {
		return fmt.Errorf("attach entity-type handle unique constraint: %w", err)
	}
	return nil
}

func (o *entityTypeHandleUniquenessOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleUniqueness(ctx, access, target)
}

func checkAddEntityTypeHandleUniquenessReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	constraintRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryEntityTypeUniqueConstraints)
	if err != nil {
		return engine.ReadinessResult{}, fmt.Errorf("inspect entity-type unique constraints: %w", err)
	}
	legacyUnique, targetUnique, err := inspectEntityTypeUniqueConstraints(constraintRows)
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	name, hasName := columns["NAME"]
	if !hasName || !matchesEntityTypeColumn(name, "NO") || !legacyUnique {
		return blockedEntityTypeHandleUniquenessReadiness(
			"The entity-type name column or legacy uniqueness constraint does not match the expected schema"), nil
	}
	handle, hasHandle := columns["HANDLE"]
	if !hasHandle {
		return deferredEntityTypeHandleUniquenessReadiness(), nil
	}
	if !matchesEntityTypeColumn(handle, "YES") {
		return blockedEntityTypeHandleUniquenessReadiness(
			"The entity-type handle column does not match the expected nullable definition"), nil
	}
	indexInspection, invalidSummary, err := inspectEntityTypeHandleUniqueIndex(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if invalidSummary != "" || targetUnique && indexInspection.present {
		if invalidSummary == "" {
			invalidSummary = "The completed entity-type handle uniqueness constraint has a leftover migration index"
		}
		return blockedEntityTypeHandleUniquenessReadiness(invalidSummary), nil
	}
	if targetUnique {
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The entity-type name and handle uniqueness constraints already coexist",
		}, nil
	}
	return deferredEntityTypeHandleUniquenessReadiness(), nil
}

func deferredEntityTypeHandleUniquenessReadiness() engine.ReadinessResult {
	return engine.ReadinessResult{
		State:   engine.ReadinessDeferred,
		Summary: "Entity-type handle uniqueness depends on the preceding handle backfill",
		DeferredUntil: []engine.OperationID{
			backfillEntityTypeHandleOperationID,
		},
	}
}

func blockedEntityTypeHandleUniquenessReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State:   engine.ReadinessBlocked,
		Summary: summary,
		Findings: []engine.Finding{{
			Code:        "unexpected_entity_type_uniqueness_schema",
			Message:     summary,
			Remediation: "Reconcile the ENTITY_TYPES columns and uniqueness constraints before retrying",
		}},
	}
}

func verifyEntityTypeHandleUniqueness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	constraintRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryEntityTypeUniqueConstraints)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect entity-type unique constraints: %w", err)
	}
	legacyUnique, targetUnique, err := inspectEntityTypeUniqueConstraints(constraintRows)
	if err != nil {
		return engine.VerificationResult{}, err
	}

	name, hasName := columns["NAME"]
	handle, hasHandle := columns["HANDLE"]
	indexInspection, invalidSummary, err := inspectEntityTypeHandleUniqueIndex(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if invalidSummary != "" {
		return invalidEntityTypeHandleUniquenessVerification(invalidSummary), nil
	}
	if hasName && matchesEntityTypeColumn(name, "NO") &&
		hasHandle && matchesEntityTypeColumn(handle, "YES") &&
		legacyUnique && !targetUnique {
		return engine.VerificationResult{
			State:   engine.VerificationSource,
			Summary: "Entity-type handles are ready for the target uniqueness constraint",
		}, nil
	}
	if hasName && matchesEntityTypeColumn(name, "NO") &&
		hasHandle && matchesEntityTypeColumn(handle, "YES") &&
		legacyUnique && targetUnique && !indexInspection.present {
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: "Entity-type name and handle uniqueness constraints coexist",
		}, nil
	}
	return invalidEntityTypeHandleUniquenessVerification(
		"The entity-type columns or unique constraints do not match the migrate source or target schema"), nil
}

func invalidEntityTypeHandleUniquenessVerification(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_entity_type_uniqueness_schema", Message: summary}},
	}
}

func inspectEntityTypeUniqueConstraints(rows []engine.QueryRow) (bool, bool, error) {
	legacy := false
	target := false
	for _, row := range rows {
		columns, err := row.String("column_names")
		if err != nil {
			return false, false, err
		}
		switch columns {
		case legacyEntityTypeUniqueColumns:
			legacy = true
		case targetEntityTypeUniqueColumns:
			target = true
		}
	}
	return legacy, target, nil
}

func inspectEntityTypeHandleUniqueIndex(
	ctx context.Context,
	access engine.ReadAccess,
) (entityTypeHandleUniqueIndexInspection, string, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryEntityTypeHandleUniqueIndex)
	if err != nil {
		return entityTypeHandleUniqueIndexInspection{}, "",
			fmt.Errorf("inspect entity-type handle unique index: %w", err)
	}
	if len(rows) == 0 {
		return entityTypeHandleUniqueIndexInspection{}, "", nil
	}
	if len(rows) != 1 {
		return entityTypeHandleUniqueIndexInspection{},
			fmt.Sprintf("Expected at most one entity-type handle migration index, found %d", len(rows)), nil
	}
	columns, columnsErr := rows[0].String("column_names")
	unique, uniqueErr := rows[0].Bool("is_unique")
	valid, validErr := rows[0].Bool("is_valid")
	ready, readyErr := rows[0].Bool("is_ready")
	if err := errorsJoin(columnsErr, uniqueErr, validErr, readyErr); err != nil {
		return entityTypeHandleUniqueIndexInspection{}, "", err
	}
	if columns != targetEntityTypeUniqueColumns || !unique {
		return entityTypeHandleUniqueIndexInspection{},
			fmt.Sprintf("Relation name %s is used by an unexpected index", entityTypeHandleUniqueIndex), nil
	}
	return entityTypeHandleUniqueIndexInspection{present: true, valid: valid && ready}, "", nil
}
