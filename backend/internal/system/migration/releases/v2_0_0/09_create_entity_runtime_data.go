// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const createEntityRuntimeDataOperationID engine.OperationID = "2.0.0/runtime_persistent/09_create_entity_runtime_data"

var (
	queryEntityRuntimeDataTable = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-03",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = 'ENTITY_RUNTIME_DATA'`,
	}
	queryEntityRuntimeDataColumns = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-04",
		PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable,
			DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length,
			COALESCE(COLUMN_DEFAULT, 'none') AS column_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_RUNTIME_DATA'
			ORDER BY ORDINAL_POSITION`,
	}
	queryEntityRuntimeDataPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-05",
		PostgresQuery: `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_RUNTIME_DATA' AND c.contype = 'p'
			GROUP BY c.oid`,
	}
	queryEntityRuntimeDataChecks = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-06",
		PostgresQuery: `SELECT pg_get_constraintdef(c.oid, TRUE) AS definition
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'ENTITY_RUNTIME_DATA' AND c.contype = 'c'
			ORDER BY c.conname`,
	}
)

type entityRuntimeDataInspection struct {
	state   engine.VerificationState
	summary string
}

type entityRuntimeDataColumn struct {
	name          string
	nullable      string
	dataType      string
	maximumLength string
	defaultValue  string
}

var expectedEntityRuntimeDataColumns = []entityRuntimeDataColumn{
	{name: "DEPLOYMENT_ID", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
	{name: "ENTITY_ID", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
	{name: "STATE", nullable: "NO", dataType: "character varying", maximumLength: "50", defaultValue: "none"},
	{name: "RUNTIME_ATTRIBUTES", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "'{}'::jsonb"},
	{name: "REVISION", nullable: "NO", dataType: "bigint", maximumLength: "none", defaultValue: "0"},
	{name: "CREATED_AT", nullable: "NO", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "none"},
	{name: "UPDATED_AT", nullable: "NO", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "none"},
}

func newCreateEntityRuntimeDataOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               createEntityRuntimeDataOperationID,
			Description:      "Create the entity runtime data table",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkCreateEntityRuntimeDataReadiness,
		PreVerifier:      verifyEntityRuntimeData,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{{ID: "create-table", Query: postgresCreateEntityRuntimeDataSQL}},
			},
		},
		PostVerifier: verifyEntityRuntimeData,
	})
}

func checkCreateEntityRuntimeDataReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectEntityRuntimeData(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The entity runtime data table is ready to be created"}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The entity runtime data table already matches the target schema"}, nil
	default:
		return blockedEntityRuntimeDataReadiness(inspection.summary), nil
	}
}

func blockedEntityRuntimeDataReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "unexpected_entity_runtime_data_schema", Message: summary,
			Remediation: "Remove or reconcile the conflicting ENTITY_RUNTIME_DATA schema before retrying",
		}},
	}
}

func verifyEntityRuntimeData(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := inspectEntityRuntimeData(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_entity_runtime_data_schema", Message: inspection.summary,
		}}
	}
	return result, nil
}

func inspectEntityRuntimeData(
	ctx context.Context,
	access engine.ReadAccess,
) (entityRuntimeDataInspection, error) {
	tableRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryEntityRuntimeDataTable)
	if err != nil {
		return entityRuntimeDataInspection{}, fmt.Errorf("inspect ENTITY_RUNTIME_DATA table: %w", err)
	}
	if len(tableRows) == 0 {
		return entityRuntimeDataInspection{state: engine.VerificationSource,
			summary: "The ENTITY_RUNTIME_DATA table is absent"}, nil
	}
	if len(tableRows) != 1 {
		return invalidEntityRuntimeData(fmt.Sprintf(
			"Expected one ENTITY_RUNTIME_DATA relation, found %d", len(tableRows))), nil
	}
	relationKind, err := tableRows[0].String("relation_kind")
	if err != nil {
		return entityRuntimeDataInspection{}, err
	}
	if relationKind != "r" {
		return invalidEntityRuntimeData("Relation name ENTITY_RUNTIME_DATA is not an ordinary table"), nil
	}

	columnRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryEntityRuntimeDataColumns)
	if err != nil {
		return entityRuntimeDataInspection{}, fmt.Errorf("inspect ENTITY_RUNTIME_DATA columns: %w", err)
	}
	if err := validateEntityRuntimeDataColumns(columnRows); err != nil {
		return invalidEntityRuntimeData(err.Error()), nil
	}
	primaryKeyRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryEntityRuntimeDataPrimaryKey)
	if err != nil {
		return entityRuntimeDataInspection{}, fmt.Errorf("inspect ENTITY_RUNTIME_DATA primary key: %w", err)
	}
	if len(primaryKeyRows) != 1 {
		return invalidEntityRuntimeData(fmt.Sprintf(
			"Expected one ENTITY_RUNTIME_DATA primary key, found %d", len(primaryKeyRows))), nil
	}
	primaryKeyColumns, err := primaryKeyRows[0].String("column_names")
	if err != nil {
		return entityRuntimeDataInspection{}, err
	}
	if primaryKeyColumns != "DEPLOYMENT_ID,ENTITY_ID" {
		return invalidEntityRuntimeData(fmt.Sprintf(
			"Unexpected ENTITY_RUNTIME_DATA primary key columns %q", primaryKeyColumns)), nil
	}
	checkRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryEntityRuntimeDataChecks)
	if err != nil {
		return entityRuntimeDataInspection{}, fmt.Errorf("inspect ENTITY_RUNTIME_DATA checks: %w", err)
	}
	if err := validateEntityRuntimeDataCheck(checkRows); err != nil {
		return invalidEntityRuntimeData(err.Error()), nil
	}
	return entityRuntimeDataInspection{state: engine.VerificationTarget,
		summary: "The ENTITY_RUNTIME_DATA table matches the target schema"}, nil
}

func validateEntityRuntimeDataColumns(rows []engine.QueryRow) error {
	if len(rows) != len(expectedEntityRuntimeDataColumns) {
		return fmt.Errorf("expected %d ENTITY_RUNTIME_DATA columns, found %d",
			len(expectedEntityRuntimeDataColumns), len(rows))
	}
	for index, expected := range expectedEntityRuntimeDataColumns {
		actualName, err := rows[index].String("column_name")
		if err != nil {
			return err
		}
		nullable, err := rows[index].String("is_nullable")
		if err != nil {
			return err
		}
		dataType, err := rows[index].String("data_type")
		if err != nil {
			return err
		}
		maximumLength, err := rows[index].String("maximum_length")
		if err != nil {
			return err
		}
		defaultValue, err := rows[index].String("column_default")
		if err != nil {
			return err
		}
		if actualName != expected.name || nullable != expected.nullable || dataType != expected.dataType ||
			maximumLength != expected.maximumLength || defaultValue != expected.defaultValue {
			return fmt.Errorf("unexpected ENTITY_RUNTIME_DATA.%s definition", actualName)
		}
	}
	return nil
}

func validateEntityRuntimeDataCheck(rows []engine.QueryRow) error {
	if len(rows) != 1 {
		return fmt.Errorf("expected one ENTITY_RUNTIME_DATA check constraint, found %d", len(rows))
	}
	definition, err := rows[0].String("definition")
	if err != nil {
		return err
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(definition, `"`, "")), ""))
	if !strings.Contains(normalized, "jsonb_typeof(runtime_attributes)") ||
		!strings.Contains(normalized, "='object'::text") {
		return fmt.Errorf("unexpected ENTITY_RUNTIME_DATA check constraint %q", definition)
	}
	return nil
}

func invalidEntityRuntimeData(summary string) entityRuntimeDataInspection {
	return entityRuntimeDataInspection{state: engine.VerificationInvalid, summary: summary}
}

const postgresCreateEntityRuntimeDataSQL = `CREATE TABLE "ENTITY_RUNTIME_DATA" (
	DEPLOYMENT_ID       VARCHAR(255) NOT NULL,
	ENTITY_ID           VARCHAR(36) NOT NULL,
	STATE               VARCHAR(50) NOT NULL,
	RUNTIME_ATTRIBUTES  JSONB NOT NULL DEFAULT '{}'::jsonb,
	REVISION            BIGINT NOT NULL DEFAULT 0,
	CREATED_AT          TIMESTAMPTZ NOT NULL,
	UPDATED_AT          TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (DEPLOYMENT_ID, ENTITY_ID),
	CHECK (jsonb_typeof(RUNTIME_ATTRIBUTES) = 'object')
)`
