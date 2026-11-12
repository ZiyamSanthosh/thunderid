// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"strings"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type legacyPrimaryKeyContractOperation struct {
	definition engine.OperationDefinition
	expand     *onlinePrimaryKeyOperation
}

func newLegacyPrimaryKeyContractOperation(expand engine.Operation) (engine.Operation, error) {
	expandOperation, ok := expand.(*onlinePrimaryKeyOperation)
	if !ok {
		return nil, fmt.Errorf("create primary-key contract operation: unsupported expand operation %T", expand)
	}
	expandDefinition := expandOperation.Definition()
	operationID := strings.Replace(string(expandDefinition.ID), "_expand_", "_finalize_", 1)
	if operationID == string(expandDefinition.ID) {
		return nil, fmt.Errorf("create primary-key contract operation: expand operation ID %q has no expand segment",
			expandDefinition.ID)
	}
	return &legacyPrimaryKeyContractOperation{
		definition: engine.OperationDefinition{
			ID:               engine.OperationID(operationID),
			Description:      fmt.Sprintf("Remove legacy global uniqueness from %s", expandOperation.config.table),
			StoreID:          expandDefinition.StoreID,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		expand: expandOperation,
	}, nil
}

func newLegacyPrimaryKeyContractOperations(expandOperations []engine.Operation) ([]engine.Operation, error) {
	operations := make([]engine.Operation, 0, len(expandOperations))
	for _, expandOperation := range expandOperations {
		operation, err := newLegacyPrimaryKeyContractOperation(expandOperation)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

func (o *legacyPrimaryKeyContractOperation) Definition() engine.OperationDefinition {
	return o.definition
}

func (o *legacyPrimaryKeyContractOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	config := o.expand.config
	primaryKeyRows, err := access.Query(ctx, o.definition.StoreID, o.expand.primaryKeyQuery, config.table)
	if err != nil {
		return engine.ReadinessResult{}, fmt.Errorf("inspect %s primary key for contract readiness: %w",
			config.table, err)
	}
	if len(primaryKeyRows) == 1 {
		primaryKeyColumns, rowErr := primaryKeyRows[0].String("column_names")
		if rowErr != nil {
			return engine.ReadinessResult{}, rowErr
		}
		if primaryKeyColumns == strings.Join(config.legacyColumns, ",") {
			return engine.ReadinessResult{
				State:   engine.ReadinessDeferred,
				Summary: fmt.Sprintf("%s legacy uniqueness removal waits for primary-key expansion", config.table),
			}, nil
		}
	}
	verification, err := o.verify(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary,
			Findings: []engine.Finding{{
				Code: "unexpected_primary_key_contract_schema", Message: verification.Summary,
				Remediation: "Complete or reconcile the deployment-scoped primary-key expansion before retrying contract",
			}},
		}, nil
	}
	return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
}

func (o *legacyPrimaryKeyContractOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *legacyPrimaryKeyContractOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *legacyPrimaryKeyContractOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	verification, err := o.verify(ctx, execution.Access())
	if err != nil {
		return err
	}
	if verification.State == engine.VerificationTarget {
		return nil
	}
	if verification.State != engine.VerificationSource {
		return fmt.Errorf("remove %s legacy primary-key uniqueness: %s",
			o.expand.config.table, verification.Summary)
	}
	if _, err := execution.Access().Execute(
		ctx, o.definition.StoreID, o.expand.dropLegacyIndexQuery); err != nil {
		return fmt.Errorf("drop %s legacy primary-key compatibility index concurrently: %w",
			o.expand.config.table, err)
	}
	return nil
}

func (o *legacyPrimaryKeyContractOperation) Finalize(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	verification, err := o.verify(ctx, execution.Access())
	if err != nil {
		return err
	}
	if verification.State != engine.VerificationTarget {
		return fmt.Errorf("finalize %s legacy primary-key uniqueness removal: %s",
			o.expand.config.table, verification.Summary)
	}
	return nil
}

func (o *legacyPrimaryKeyContractOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *legacyPrimaryKeyContractOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, error) {
	config := o.expand.config
	primaryKeyRows, err := access.Query(ctx, o.definition.StoreID, o.expand.primaryKeyQuery, config.table)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s primary key for contract: %w", config.table, err)
	}
	if len(primaryKeyRows) != 1 {
		return invalidPrimaryKeyContractVerification(fmt.Sprintf("Expected one %s primary key, found %d",
			config.table, len(primaryKeyRows))), nil
	}
	primaryKeyColumns, err := primaryKeyRows[0].String("column_names")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if primaryKeyColumns != strings.Join(config.targetColumns, ",") {
		return invalidPrimaryKeyContractVerification(fmt.Sprintf(
			"Expected %s target primary key columns %q, found %q",
			config.table, strings.Join(config.targetColumns, ","), primaryKeyColumns)), nil
	}

	indexName := legacyPrimaryKeyIndexName(config.table)
	indexRows, err := access.Query(ctx, o.definition.StoreID, o.expand.legacyIndexQuery, config.table, indexName)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s legacy compatibility index for contract: %w",
			config.table, err)
	}
	if len(indexRows) == 0 {
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: fmt.Sprintf("%s no longer enforces legacy global primary-key uniqueness", config.table),
		}, nil
	}
	if len(indexRows) != 1 {
		return invalidPrimaryKeyContractVerification(fmt.Sprintf(
			"Expected at most one %s legacy compatibility index, found %d", config.table, len(indexRows))), nil
	}
	columns, columnsErr := indexRows[0].String("column_names")
	unique, uniqueErr := indexRows[0].Bool("is_unique")
	valid, validErr := indexRows[0].Bool("is_valid")
	ready, readyErr := indexRows[0].Bool("is_ready")
	if err := errorsJoin(columnsErr, uniqueErr, validErr, readyErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if columns != strings.Join(config.legacyColumns, ",") || !unique || !valid || !ready {
		return invalidPrimaryKeyContractVerification(fmt.Sprintf(
			"Legacy compatibility index %s does not match the expected valid unique definition", indexName)), nil
	}
	return engine.VerificationResult{
		State:   engine.VerificationSource,
		Summary: fmt.Sprintf("%s still enforces legacy global primary-key uniqueness", config.table),
	}, nil
}

func invalidPrimaryKeyContractVerification(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_primary_key_contract_schema", Message: summary}},
	}
}
