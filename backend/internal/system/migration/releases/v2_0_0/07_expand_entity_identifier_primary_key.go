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

var queryEntityIdentifierColumns = dbmodel.DBQuery{
	ID: "MIG-ENT_MGT-03",
	PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable,
		DATA_TYPE AS data_type,
		COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'ENTITY_IDENTIFIER'
		AND UPPER(COLUMN_NAME) IN ('DEPLOYMENT_ID', 'ENTITY_ID', 'NAME', 'VALUE')
		ORDER BY ORDINAL_POSITION`,
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

type entityIdentifierPrimaryKeyOperation struct {
	*onlinePrimaryKeyOperation
}

func newExpandEntityIdentifierPrimaryKeyOperation() (engine.Operation, error) {
	operation, err := newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:               "2.0.0/entitydb/07_expand_entity_identifier_primary_key",
			Description:      "Allow multiple values for an entity identifier name",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table:         "ENTITY_IDENTIFIER",
		legacyColumns: []string{"ENTITY_ID", "DEPLOYMENT_ID", "NAME"},
		targetColumns: []string{"ENTITY_ID", "DEPLOYMENT_ID", "NAME", "VALUE"},
		indexName:     entityIdentifierPrimaryKeyIndex,
		queryPrefix:   "MIG-ENTITY-PK-07",
		sourceSummary: "The ENTITY_IDENTIFIER primary key is ready to include VALUE",
		targetSummary: "The ENTITY_IDENTIFIER primary key already includes VALUE",
		findingCode:   "unexpected_entity_identifier_primary_key_schema",
		remediation:   "Restore the expected ENTITY_IDENTIFIER source schema or reconcile the primary key before retrying",
	})
	if err != nil {
		return nil, err
	}
	return &entityIdentifierPrimaryKeyOperation{onlinePrimaryKeyOperation: operation}, nil
}

func (o *entityIdentifierPrimaryKeyOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := o.verify(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State != engine.VerificationInvalid {
		return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
	}
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: verification.Summary,
		Findings: []engine.Finding{{
			Code: "unexpected_entity_identifier_primary_key_schema", Message: verification.Summary,
			Remediation: "Restore the expected ENTITY_IDENTIFIER source schema or reconcile the primary key before retrying",
		}},
	}, nil
}

func (o *entityIdentifierPrimaryKeyOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *entityIdentifierPrimaryKeyOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *entityIdentifierPrimaryKeyOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, error) {
	invalidSummary, err := inspectEntityIdentifierColumnDefinitions(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if invalidSummary != "" {
		return engine.VerificationResult{
			State: engine.VerificationInvalid, Summary: invalidSummary,
			Findings: []engine.Finding{{
				Code: "unexpected_entity_identifier_primary_key_schema", Message: invalidSummary,
			}},
		}, nil
	}
	return o.onlinePrimaryKeyOperation.verify(ctx, access)
}

func inspectEntityIdentifierColumnDefinitions(
	ctx context.Context,
	access engine.ReadAccess,
) (string, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryEntityIdentifierColumns)
	if err != nil {
		return "", fmt.Errorf("inspect ENTITY_IDENTIFIER columns: %w", err)
	}
	if err := validateEntityIdentifierColumns(rows); err != nil {
		return err.Error(), nil
	}
	return "", nil
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
