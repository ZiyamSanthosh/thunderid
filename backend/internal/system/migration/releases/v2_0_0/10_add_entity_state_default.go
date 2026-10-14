// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const addEntityStateDefaultOperationID engine.OperationID = "2.0.0/entitydb/10_add_entity_state_default"

var (
	queryEntityStateColumn = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-06",
		PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length,
			COALESCE(COLUMN_DEFAULT, 'none') AS column_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY' AND COLUMN_NAME = 'state'`,
	}
	queryNonActiveEntityState = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-07",
		PostgresQuery: `SELECT EXISTS (
			SELECT 1 FROM "ENTITY" WHERE STATE IS DISTINCT FROM 'ACTIVE'
		) AS has_non_active_state`,
	}
)

type entityStateInspection struct {
	state   engine.VerificationState
	summary string
	absent  bool
}

func newAddEntityStateDefaultOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               addEntityStateDefaultOperationID,
			Description:      "Default legacy entity state for mixed-version creation",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkAddEntityStateDefaultReadiness,
		PreVerifier:      verifyEntityStateDefault,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "set-active-default", Query: `ALTER TABLE "ENTITY" ALTER COLUMN STATE SET DEFAULT 'ACTIVE'`},
				},
			},
		},
		PostVerifier: verifyEntityStateDefault,
	})
}

func checkAddEntityStateDefaultReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectEntityState(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if inspection.absent {
		return blockedEntityStateReadiness("The ENTITY.STATE column is absent before contract"), nil
	}
	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The legacy entity state column is ready to receive an ACTIVE default"}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The legacy entity state column already defaults to ACTIVE"}, nil
	default:
		return blockedEntityStateReadiness(inspection.summary), nil
	}
}

func blockedEntityStateReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "unexpected_entity_state_schema", Message: summary,
			Remediation: "Restore the expected ACTIVE-only ENTITY.STATE schema before retrying",
		}},
	}
}

func verifyEntityStateDefault(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := inspectEntityState(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if inspection.absent {
		inspection.state = engine.VerificationInvalid
		inspection.summary = "The ENTITY.STATE column is absent before contract"
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{Code: "unexpected_entity_state_schema", Message: inspection.summary}}
	}
	return result, nil
}

func inspectEntityState(ctx context.Context, access engine.ReadAccess) (entityStateInspection, error) {
	columnRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityStateColumn)
	if err != nil {
		return entityStateInspection{}, fmt.Errorf("inspect ENTITY.STATE column: %w", err)
	}
	if len(columnRows) == 0 {
		return entityStateInspection{state: engine.VerificationTarget,
			summary: "The ENTITY.STATE column is absent", absent: true}, nil
	}
	if len(columnRows) != 1 {
		return invalidEntityState(fmt.Sprintf("Expected at most one ENTITY.STATE column, found %d", len(columnRows))), nil
	}
	row := columnRows[0]
	nullable, err := row.String("is_nullable")
	if err != nil {
		return entityStateInspection{}, err
	}
	dataType, err := row.String("data_type")
	if err != nil {
		return entityStateInspection{}, err
	}
	maximumLength, err := row.String("maximum_length")
	if err != nil {
		return entityStateInspection{}, err
	}
	defaultValue, err := row.String("column_default")
	if err != nil {
		return entityStateInspection{}, err
	}
	if nullable != "NO" || dataType != "character varying" || maximumLength != "50" {
		return invalidEntityState(fmt.Sprintf(
			"Unexpected ENTITY.STATE definition: nullable=%q, type=%q, maximumLength=%q",
			nullable, dataType, maximumLength)), nil
	}
	stateRows, err := access.Query(ctx, engine.StoreIDEntityDB, queryNonActiveEntityState)
	if err != nil {
		return entityStateInspection{}, fmt.Errorf("inspect ENTITY.STATE values: %w", err)
	}
	if len(stateRows) != 1 {
		return entityStateInspection{}, fmt.Errorf(
			"inspect ENTITY.STATE values: expected one result row, found %d", len(stateRows))
	}
	hasNonActiveState, err := stateRows[0].Bool("has_non_active_state")
	if err != nil {
		return entityStateInspection{}, err
	}
	if hasNonActiveState {
		return invalidEntityState("ENTITY contains state values other than ACTIVE"), nil
	}
	switch defaultValue {
	case "none":
		return entityStateInspection{state: engine.VerificationSource,
			summary: "ENTITY.STATE is required, ACTIVE-only, and has no default"}, nil
	case "'ACTIVE'::character varying":
		return entityStateInspection{state: engine.VerificationTarget,
			summary: "ENTITY.STATE is required, ACTIVE-only, and defaults to ACTIVE"}, nil
	default:
		return invalidEntityState(fmt.Sprintf("Unexpected ENTITY.STATE default %q", defaultValue)), nil
	}
}

func invalidEntityState(summary string) entityStateInspection {
	return entityStateInspection{state: engine.VerificationInvalid, summary: summary}
}
