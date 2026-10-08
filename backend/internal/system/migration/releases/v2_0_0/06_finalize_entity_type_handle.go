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

func newFinalizeEntityTypeHandleOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/06_finalize_entity_type_handle",
			Description:      "Require entity-type handles and replace legacy name uniqueness",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		PreVerifier: verifyEntityTypeFinalization,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "require-handle", Query: `ALTER TABLE "ENTITY_TYPES" ALTER COLUMN HANDLE SET NOT NULL`},
					{ID: "add-handle-uniqueness", Query: `ALTER TABLE "ENTITY_TYPES"
	                        ADD CONSTRAINT "UQ_ENTITY_TYPES_HANDLE_CATEGORY_DEPLOYMENT"
							UNIQUE (HANDLE, CATEGORY, DEPLOYMENT_ID)`},
					{ID: "drop-name-uniqueness", Query: postgresDropLegacyEntityTypeUniqueConstraintSQL},
				},
			},
		},
		PostVerifier: verifyEntityTypeFinalization,
	})
}

func verifyEntityTypeFinalization(
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
			Summary: "The legacy entity-type name schema is ready for contract",
		}, nil
	}
	if hasName && matchesEntityTypeColumn(name, "NO") &&
		hasHandle && matchesEntityTypeColumn(handle, "NO") &&
		!legacyUnique && targetUnique {
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: "Entity types retain names and use required unique handles",
		}, nil
	}
	summary := "The entity-type columns or unique constraints do not match the contract source or target schema"
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_entity_type_contract_schema", Message: summary}},
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

const postgresDropLegacyEntityTypeUniqueConstraintSQL = `DO $migration$
DECLARE
    constraint_name TEXT;
BEGIN
    SELECT c.conname INTO constraint_name
    FROM pg_constraint c
    INNER JOIN pg_class t ON t.oid = c.conrelid
    INNER JOIN pg_namespace n ON n.oid = t.relnamespace
    CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
    INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
    WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_TYPES' AND c.contype = 'u'
    GROUP BY c.oid, c.conname
	HAVING string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) = 'NAME,CATEGORY,DEPLOYMENT_ID';

    IF constraint_name IS NULL THEN
        RAISE EXCEPTION 'legacy ENTITY_TYPES name uniqueness constraint was not found';
    END IF;
    EXECUTE format('ALTER TABLE %I.%I DROP CONSTRAINT %I', CURRENT_SCHEMA(), 'ENTITY_TYPES', constraint_name);
END;
$migration$;`
