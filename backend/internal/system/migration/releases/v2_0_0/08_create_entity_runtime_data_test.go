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

type entityRuntimeMigrationReadAccess struct {
	rows map[engine.StoreID]map[string][]engine.QueryRow
	err  error
}

func (a entityRuntimeMigrationReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	storeRows, ok := a.rows[storeID]
	if !ok {
		return nil, errors.New("unexpected store")
	}
	rows, ok := storeRows[query.GetID()]
	if !ok {
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}

func (entityRuntimeMigrationReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestCreateEntityRuntimeDataOperation(t *testing.T) {
	operation, err := newCreateEntityRuntimeDataOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, createEntityRuntimeDataOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	assert.Contains(t, implementation.Statements[0].Query, `CREATE TABLE "ENTITY_RUNTIME_DATA"`)
	assert.Contains(t, implementation.Statements[0].Query, `PRIMARY KEY (DEPLOYMENT_ID, ENTITY_ID)`)
}

func TestCreateEntityRuntimeDataVerification(t *testing.T) {
	operation, err := newCreateEntityRuntimeDataOperation()
	require.NoError(t, err)

	source, err := operation.PreVerify(context.Background(), entityRuntimeSourceAccess(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityRuntimeTargetAccess(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalidAccess := entityRuntimeTargetAccess()
	invalidAccess.rows[engine.StoreIDRuntimePersistent][queryEntityRuntimeDataColumns.GetID()][2]["is_nullable"] = "YES"
	invalid, err := operation.PreVerify(context.Background(), invalidAccess, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
	assert.Contains(t, invalid.Summary, "ENTITY_RUNTIME_DATA.STATE")
}

func TestCreateEntityRuntimeDataReadiness(t *testing.T) {
	operation, err := newCreateEntityRuntimeDataOperation()
	require.NoError(t, err)
	check := operation.(*engine.SQLOperation)

	source, err := check.CheckReadiness(context.Background(), entityRuntimeSourceAccess(), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, source.State)

	target, err := check.CheckReadiness(context.Background(), entityRuntimeTargetAccess(), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, target.State)

	invalidAccess := entityRuntimeTargetAccess()
	invalidAccess.rows[engine.StoreIDRuntimePersistent][queryEntityRuntimeDataChecks.GetID()] = nil
	invalid, err := check.CheckReadiness(context.Background(), invalidAccess, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, invalid.State)
	require.Len(t, invalid.Findings, 1)
	assert.Equal(t, "unexpected_entity_runtime_data_schema", invalid.Findings[0].Code)
}

func entityRuntimeSourceAccess() entityRuntimeMigrationReadAccess {
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDRuntimePersistent: {
			queryEntityRuntimeDataTable.GetID(): nil,
		},
	}}
}

func entityRuntimeTargetAccess() entityRuntimeMigrationReadAccess {
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDRuntimePersistent: entityRuntimeTargetRows(),
	}}
}

func entityRuntimeTargetRows() map[string][]engine.QueryRow {
	return map[string][]engine.QueryRow{
		queryEntityRuntimeDataTable.GetID(): {{"relation_kind": "r"}},
		queryEntityRuntimeDataColumns.GetID(): {
			{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO", "data_type": "character varying",
				"maximum_length": "255", "column_default": "none"},
			{"column_name": "ENTITY_ID", "is_nullable": "NO", "data_type": "character varying",
				"maximum_length": "36", "column_default": "none"},
			{"column_name": "STATE", "is_nullable": "NO", "data_type": "character varying",
				"maximum_length": "50", "column_default": "none"},
			{"column_name": "RUNTIME_ATTRIBUTES", "is_nullable": "NO", "data_type": "jsonb",
				"maximum_length": "none", "column_default": "'{}'::jsonb"},
			{"column_name": "REVISION", "is_nullable": "NO", "data_type": "bigint",
				"maximum_length": "none", "column_default": "0"},
			{"column_name": "CREATED_AT", "is_nullable": "NO", "data_type": "timestamp with time zone",
				"maximum_length": "none", "column_default": "none"},
			{"column_name": "UPDATED_AT", "is_nullable": "NO", "data_type": "timestamp with time zone",
				"maximum_length": "none", "column_default": "none"},
		},
		queryEntityRuntimeDataPrimaryKey.GetID(): {{"column_names": "DEPLOYMENT_ID,ENTITY_ID"}},
		queryEntityRuntimeDataChecks.GetID(): {{
			"definition": "CHECK ((jsonb_typeof(runtime_attributes) = 'object'::text))",
		}},
	}
}
