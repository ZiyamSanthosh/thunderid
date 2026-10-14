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

type entityMigrationReadAccess struct {
	rows []engine.QueryRow
	err  error
}

func (a entityMigrationReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	if storeID != engine.StoreIDEntityDB || query.GetID() != queryEntityTypeLength.GetID() {
		return nil, errors.New("unexpected query")
	}
	return a.rows, nil
}

func (entityMigrationReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandEntityTypeLengthOperation(t *testing.T) {
	operation, err := newExpandEntityTypeLengthOperation()
	require.NoError(t, err)

	assert.Equal(t, engine.StoreIDEntityDB, operation.Definition().StoreID)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `VARCHAR(100)`)

	source, err := operation.PreVerify(context.Background(), entityTypeLengthAccess(50), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeLengthAccess(100), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeLengthAccess(255), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestExpandEntityTypeLengthReadiness(t *testing.T) {
	operation, err := newExpandEntityTypeLengthOperation()
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		access        entityMigrationReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{
			name:          "source column is ready",
			access:        entityTypeLengthAccess(50),
			expectedState: engine.ReadinessReady,
			expectedText:  "ready to be expanded",
		},
		{
			name:          "target column is ready",
			access:        entityTypeLengthAccess(100),
			expectedState: engine.ReadinessReady,
			expectedText:  "already accepts",
		},
		{
			name:          "unexpected column blocks migration",
			access:        entityTypeLengthAccess(255),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "Unexpected ENTITY.TYPE maximum length",
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
				assert.Equal(t, "unexpected_entity_type_length", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestExpandEntityTypeLengthReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newExpandEntityTypeLengthOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), entityMigrationReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func entityTypeLengthAccess(length int64) entityMigrationReadAccess {
	return entityMigrationReadAccess{rows: []engine.QueryRow{{
		"is_nullable": "NO", "data_type": "character varying", "maximum_length": length,
	}}}
}
