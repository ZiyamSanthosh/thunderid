// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryRevocationCriterionValueLength = dbmodel.DBQuery{
	ID: "MIG-RTP-REV-41-01",
	PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH, -1) AS maximum_length
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA()
			AND TABLE_NAME = 'REVOCATION_CRITERIA' AND COLUMN_NAME = 'criterion_value'`,
}

func newExpandRevocationCriterionValueOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/runtime_persistent/41_expand_revocation_criterion_value",
			Description:      "Increase the revocation criterion value length to 2048 characters",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkExpandRevocationCriterionValueReadiness,
		PreVerifier:      verifyRevocationCriterionValueLength,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "increase-length", Query: `ALTER TABLE "REVOCATION_CRITERIA"
						ALTER COLUMN CRITERION_VALUE TYPE VARCHAR(2048)`},
				},
			},
		},
		PostVerifier: verifyRevocationCriterionValueLength,
	})
}

func checkExpandRevocationCriterionValueReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := verifyRevocationCriterionValueLength(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	switch verification.State {
	case engine.VerificationSource:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "REVOCATION_CRITERIA.CRITERION_VALUE is ready to be expanded to 2048 characters",
		}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "REVOCATION_CRITERIA.CRITERION_VALUE already accepts up to 2048 characters",
		}, nil
	default:
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: verification.Summary,
			Findings: []engine.Finding{{
				Code:    "unexpected_revocation_criterion_value_length",
				Message: verification.Summary,
				Remediation: "Restore REVOCATION_CRITERIA.CRITERION_VALUE to the expected source " +
					"definition or reconcile the column before retrying",
			}},
		}, nil
	}
}

func verifyRevocationCriterionValueLength(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryRevocationCriterionValueLength)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf(
			"inspect REVOCATION_CRITERIA.CRITERION_VALUE column: %w", err)
	}
	if len(rows) != 1 {
		return invalidRevocationCriterionValueLength(fmt.Sprintf(
			"Expected one REVOCATION_CRITERIA.CRITERION_VALUE column, found %d", len(rows))), nil
	}
	nullable, nullableErr := rows[0].String("is_nullable")
	dataType, dataTypeErr := rows[0].String("data_type")
	maximumLength, maximumLengthErr := rows[0].Int64("maximum_length")
	if err := errorsJoin(nullableErr, dataTypeErr, maximumLengthErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if nullable != "NO" || dataType != "character varying" {
		return invalidRevocationCriterionValueLength(fmt.Sprintf(
			"Unexpected REVOCATION_CRITERIA.CRITERION_VALUE definition: nullable=%q, type=%q",
			nullable, dataType)), nil
	}
	switch maximumLength {
	case 255:
		return engine.VerificationResult{
			State:   engine.VerificationSource,
			Summary: "REVOCATION_CRITERIA.CRITERION_VALUE accepts up to 255 characters",
		}, nil
	case 2048:
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: "REVOCATION_CRITERIA.CRITERION_VALUE accepts up to 2048 characters",
		}, nil
	default:
		return invalidRevocationCriterionValueLength(fmt.Sprintf(
			"Unexpected REVOCATION_CRITERIA.CRITERION_VALUE maximum length %d", maximumLength)), nil
	}
}

func invalidRevocationCriterionValueLength(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_revocation_criterion_value_length", Message: summary}},
	}
}
