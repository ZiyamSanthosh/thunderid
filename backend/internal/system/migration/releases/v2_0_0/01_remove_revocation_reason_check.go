// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const legacyRevokedTokenReasonConstraint = "REVOKED_TOKEN_revocation_reason_check"

var (
	queryRevokedTokenReasonColumn = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-01",
		PostgresQuery: `SELECT IS_NULLABLE, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'REVOKED_TOKEN'
            AND COLUMN_NAME = 'revocation_reason'`,
	}
	queryRevokedTokenReasonConstraints = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-02",
		PostgresQuery: `SELECT c.conname AS CONSTRAINT_NAME
            FROM pg_constraint c
            INNER JOIN pg_class t ON t.oid = c.conrelid
            INNER JOIN pg_namespace n ON n.oid = t.relnamespace
            INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
            WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'REVOKED_TOKEN'
            AND a.attname = 'revocation_reason' AND c.contype = 'c'
            ORDER BY c.conname`,
	}
)

type revokedTokenReasonInspection struct {
	state   engine.VerificationState
	summary string
}

func newRemoveRevocationReasonCheckOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/runtime_persistent/01_remove_revocation_reason_check",
			Description:      "Remove the fixed revoked-token reason constraint",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkRemoveRevocationReasonReadiness,
		PreVerifier: func(
			ctx context.Context,
			access engine.ReadAccess,
			_ engine.ExecutionTarget,
		) (engine.VerificationResult, error) {
			return verifyRevokedTokenReason(ctx, access)
		},
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "drop-legacy-constraint", Query: postgresDropRevocationReasonCheckSQL},
				},
			},
		},
		PostVerifier: func(
			ctx context.Context,
			access engine.ReadAccess,
			_ engine.ExecutionTarget,
		) (engine.VerificationResult, error) {
			return verifyRevokedTokenReason(ctx, access)
		},
	})
}

func checkRemoveRevocationReasonReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectRevokedTokenReason(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The legacy revoked-token reason check constraint is ready to be removed",
		}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "The revoked-token reason check constraint has already been removed",
		}, nil
	default:
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code:        "unexpected_revoked_token_reason_schema",
				Message:     inspection.summary,
				Remediation: "Restore the expected source schema or reconcile the constraint manually before retrying",
			}},
		}, nil
	}
}

func verifyRevokedTokenReason(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, error) {
	inspection, err := inspectRevokedTokenReason(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	switch inspection.state {
	case engine.VerificationSource:
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: inspection.summary,
		}, nil
	case engine.VerificationTarget:
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: inspection.summary,
		}, nil
	default:
		return engine.VerificationResult{
			State: engine.VerificationInvalid, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: "unexpected_revoked_token_reason_schema", Message: inspection.summary,
			}},
		}, nil
	}
}

func inspectRevokedTokenReason(
	ctx context.Context,
	access engine.ReadAccess,
) (revokedTokenReasonInspection, error) {
	columnRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryRevokedTokenReasonColumn)
	if err != nil {
		return revokedTokenReasonInspection{}, fmt.Errorf("inspect revoked-token reason column: %w", err)
	}
	if len(columnRows) != 1 {
		return revokedTokenReasonInspection{
			state:   engine.VerificationInvalid,
			summary: fmt.Sprintf("Expected one REVOKED_TOKEN.revocation_reason column, found %d", len(columnRows)),
		}, nil
	}
	if err := validateRevokedTokenReasonColumn(columnRows[0]); err != nil {
		return revokedTokenReasonInspection{state: engine.VerificationInvalid, summary: err.Error()}, nil
	}

	constraintRows, err := access.Query(
		ctx, engine.StoreIDRuntimePersistent, queryRevokedTokenReasonConstraints)
	if err != nil {
		return revokedTokenReasonInspection{}, fmt.Errorf("inspect revoked-token reason constraints: %w", err)
	}
	if len(constraintRows) == 0 {
		return revokedTokenReasonInspection{
			state:   engine.VerificationTarget,
			summary: "The revoked-token reason column has no check constraint",
		}, nil
	}
	if len(constraintRows) != 1 {
		return revokedTokenReasonInspection{
			state: engine.VerificationInvalid,
			summary: fmt.Sprintf("Expected at most one revoked-token reason check constraint, found %d",
				len(constraintRows)),
		}, nil
	}
	constraintName, err := constraintRows[0].String("constraint_name")
	if err != nil {
		return revokedTokenReasonInspection{}, fmt.Errorf("inspect revoked-token reason constraint: %w", err)
	}
	if constraintName != legacyRevokedTokenReasonConstraint {
		return revokedTokenReasonInspection{
			state:   engine.VerificationInvalid,
			summary: fmt.Sprintf("Found unexpected revoked-token reason check constraint %q", constraintName),
		}, nil
	}
	return revokedTokenReasonInspection{
		state:   engine.VerificationSource,
		summary: "The legacy revoked-token reason check constraint is present",
	}, nil
}

func validateRevokedTokenReasonColumn(row engine.QueryRow) error {
	nullable, err := row.String("is_nullable")
	if err != nil {
		return err
	}
	dataType, err := row.String("data_type")
	if err != nil {
		return err
	}
	maxLength, err := row.Int64("character_maximum_length")
	if err != nil {
		return err
	}
	if nullable != "NO" || dataType != "character varying" || maxLength != 30 {
		return fmt.Errorf(
			"unexpected REVOKED_TOKEN.revocation_reason definition: nullable=%q, type=%q, maximumLength=%d",
			nullable, dataType, maxLength)
	}
	return nil
}

const postgresDropRevocationReasonCheckSQL = `DO $migration$
DECLARE
    constraint_name TEXT;
BEGIN
    SELECT c.conname INTO constraint_name
    FROM pg_constraint c
    INNER JOIN pg_class t ON t.oid = c.conrelid
    INNER JOIN pg_namespace n ON n.oid = t.relnamespace
    INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
    WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'REVOKED_TOKEN'
    AND a.attname = 'revocation_reason' AND c.contype = 'c'
    AND c.conname = 'REVOKED_TOKEN_revocation_reason_check';

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE %I.%I DROP CONSTRAINT %I', CURRENT_SCHEMA(), 'REVOKED_TOKEN', constraint_name);
    END IF;
END;
$migration$;`
