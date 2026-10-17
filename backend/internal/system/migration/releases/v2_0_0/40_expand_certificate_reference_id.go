// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryCertificateReferenceIDLength = dbmodel.DBQuery{
	ID: "MIG-CONFIG-CERT-40-01",
	PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH, -1) AS maximum_length
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'CERTIFICATE' AND COLUMN_NAME = 'ref_id'`,
}

func newExpandCertificateReferenceIDOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/40_expand_certificate_reference_id",
			Description:      "Increase the certificate reference ID length to 2048 characters",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkExpandCertificateReferenceIDReadiness,
		PreVerifier:      verifyCertificateReferenceIDLength,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "increase-length", Query: `ALTER TABLE "CERTIFICATE" ALTER COLUMN REF_ID TYPE VARCHAR(2048)`},
				},
			},
		},
		PostVerifier: verifyCertificateReferenceIDLength,
	})
}

func checkExpandCertificateReferenceIDReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := verifyCertificateReferenceIDLength(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}

	switch verification.State {
	case engine.VerificationSource:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "CERTIFICATE.REF_ID is ready to be expanded to 2048 characters",
		}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "CERTIFICATE.REF_ID already accepts up to 2048 characters",
		}, nil
	default:
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: verification.Summary,
			Findings: []engine.Finding{{
				Code:        "unexpected_certificate_reference_id_length",
				Message:     verification.Summary,
				Remediation: "Restore CERTIFICATE.REF_ID to the expected source definition or reconcile the column before retrying",
			}},
		}, nil
	}
}

func verifyCertificateReferenceIDLength(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryCertificateReferenceIDLength)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect CERTIFICATE.REF_ID column: %w", err)
	}
	if len(rows) != 1 {
		return invalidCertificateReferenceIDLength(fmt.Sprintf(
			"Expected one CERTIFICATE.REF_ID column, found %d", len(rows))), nil
	}
	nullable, nullableErr := rows[0].String("is_nullable")
	dataType, dataTypeErr := rows[0].String("data_type")
	maximumLength, maximumLengthErr := rows[0].Int64("maximum_length")
	if err := errorsJoin(nullableErr, dataTypeErr, maximumLengthErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if nullable != "NO" || dataType != "character varying" {
		return invalidCertificateReferenceIDLength(fmt.Sprintf(
			"Unexpected CERTIFICATE.REF_ID definition: nullable=%q, type=%q", nullable, dataType)), nil
	}
	switch maximumLength {
	case 36:
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: "CERTIFICATE.REF_ID accepts up to 36 characters",
		}, nil
	case 2048:
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "CERTIFICATE.REF_ID accepts up to 2048 characters",
		}, nil
	default:
		return invalidCertificateReferenceIDLength(fmt.Sprintf(
			"Unexpected CERTIFICATE.REF_ID maximum length %d", maximumLength)), nil
	}
}

func invalidCertificateReferenceIDLength(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_certificate_reference_id_length", Message: summary}},
	}
}
