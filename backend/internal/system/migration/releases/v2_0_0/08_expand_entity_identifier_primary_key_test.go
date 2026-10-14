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

type entityIdentifierPrimaryKeyReadAccess struct {
	rowsByQueryID map[string][]engine.QueryRow
	err           error
}

func (a entityIdentifierPrimaryKeyReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	if storeID != engine.StoreIDEntityDB {
		return nil, errors.New("unexpected store")
	}
	rows, ok := a.rowsByQueryID[query.GetID()]
	if !ok {
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}

func (entityIdentifierPrimaryKeyReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandEntityIdentifierPrimaryKeyOperation(t *testing.T) {
	operation, err := newExpandEntityIdentifierPrimaryKeyOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/entitydb/08_expand_entity_identifier_primary_key"), definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 3)
	assert.Contains(t, implementation.Statements[1].Query,
		`(ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)`)
	assert.Contains(t, implementation.Statements[2].Query, `PRIMARY KEY USING INDEX`)

	source, err := operation.PreVerify(
		context.Background(), entityIdentifierPrimaryKeyAccess(legacyEntityIdentifierPrimaryKeyColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(
		context.Background(), entityIdentifierPrimaryKeyAccess(targetEntityIdentifierPrimaryKeyColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestExpandEntityIdentifierPrimaryKeyReadiness(t *testing.T) {
	operation, err := newExpandEntityIdentifierPrimaryKeyOperation()
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		access        entityIdentifierPrimaryKeyReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{
			name:          "source primary key is ready",
			access:        entityIdentifierPrimaryKeyAccess(legacyEntityIdentifierPrimaryKeyColumns),
			expectedState: engine.ReadinessReady,
			expectedText:  "ready to include VALUE",
		},
		{
			name:          "target primary key is ready",
			access:        entityIdentifierPrimaryKeyAccess(targetEntityIdentifierPrimaryKeyColumns),
			expectedState: engine.ReadinessReady,
			expectedText:  "already includes VALUE",
		},
		{
			name:          "unexpected primary key blocks migration",
			access:        entityIdentifierPrimaryKeyAccess("DEPLOYMENT_ID,ENTITY_ID,NAME"),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "Unexpected ENTITY_IDENTIFIER primary key columns",
			findingCount:  1,
		},
		{
			name: "migration index name collision blocks migration",
			access: func() entityIdentifierPrimaryKeyReadAccess {
				access := entityIdentifierPrimaryKeyAccess(legacyEntityIdentifierPrimaryKeyColumns)
				access.rowsByQueryID[queryEntityIdentifierPrimaryKeyIndexName.GetID()] = []engine.QueryRow{{
					"relation_kind": "i",
				}}
				return access
			}(),
			expectedState: engine.ReadinessBlocked,
			expectedText:  "already in use",
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
				assert.Equal(t, "unexpected_entity_identifier_primary_key_schema", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestExpandEntityIdentifierPrimaryKeyRejectsUnexpectedSchema(t *testing.T) {
	operation, err := newExpandEntityIdentifierPrimaryKeyOperation()
	require.NoError(t, err)

	tests := []struct {
		name         string
		mutateAccess func(*entityIdentifierPrimaryKeyReadAccess)
		expectedText string
	}{
		{
			name: "missing table",
			mutateAccess: func(access *entityIdentifierPrimaryKeyReadAccess) {
				access.rowsByQueryID[queryEntityIdentifierTable.GetID()] = nil
			},
			expectedText: "Expected one ENTITY_IDENTIFIER table",
		},
		{
			name: "wrong relation kind",
			mutateAccess: func(access *entityIdentifierPrimaryKeyReadAccess) {
				access.rowsByQueryID[queryEntityIdentifierTable.GetID()] = []engine.QueryRow{{
					"relation_kind": "v",
				}}
			},
			expectedText: "not an ordinary table",
		},
		{
			name: "unexpected column definition",
			mutateAccess: func(access *entityIdentifierPrimaryKeyReadAccess) {
				access.rowsByQueryID[queryEntityIdentifierColumns.GetID()][3]["is_nullable"] = "YES"
			},
			expectedText: "unexpected ENTITY_IDENTIFIER.VALUE definition",
		},
		{
			name: "missing primary key",
			mutateAccess: func(access *entityIdentifierPrimaryKeyReadAccess) {
				access.rowsByQueryID[queryEntityIdentifierPrimaryKey.GetID()] = nil
			},
			expectedText: "Expected one ENTITY_IDENTIFIER primary key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := entityIdentifierPrimaryKeyAccess(legacyEntityIdentifierPrimaryKeyColumns)
			test.mutateAccess(&access)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})

			require.NoError(t, err)
			assert.Equal(t, engine.VerificationInvalid, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, 1)
		})
	}
}

func TestExpandEntityIdentifierPrimaryKeyReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newExpandEntityIdentifierPrimaryKeyOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), entityIdentifierPrimaryKeyReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func entityIdentifierPrimaryKeyAccess(primaryKeyColumns string) entityIdentifierPrimaryKeyReadAccess {
	return entityIdentifierPrimaryKeyReadAccess{rowsByQueryID: map[string][]engine.QueryRow{
		queryEntityIdentifierTable.GetID(): {{"relation_kind": "r"}},
		queryEntityIdentifierColumns.GetID(): {
			{
				"column_name": "DEPLOYMENT_ID", "is_nullable": "NO",
				"data_type": "character varying", "maximum_length": "255",
			},
			{
				"column_name": "ENTITY_ID", "is_nullable": "NO",
				"data_type": "character varying", "maximum_length": "36",
			},
			{
				"column_name": "NAME", "is_nullable": "NO",
				"data_type": "character varying", "maximum_length": "255",
			},
			{
				"column_name": "VALUE", "is_nullable": "NO",
				"data_type": "text", "maximum_length": "none",
			},
		},
		queryEntityIdentifierPrimaryKey.GetID(): {{
			"constraint_name": "ENTITY_IDENTIFIER_pkey", "column_names": primaryKeyColumns,
		}},
		queryEntityIdentifierPrimaryKeyIndexName.GetID(): nil,
	}}
}
