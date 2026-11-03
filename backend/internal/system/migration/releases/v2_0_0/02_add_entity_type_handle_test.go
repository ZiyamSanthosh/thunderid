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

func TestAddEntityTypeHandleOperation(t *testing.T) {
	operation, err := newAddEntityTypeHandleOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/02_add_entity_type_handle"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `ADD COLUMN HANDLE VARCHAR(100)`)

	source, err := operation.PreVerify(context.Background(), entityTypeColumnAccess(entityTypeSourceColumnRows()),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeColumnAccess(entityTypeExpandedColumnRows()),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	partialRows := entityTypeSourceColumnRows()
	partialRows = append(partialRows, entityTypeColumnRow("HANDLE", "NO"))
	invalid, err := operation.PreVerify(context.Background(), entityTypeColumnAccess(partialRows),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestAddEntityTypeHandleReadiness(t *testing.T) {
	operation, err := newAddEntityTypeHandleOperation()
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		rows          []engine.QueryRow
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{
			name:          "source schema is ready",
			rows:          entityTypeSourceColumnRows(),
			expectedState: engine.ReadinessReady,
			expectedText:  "ready to be added",
		},
		{
			name:          "target schema is ready",
			rows:          entityTypeExpandedColumnRows(),
			expectedState: engine.ReadinessReady,
			expectedText:  "already present",
		},
		{
			name: "unexpected name column blocks migration",
			rows: []engine.QueryRow{
				entityTypeColumnRow("NAME", "YES"),
			},
			expectedState: engine.ReadinessBlocked,
			expectedText:  "NAME does not match",
			findingCount:  1,
		},
		{
			name: "unexpected handle column blocks migration",
			rows: []engine.QueryRow{
				entityTypeColumnRow("NAME", "NO"),
				entityTypeColumnRow("HANDLE", "NO"),
			},
			expectedState: engine.ReadinessBlocked,
			expectedText:  "handle column has an unexpected definition",
			findingCount:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := sqlOperation.CheckReadiness(
				context.Background(), entityTypeColumnAccess(test.rows), engine.PlanContext{})

			require.NoError(t, err)
			assert.Equal(t, test.expectedState, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, test.findingCount)
			if test.findingCount > 0 {
				assert.Equal(t, "unexpected_entity_type_column_expansion", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestAddEntityTypeHandleReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newAddEntityTypeHandleOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), authorizationPDPReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func entityTypeColumnAccess(rows []engine.QueryRow) authorizationPDPReadAccess {
	return authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
		queryEntityTypeMigrationColumns.GetID(): rows,
	}}
}

func entityTypeSourceColumnRows() []engine.QueryRow {
	return []engine.QueryRow{entityTypeColumnRow("NAME", "NO")}
}

func entityTypeExpandedColumnRows() []engine.QueryRow {
	return []engine.QueryRow{
		entityTypeColumnRow("NAME", "NO"),
		entityTypeColumnRow("HANDLE", "YES"),
	}
}

func entityTypeFinalColumnRows() []engine.QueryRow {
	return []engine.QueryRow{
		entityTypeColumnRow("NAME", "NO"),
		entityTypeColumnRow("HANDLE", "NO"),
	}
}

func entityTypeColumnRow(name string, nullable string) engine.QueryRow {
	return engine.QueryRow{
		"column_name": name, "is_nullable": nullable,
		"data_type": "character varying", "maximum_length": int64(100),
	}
}
