// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type primaryKeyReadAccess struct {
	storeID       engine.StoreID
	rowsByQueryID map[string][]engine.QueryRow
}

func (a primaryKeyReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != a.storeID {
		return nil, errors.New("unexpected store")
	}
	rows, exists := a.rowsByQueryID[query.GetID()]
	if !exists {
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}

func (primaryKeyReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandEntityTypesPrimaryKeyOperation(t *testing.T) {
	registered, err := newExpandEntityTypesPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/23_expand_entity_types_primary_key"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)
	assert.Contains(t, operation.createIndexQuery.PostgresQuery,
		`CREATE UNIQUE INDEX CONCURRENTLY "idx_entity_types_deployment_primary_key"`)
	assert.Contains(t, operation.createIndexQuery.PostgresQuery,
		`ON "ENTITY_TYPES" ("deployment_id", "id")`)
	assert.Contains(t, operation.finalizeQuery.PostgresQuery, "PRIMARY KEY USING INDEX")

	source, err := operation.PreVerify(context.Background(), entityTypesPrimaryKeyAccess("ID", nil),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(),
		entityTypesPrimaryKeyAccess("DEPLOYMENT_ID,ID", nil), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestExpandEntityTypesPrimaryKeyReadiness(t *testing.T) {
	registered, err := newExpandEntityTypesPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)

	invalidIndex := []engine.QueryRow{{
		"is_unique": true, "is_valid": false, "is_ready": false,
		"column_names": "DEPLOYMENT_ID,ID",
	}}
	tests := []struct {
		name     string
		access   primaryKeyReadAccess
		expected engine.ReadinessState
	}{
		{name: "legacy key", access: entityTypesPrimaryKeyAccess("ID", nil), expected: engine.ReadinessReady},
		{name: "interrupted concurrent index", access: entityTypesPrimaryKeyAccess("ID", invalidIndex),
			expected: engine.ReadinessReady},
		{name: "target key", access: entityTypesPrimaryKeyAccess("DEPLOYMENT_ID,ID", nil),
			expected: engine.ReadinessReady},
		{name: "unexpected key", access: entityTypesPrimaryKeyAccess("ID,DEPLOYMENT_ID", nil),
			expected: engine.ReadinessBlocked},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.CheckReadiness(context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
		})
	}
}

func TestEntityTypesPrimaryKeyContractOperation(t *testing.T) {
	expand, err := newExpandEntityTypesPrimaryKeyOperation()
	require.NoError(t, err)
	registered, err := newLegacyPrimaryKeyContractOperation(expand)
	require.NoError(t, err)
	operation := registered.(*legacyPrimaryKeyContractOperation)

	assert.Equal(t, engine.OperationID("2.0.0/configdb/23_finalize_entity_types_primary_key"),
		operation.Definition().ID)
	assert.Equal(t, engine.InterruptionModeOnline, operation.Definition().InterruptionMode)
	assert.Contains(t, operation.expand.dropLegacyIndexQuery.PostgresQuery,
		`DROP INDEX CONCURRENTLY "idx_entity_types_legacy_primary_key_unique"`)
	readiness, err := operation.CheckReadiness(context.Background(),
		entityTypesPrimaryKeyAccess("ID", nil), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, readiness.State)

	sourceAccess := entityTypesPrimaryKeyAccess("DEPLOYMENT_ID,ID", nil)
	source, err := operation.PreVerify(context.Background(), sourceAccess, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	targetAccess := entityTypesPrimaryKeyAccess("DEPLOYMENT_ID,ID", nil)
	targetAccess.rowsByQueryID["MIG-CONFIG-PK-23-09"] = nil
	target, err := operation.PostVerify(context.Background(), targetAccess, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func entityTypesPrimaryKeyAccess(
	primaryKeyColumns string,
	indexRows []engine.QueryRow,
) primaryKeyReadAccess {
	var legacyIndexRows []engine.QueryRow
	if primaryKeyColumns == "DEPLOYMENT_ID,ID" {
		legacyIndexRows = []engine.QueryRow{{
			"is_unique": true, "is_valid": true, "is_ready": true, "column_names": "ID",
		}}
	}
	return primaryKeyReadAccess{
		storeID: engine.StoreIDConfigDB,
		rowsByQueryID: map[string][]engine.QueryRow{
			"MIG-CONFIG-PK-23-01": {{"relation_kind": "r"}},
			"MIG-CONFIG-PK-23-02": {
				{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO"},
				{"column_name": "ID", "is_nullable": "NO"},
			},
			"MIG-CONFIG-PK-23-03": {{
				"constraint_name": "ENTITY_TYPES_pkey", "column_names": primaryKeyColumns,
			}},
			"MIG-CONFIG-PK-23-04": indexRows,
			"MIG-CONFIG-PK-23-09": legacyIndexRows,
		},
	}
}

func assertOnlinePrimaryKeyDefinition(
	t *testing.T,
	constructor func() (engine.Operation, error),
	id engine.OperationID,
	storeID engine.StoreID,
	table string,
	targetColumns string,
) {
	t.Helper()
	registered, err := constructor()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	assert.True(t, operation.preserveLegacyUniqueness)
	assert.Equal(t, id, operation.Definition().ID)
	assert.Equal(t, storeID, operation.Definition().StoreID)
	assert.Equal(t, engine.InterruptionModeOnline, operation.Definition().InterruptionMode)
	assert.Contains(t, operation.createIndexQuery.PostgresQuery, `CREATE UNIQUE INDEX CONCURRENTLY`)
	assert.Contains(t, operation.createIndexQuery.PostgresQuery,
		`ON "`+table+`" (`+strings.ToLower(targetColumns)+`)`)
	assert.Contains(t, operation.createLegacyIndexQuery.PostgresQuery,
		`CREATE UNIQUE INDEX CONCURRENTLY`)
	assert.Contains(t, operation.finalizeQuery.PostgresQuery, "PRIMARY KEY USING INDEX")
}
