// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryEntityTypeLength = dbmodel.DBQuery{
	ID: "MIG-ENT_MGT-01",
	PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
            CHARACTER_MAXIMUM_LENGTH AS maximum_length
        FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY' AND COLUMN_NAME = 'type'`,
}

func newExpandEntityTypeLengthOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/entitydb/04_expand_entity_type_length",
			Description:      "Increase the entity type column length to 100 characters",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		PreVerifier: verifyEntityTypeLength,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "increase-length", Query: `ALTER TABLE "ENTITY" ALTER COLUMN TYPE TYPE VARCHAR(100)`},
				},
			},
		},
		PostVerifier: verifyEntityTypeLength,
	})
}

func verifyEntityTypeLength(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityTypeLength)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect ENTITY.TYPE column: %w", err)
	}
	if len(rows) != 1 {
		return invalidEntityTypeLength(fmt.Sprintf("Expected one ENTITY.TYPE column, found %d", len(rows))), nil
	}
	nullable, err := rows[0].String("is_nullable")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	dataType, err := rows[0].String("data_type")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	maximumLength, err := rows[0].Int64("maximum_length")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if nullable != "NO" || dataType != "character varying" {
		return invalidEntityTypeLength(fmt.Sprintf(
			"Unexpected ENTITY.TYPE definition: nullable=%q, type=%q", nullable, dataType)), nil
	}
	switch maximumLength {
	case 50:
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: "ENTITY.TYPE accepts up to 50 characters",
		}, nil
	case 100:
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "ENTITY.TYPE accepts up to 100 characters",
		}, nil
	default:
		return invalidEntityTypeLength(fmt.Sprintf(
			"Unexpected ENTITY.TYPE maximum length %d", maximumLength)), nil
	}
}

func invalidEntityTypeLength(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_entity_type_length", Message: summary}},
	}
}
