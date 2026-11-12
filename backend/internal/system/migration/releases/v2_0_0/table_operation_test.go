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

// tableReadAccess reports a table either absent or exactly as its spec describes, with optional deviations.
type tableReadAccess struct {
	table            tableSpec
	absent           bool
	invalidType      bool
	columnOverride   map[int]columnSpec
	presentRelations map[string]bool
	err              error
}

func (a tableReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != a.table.storeID {
		return nil, errors.New("unexpected store")
	}
	if a.err != nil {
		return nil, a.err
	}
	if len(args) != 1 {
		return nil, errors.New("expected relation argument")
	}
	name, ok := args[0].(string)
	if !ok {
		return nil, errors.New("expected string relation argument")
	}

	switch query.GetID() {
	case queryTableRelation.GetID():
		if name != a.table.name {
			if a.presentRelations[name] {
				return []engine.QueryRow{{"relation_kind": "i"}}, nil
			}
			return []engine.QueryRow{}, nil
		}
		if a.absent {
			return []engine.QueryRow{}, nil
		}
		kind := "r"
		if a.invalidType {
			kind = "v"
		}
		return []engine.QueryRow{{"relation_kind": kind}}, nil
	case queryTableColumns.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.columns))
		for index, column := range a.table.columns {
			if override, exists := a.columnOverride[index]; exists {
				column = override
			}
			rows = append(rows, engine.QueryRow{
				"column_name": column.name, "is_nullable": column.nullable, "data_type": column.dataType,
				"maximum_length": column.maximumLength, "column_default": column.defaultValue,
			})
		}
		return rows, nil
	case queryTableKeys.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.keys))
		for _, key := range a.table.keys {
			rows = append(rows, engine.QueryRow{"constraint_type": key.typeCode, "column_names": key.columns})
		}
		return rows, nil
	case queryTableChecks.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.checks))
		for _, check := range a.table.checks {
			rows = append(rows, engine.QueryRow{"definition": strings.Join(check.requiredFragments, " ")})
		}
		return rows, nil
	case queryTableIndexes.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.indexes))
		for _, index := range a.table.indexes {
			rows = append(rows, engine.QueryRow{
				"index_name": index.name, "is_unique": index.unique, "column_names": index.columns,
			})
		}
		return rows, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (tableReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

//nolint:dupl // Mirrors the configuration table helper test; the two helpers describe different table specs.
func assertCreateTableOperation(
	t *testing.T,
	constructor func() (engine.Operation, error),
	id engine.OperationID,
	table tableSpec,
	statementCount int,
) {
	t.Helper()
	operation, err := constructor()
	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, id, definition.ID)
	assert.Equal(t, table.storeID, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Len(t, implementation.Statements, statementCount)

	source, err := operation.PreVerify(context.Background(),
		tableReadAccess{table: table, absent: true}, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	targetAccess := tableReadAccess{table: table}
	target, err := operation.PostVerify(context.Background(), targetAccess, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State, target.Summary)

	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), targetAccess, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, readiness.State, readiness.Summary)
}

func TestCreateGatewayMasterOperation(t *testing.T) {
	assertCreateTableOperation(t, newCreateGatewayMasterOperation, createGatewayMasterOperationID,
		gatewayMasterTable, 1)
}

func TestCreateProjectTableOperation(t *testing.T) {
	assertCreateTableOperation(t, newCreateProjectTableOperation, createProjectTableOperationID,
		projectTable, 2)
}

func TestCreateUserShareMigrationTableOperation(t *testing.T) {
	assertCreateTableOperation(t, newCreateUserShareMigrationTableOperation,
		createUserShareMigrationTableOperationID, userShareMigrationTable, 2)
}

func TestCreateTableOperationRejectsConflictingRelation(t *testing.T) {
	operation, err := newCreateGatewayMasterOperation()
	require.NoError(t, err)
	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		tableReadAccess{table: gatewayMasterTable, invalidType: true}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
	assert.Len(t, readiness.Findings, 1)
}

func TestCreateTableOperationRejectsIndexNameInUse(t *testing.T) {
	operation, err := newCreateProjectTableOperation()
	require.NoError(t, err)
	verification, err := operation.PreVerify(context.Background(), tableReadAccess{
		table: projectTable, absent: true,
		presentRelations: map[string]bool{"idx_project_handle": true},
	}, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, verification.State)
	assert.Contains(t, verification.Summary, "idx_project_handle")
}

func TestCreateTableOperationRejectsDifferentColumn(t *testing.T) {
	operation, err := newCreateProjectTableOperation()
	require.NoError(t, err)
	// created_at without its default does not match the target table.
	column := projectTable.columns[5]
	column.defaultValue = "none"
	verification, err := operation.PostVerify(context.Background(), tableReadAccess{
		table: projectTable, columnOverride: map[int]columnSpec{5: column},
	}, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, verification.State)
	assert.Len(t, verification.Findings, 1)
}

func TestCreateTableOperationPropagatesInspectionErrors(t *testing.T) {
	operation, err := newCreateUserShareMigrationTableOperation()
	require.NoError(t, err)
	_, err = operation.PreVerify(context.Background(), tableReadAccess{
		table: userShareMigrationTable, err: errors.New("database unavailable"),
	}, engine.ExecutionTarget{})
	require.ErrorContains(t, err, "database unavailable")
}
