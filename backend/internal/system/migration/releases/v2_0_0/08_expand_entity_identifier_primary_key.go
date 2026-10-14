// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	legacyEntityIdentifierPrimaryKeyColumns = "ENTITY_ID,DEPLOYMENT_ID,NAME"
	targetEntityIdentifierPrimaryKeyColumns = "ENTITY_ID,DEPLOYMENT_ID,NAME,VALUE"
	entityIdentifierPrimaryKeyIndex         = "idx_entity_identifier_expanded_primary_key"
)

var (
	queryEntityIdentifierTable = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-02",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = 'ENTITY_IDENTIFIER'`,
	}
	queryEntityIdentifierColumns = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-03",
		PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable,
			DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_IDENTIFIER'
			AND UPPER(COLUMN_NAME) IN ('DEPLOYMENT_ID', 'ENTITY_ID', 'NAME', 'VALUE')
			ORDER BY ORDINAL_POSITION`,
	}
	queryEntityIdentifierPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-04",
		PostgresQuery: `SELECT c.conname AS constraint_name,
			string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_IDENTIFIER'
			AND c.contype = 'p'
			GROUP BY c.oid, c.conname`,
	}
	queryEntityIdentifierPrimaryKeyIndexName = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-05",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA()
			AND c.relname = 'idx_entity_identifier_expanded_primary_key'`,
	}
)

type entityIdentifierPrimaryKeyInspection struct {
	state   engine.VerificationState
	summary string
}

type entityIdentifierColumn struct {
	nullable      string
	dataType      string
	maximumLength string
}

var expectedEntityIdentifierColumns = map[string]entityIdentifierColumn{
	"DEPLOYMENT_ID": {nullable: "NO", dataType: "character varying", maximumLength: "255"},
	"ENTITY_ID":     {nullable: "NO", dataType: "character varying", maximumLength: "36"},
	"NAME":          {nullable: "NO", dataType: "character varying", maximumLength: "255"},
	"VALUE":         {nullable: "NO", dataType: "text", maximumLength: "none"},
}

func newExpandEntityIdentifierPrimaryKeyOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/entitydb/08_expand_entity_identifier_primary_key",
			Description:      "Allow multiple values for an entity identifier name",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkExpandEntityIdentifierPrimaryKeyReadiness,
		PreVerifier:      verifyEntityIdentifierPrimaryKey,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "create-expanded-primary-key-index", Query: postgresCreateEntityIdentifierPrimaryKeyIndexSQL},
					{ID: "replace-primary-key", Query: postgresReplaceEntityIdentifierPrimaryKeySQL},
				},
			},
		},
		PostVerifier: verifyEntityIdentifierPrimaryKey,
	})
}

func checkExpandEntityIdentifierPrimaryKeyReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectEntityIdentifierPrimaryKey(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The ENTITY_IDENTIFIER primary key is ready to include VALUE",
		}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The ENTITY_IDENTIFIER primary key already includes VALUE",
		}, nil
	default:
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code:        "unexpected_entity_identifier_primary_key_schema",
				Message:     inspection.summary,
				Remediation: "Restore the expected ENTITY_IDENTIFIER source schema or reconcile the primary key before retrying",
			}},
		}, nil
	}
}

func verifyEntityIdentifierPrimaryKey(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := inspectEntityIdentifierPrimaryKey(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_entity_identifier_primary_key_schema", Message: inspection.summary,
		}}
	}
	return result, nil
}

func inspectEntityIdentifierPrimaryKey(
	ctx context.Context,
	access engine.ReadAccess,
) (entityIdentifierPrimaryKeyInspection, error) {
	tableRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityIdentifierTable)
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER table: %w", err)
	}
	if len(tableRows) != 1 {
		return invalidEntityIdentifierPrimaryKey(fmt.Sprintf(
			"Expected one ENTITY_IDENTIFIER table, found %d", len(tableRows))), nil
	}
	relationKind, err := tableRows[0].String("relation_kind")
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER relation kind: %w", err)
	}
	if relationKind != "r" {
		return invalidEntityIdentifierPrimaryKey("Relation name ENTITY_IDENTIFIER is not an ordinary table"), nil
	}

	columnRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityIdentifierColumns)
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER columns: %w", err)
	}
	if err := validateEntityIdentifierColumns(columnRows); err != nil {
		return invalidEntityIdentifierPrimaryKey(err.Error()), nil
	}

	primaryKeyRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityIdentifierPrimaryKey)
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER primary key: %w", err)
	}
	if len(primaryKeyRows) != 1 {
		return invalidEntityIdentifierPrimaryKey(fmt.Sprintf(
			"Expected one ENTITY_IDENTIFIER primary key, found %d", len(primaryKeyRows))), nil
	}
	primaryKeyColumns, err := primaryKeyRows[0].String("column_names")
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER primary key columns: %w", err)
	}

	indexRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityIdentifierPrimaryKeyIndexName)
	if err != nil {
		return entityIdentifierPrimaryKeyInspection{}, fmt.Errorf("inspect ENTITY_IDENTIFIER migration index name: %w", err)
	}
	if len(indexRows) != 0 {
		return invalidEntityIdentifierPrimaryKey(fmt.Sprintf(
			"Relation name %s is already in use", entityIdentifierPrimaryKeyIndex)), nil
	}

	switch primaryKeyColumns {
	case legacyEntityIdentifierPrimaryKeyColumns:
		return entityIdentifierPrimaryKeyInspection{
			state:   engine.VerificationSource,
			summary: "ENTITY_IDENTIFIER uses the legacy primary key without VALUE",
		}, nil
	case targetEntityIdentifierPrimaryKeyColumns:
		return entityIdentifierPrimaryKeyInspection{
			state:   engine.VerificationTarget,
			summary: "ENTITY_IDENTIFIER uses the primary key including VALUE",
		}, nil
	default:
		return invalidEntityIdentifierPrimaryKey(fmt.Sprintf(
			"Unexpected ENTITY_IDENTIFIER primary key columns %q", primaryKeyColumns)), nil
	}
}

func validateEntityIdentifierColumns(rows []engine.QueryRow) error {
	if len(rows) != len(expectedEntityIdentifierColumns) {
		return fmt.Errorf("expected %d relevant ENTITY_IDENTIFIER columns, found %d",
			len(expectedEntityIdentifierColumns), len(rows))
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		name, err := row.String("column_name")
		if err != nil {
			return err
		}
		expected, exists := expectedEntityIdentifierColumns[name]
		if !exists || seen[name] {
			return fmt.Errorf("unexpected ENTITY_IDENTIFIER column %q", name)
		}
		seen[name] = true
		nullable, err := row.String("is_nullable")
		if err != nil {
			return err
		}
		dataType, err := row.String("data_type")
		if err != nil {
			return err
		}
		maximumLength, err := row.String("maximum_length")
		if err != nil {
			return err
		}
		if nullable != expected.nullable || dataType != expected.dataType || maximumLength != expected.maximumLength {
			return fmt.Errorf(
				"unexpected ENTITY_IDENTIFIER.%s definition: nullable=%q, type=%q, maximumLength=%q",
				name, nullable, dataType, maximumLength)
		}
	}
	return nil
}

func invalidEntityIdentifierPrimaryKey(summary string) entityIdentifierPrimaryKeyInspection {
	return entityIdentifierPrimaryKeyInspection{state: engine.VerificationInvalid, summary: summary}
}

const postgresCreateEntityIdentifierPrimaryKeyIndexSQL = `CREATE UNIQUE INDEX
	idx_entity_identifier_expanded_primary_key
	ON "ENTITY_IDENTIFIER" (ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)`

const postgresReplaceEntityIdentifierPrimaryKeySQL = `DO $migration$
DECLARE
	primary_key_name TEXT;
BEGIN
	SELECT c.conname INTO primary_key_name
	FROM pg_constraint c
	INNER JOIN pg_class t ON t.oid = c.conrelid
	INNER JOIN pg_namespace n ON n.oid = t.relnamespace
	CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
	INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
	WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_IDENTIFIER'
	AND c.contype = 'p'
	GROUP BY c.oid, c.conname
	HAVING string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) =
		'ENTITY_ID,DEPLOYMENT_ID,NAME';

	IF primary_key_name IS NULL THEN
		RAISE EXCEPTION 'expected legacy ENTITY_IDENTIFIER primary key was not found';
	END IF;

	EXECUTE format('ALTER TABLE %I.%I DROP CONSTRAINT %I',
		CURRENT_SCHEMA(), 'ENTITY_IDENTIFIER', primary_key_name);
	EXECUTE format('ALTER TABLE %I.%I ADD CONSTRAINT %I PRIMARY KEY USING INDEX %I',
		CURRENT_SCHEMA(), 'ENTITY_IDENTIFIER', primary_key_name,
		'idx_entity_identifier_expanded_primary_key');
END;
$migration$;`
