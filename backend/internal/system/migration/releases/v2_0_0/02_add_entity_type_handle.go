// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryEntityTypeMigrationColumns = dbmodel.DBQuery{
	ID: "MIG-CFG_MGT-05",
	PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable,
            DATA_TYPE AS data_type, CHARACTER_MAXIMUM_LENGTH AS maximum_length
        FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_TYPES'
				AND COLUMN_NAME IN ('name', 'handle')`,
}

type entityTypeMigrationColumn struct {
	nullable      string
	dataType      string
	maximumLength int64
}

func newAddEntityTypeHandleOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/02_add_entity_type_handle",
			Description:      "Add a nullable entity-type handle column",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkAddEntityTypeHandleReadiness,
		PreVerifier:      verifyEntityTypeColumnExpansion,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "add-handle", Query: `ALTER TABLE "ENTITY_TYPES" ADD COLUMN HANDLE VARCHAR(100)`},
				},
			},
		},
		PostVerifier: verifyEntityTypeColumnExpansion,
	})
}

func checkAddEntityTypeHandleReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	state, summary := classifyEntityTypeColumnExpansion(columns)
	switch state {
	case engine.VerificationSource:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The nullable entity-type handle column is ready to be added",
		}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The nullable entity-type handle column is already present",
		}, nil
	default:
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: summary,
			Findings: []engine.Finding{{
				Code:        "unexpected_entity_type_column_expansion",
				Message:     summary,
				Remediation: "Restore the expected ENTITY_TYPES source schema or reconcile the HANDLE column before retrying",
			}},
		}, nil
	}
}

func verifyEntityTypeColumnExpansion(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}

	state, summary := classifyEntityTypeColumnExpansion(columns)
	result := engine.VerificationResult{State: state, Summary: summary}
	if state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_entity_type_column_expansion", Message: summary,
		}}
	}
	return result, nil
}

func inspectEntityTypeMigrationColumns(
	ctx context.Context,
	access engine.ReadAccess,
) (map[string]entityTypeMigrationColumn, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryEntityTypeMigrationColumns)
	if err != nil {
		return nil, fmt.Errorf("inspect entity-type migration columns: %w", err)
	}
	columns := make(map[string]entityTypeMigrationColumn, len(rows))
	for _, row := range rows {
		name, err := row.String("column_name")
		if err != nil {
			return nil, err
		}
		nullable, err := row.String("is_nullable")
		if err != nil {
			return nil, err
		}
		dataType, err := row.String("data_type")
		if err != nil {
			return nil, err
		}
		maximumLength, err := row.Int64("maximum_length")
		if err != nil {
			return nil, err
		}
		columns[name] = entityTypeMigrationColumn{
			nullable: nullable, dataType: dataType, maximumLength: maximumLength,
		}
	}
	return columns, nil
}

func classifyEntityTypeColumnExpansion(
	columns map[string]entityTypeMigrationColumn,
) (engine.VerificationState, string) {
	name, hasName := columns["NAME"]
	if !hasName || !matchesEntityTypeColumn(name, "NO") {
		return engine.VerificationInvalid, "ENTITY_TYPES.NAME does not match the accepted source schema"
	}

	handle, hasHandle := columns["HANDLE"]
	if !hasHandle {
		return engine.VerificationSource, "The entity-type handle column is absent"
	}
	if matchesEntityTypeColumn(handle, "YES") {
		return engine.VerificationTarget, "The nullable entity-type handle column is present"
	}
	return engine.VerificationInvalid, "The entity-type handle column has an unexpected definition"
}

func matchesEntityTypeColumn(column entityTypeMigrationColumn, nullable string) bool {
	return column.nullable == nullable && column.dataType == "character varying" && column.maximumLength == 100
}
