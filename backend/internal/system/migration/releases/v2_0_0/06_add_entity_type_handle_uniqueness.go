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

const (
	legacyEntityTypeUniqueColumns = "NAME,CATEGORY,DEPLOYMENT_ID"
	targetEntityTypeUniqueColumns = "HANDLE,CATEGORY,DEPLOYMENT_ID"
)

func newAddEntityTypeHandleUniquenessOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/06_add_entity_type_handle_uniqueness",
			Description:      "Enforce unique entity-type handles while retaining legacy name uniqueness",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkAddEntityTypeHandleUniquenessReadiness,
		PreVerifier:      verifyEntityTypeHandleUniqueness,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "add-handle-uniqueness", Query: `ALTER TABLE "ENTITY_TYPES"
	                        ADD CONSTRAINT "UQ_ENTITY_TYPES_HANDLE_CATEGORY_DEPLOYMENT"
							UNIQUE (HANDLE, CATEGORY, DEPLOYMENT_ID)`},
				},
			},
		},
		PostVerifier: verifyEntityTypeHandleUniqueness,
	})
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
		legacyUnique && targetUnique {
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: "Entity-type name and handle uniqueness constraints coexist",
		}, nil
	}
	summary := "The entity-type columns or unique constraints do not match the migrate source or target schema"
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_entity_type_uniqueness_schema", Message: summary}},
	}, nil
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
