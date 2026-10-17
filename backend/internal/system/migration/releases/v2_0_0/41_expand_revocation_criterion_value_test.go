// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type revocationCriterionValueReadAccess struct {
	rows []engine.QueryRow
	err  error
}

func (a revocationCriterionValueReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	if storeID != engine.StoreIDRuntimePersistent ||
		query.GetID() != queryRevocationCriterionValueLength.GetID() {
		return nil, errors.New("unexpected query")
	}
	return a.rows, nil
}

func (revocationCriterionValueReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandRevocationCriterionValueOperation(t *testing.T) {
	operation, err := newExpandRevocationCriterionValueOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID(
		"2.0.0/runtime_persistent/41_expand_revocation_criterion_value"), definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query,
		`ALTER COLUMN CRITERION_VALUE TYPE VARCHAR(2048)`)

	source, err := operation.PreVerify(context.Background(),
		revocationCriterionValueLengthAccess(255), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(),
		revocationCriterionValueLengthAccess(2048), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestExpandRevocationCriterionValueReadiness(t *testing.T) {
	operation, err := newExpandRevocationCriterionValueOperation()
	require.NoError(t, err)

	tests := []struct {
		name          string
		access        revocationCriterionValueReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{name: "source column", access: revocationCriterionValueLengthAccess(255),
			expectedState: engine.ReadinessReady, expectedText: "ready to be expanded"},
		{name: "target column", access: revocationCriterionValueLengthAccess(2048),
			expectedState: engine.ReadinessReady, expectedText: "already accepts"},
		{name: "missing column", access: revocationCriterionValueReadAccess{},
			expectedState: engine.ReadinessBlocked, expectedText: "found 0", findingCount: 1},
		{name: "unexpected length", access: revocationCriterionValueLengthAccess(512),
			expectedState: engine.ReadinessBlocked, expectedText: "maximum length", findingCount: 1},
		{name: "nullable column", access: revocationCriterionValueReadAccess{rows: []engine.QueryRow{{
			"is_nullable": "YES", "data_type": "character varying", "maximum_length": int64(255),
		}}}, expectedState: engine.ReadinessBlocked, expectedText: "definition", findingCount: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.(*engine.SQLOperation).CheckReadiness(
				context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			assert.Equal(t, test.expectedState, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, test.findingCount)
		})
	}
}

func TestExpandRevocationCriterionValueReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newExpandRevocationCriterionValueOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		revocationCriterionValueReadAccess{err: errors.New("database unavailable")}, engine.PlanContext{})
	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func revocationCriterionValueLengthAccess(length int64) revocationCriterionValueReadAccess {
	return revocationCriterionValueReadAccess{rows: []engine.QueryRow{{
		"is_nullable": "NO", "data_type": "character varying", "maximum_length": length,
	}}}
}
