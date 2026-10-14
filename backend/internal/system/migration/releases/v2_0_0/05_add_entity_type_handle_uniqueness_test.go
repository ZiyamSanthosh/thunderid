// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestAddEntityTypeHandleUniquenessOperation(t *testing.T) {
	operation, err := newAddEntityTypeHandleUniquenessOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/05_add_entity_type_handle_uniqueness"), definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `UNIQUE (HANDLE, CATEGORY, DEPLOYMENT_ID)`)

	source, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), targetEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestAddEntityTypeHandleUniquenessReadiness(t *testing.T) {
	operation, err := newAddEntityTypeHandleUniquenessOperation()
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
			name: "missing handle column is deferred",
			access: entityTypeContractAccess(
				entityTypeSourceColumnRows(), legacyEntityTypeUniqueColumns),
			expectedState: engine.ReadinessDeferred,
			expectedText:  "depends on the preceding handle backfill",
		},
		{
			name: "source schema is deferred until backfill",
			access: entityTypeContractAccess(
				entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns),
			expectedState: engine.ReadinessDeferred,
			expectedText:  "depends on the preceding handle backfill",
		},
		{
			name: "target schema is ready",
			access: entityTypeContractAccess(
				entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
			expectedState: engine.ReadinessReady,
			expectedText:  "already coexist",
		},
		{
			name: "missing legacy uniqueness blocks migration",
			access: entityTypeContractAccess(
				entityTypeExpandedColumnRows(), targetEntityTypeUniqueColumns),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "legacy uniqueness constraint does not match",
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
			if test.expectedState == engine.ReadinessDeferred {
				assert.Equal(t, []engine.OperationID{backfillEntityTypeHandleOperationID}, result.DeferredUntil)
			}
			if test.findingCount > 0 {
				assert.Equal(t, "unexpected_entity_type_uniqueness_schema", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestAddEntityTypeHandleUniquenessReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newAddEntityTypeHandleUniquenessOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), authorizationPDPReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func entityTypeContractAccess(
	columns []engine.QueryRow,
	uniqueColumns ...string,
) authorizationPDPReadAccess {
	constraintRows := make([]engine.QueryRow, 0, len(uniqueColumns))
	for index, columns := range uniqueColumns {
		constraintRows = append(constraintRows, engine.QueryRow{
			"constraint_name": fmt.Sprintf("entity_type_unique_%d", index),
			"column_names":    columns,
		})
	}
	return authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
		queryEntityTypeMigrationColumns.GetID():  columns,
		queryEntityTypeUniqueConstraints.GetID(): constraintRows,
		queryEntityTypeNullHandles.GetID():       {{"has_null_handles": false}},
	}}
}
