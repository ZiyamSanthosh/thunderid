// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestFinalizeEntityTypeHandleOperation(t *testing.T) {
	operation, err := newFinalizeEntityTypeHandleOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/06_finalize_entity_type_handle"), definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 3)
	assert.Contains(t, implementation.Statements[1].Query, `ALTER COLUMN HANDLE SET NOT NULL`)
	assert.NotContains(t, implementation.Statements[2].Query, `DROP COLUMN NAME`)

	source, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeContractAccess(
		entityTypeFinalColumnRows(), targetEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestFinalizeEntityTypeHandleReadiness(t *testing.T) {
	operation, err := newFinalizeEntityTypeHandleOperation()
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		access        authorizationPDPReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{
			name: "source schema is ready",
			access: entityTypeContractAccess(
				entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
			expectedState: engine.ReadinessReady,
			expectedText:  "ready to be finalized",
		},
		{
			name: "target schema is ready",
			access: entityTypeContractAccess(
				entityTypeFinalColumnRows(), targetEntityTypeUniqueColumns),
			expectedState: engine.ReadinessReady,
			expectedText:  "already finalized",
		},
		{
			name: "remaining null handles block contract",
			access: func() authorizationPDPReadAccess {
				access := entityTypeContractAccess(
					entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns)
				access.rows[queryEntityTypeNullHandles.GetID()] = []engine.QueryRow{{"has_null_handles": true}}
				return access
			}(),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "still contain null values",
			findingCount:  1,
		},
		{
			name: "incomplete migrate schema blocks contract",
			access: entityTypeContractAccess(
				entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "do not match the contract source or target schema",
			findingCount:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := sqlOperation.CheckReadiness(
				context.Background(), test.access, engine.PlanContext{})

			require.NoError(t, err)
			assert.Equal(t, test.expectedState, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, test.findingCount)
			if test.findingCount > 0 {
				assert.Equal(t, "unexpected_entity_type_contract_schema", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestFinalizeEntityTypeHandleReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newFinalizeEntityTypeHandleOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), authorizationPDPReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}
