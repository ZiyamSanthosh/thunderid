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

type authorizationPDPReadAccess struct {
	rows map[string][]engine.QueryRow
	err  error
}

func (a authorizationPDPReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != engine.StoreIDConfigDB {
		return nil, errors.New("unexpected store")
	}
	if a.err != nil {
		return nil, a.err
	}
	rows, exists := a.rows[query.GetID()]
	if !exists {
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}

func (authorizationPDPReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

type configTableReadAccess struct {
	table            configTableSpec
	absent           bool
	invalidType      bool
	presentRelations map[string]bool
	err              error
}

func (a configTableReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != engine.StoreIDConfigDB {
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
	case queryConfigTableRelation.GetID():
		if name != a.table.name {
			if a.presentRelations[name] {
				return []engine.QueryRow{{"relation_kind": "r"}}, nil
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
	case queryConfigTableColumns.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.columns))
		for _, column := range a.table.columns {
			rows = append(rows, engine.QueryRow{
				"column_name": column.name, "is_nullable": column.nullable, "data_type": column.dataType,
				"maximum_length": column.maximumLength, "column_default": column.defaultValue,
			})
		}
		return rows, nil
	case queryConfigTableKeys.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.keys))
		for _, key := range a.table.keys {
			rows = append(rows, engine.QueryRow{
				"constraint_type": key.typeCode, "column_names": key.columns,
				"referenced_table": key.referencedTable, "referenced_columns": key.referencedColumns,
				"delete_action": key.deleteAction,
			})
		}
		return rows, nil
	case queryConfigTableChecks.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.checks))
		for _, check := range a.table.checks {
			rows = append(rows, engine.QueryRow{"definition": strings.Join(check.requiredFragments, " ")})
		}
		return rows, nil
	case queryConfigTableIndexes.GetID():
		rows := make([]engine.QueryRow, 0, len(a.table.indexes))
		for _, index := range a.table.indexes {
			rows = append(rows, engine.QueryRow{
				"index_name": index.name, "is_unique": index.unique, "column_names": index.columns,
				"predicate": strings.Join(index.predicateFragments, " "),
			})
		}
		return rows, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (configTableReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func assertConfigTableOperation(
	t *testing.T,
	constructor func() (engine.Operation, error),
	id engine.OperationID,
	table configTableSpec,
	statementCount int,
) {
	t.Helper()
	operation, err := constructor()
	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, id, definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Len(t, implementation.Statements, statementCount)

	source, err := operation.PreVerify(context.Background(),
		configTableReadAccess{table: table, absent: true}, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	targetAccess := configTableReadAccess{table: table}
	target, err := operation.PostVerify(context.Background(), targetAccess, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State, target.Summary)

	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), targetAccess, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, readiness.State, readiness.Summary)
}

func TestConfigTableOperationRejectsConflictingRelation(t *testing.T) {
	operation, err := newCreateVariableOperation()
	require.NoError(t, err)
	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		configTableReadAccess{table: variableTable, invalidType: true}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
	assert.Len(t, readiness.Findings, 1)
}

func TestConfigTableOperationPropagatesInspectionErrors(t *testing.T) {
	operation, err := newCreateGatewayOperation()
	require.NoError(t, err)
	_, err = operation.PreVerify(context.Background(), configTableReadAccess{
		table: gatewayTable, err: errors.New("database unavailable"),
	}, engine.ExecutionTarget{})
	require.ErrorContains(t, err, "database unavailable")
}

func TestConfigTableOperationDefersUntilDependency(t *testing.T) {
	operation, err := newCreateResourceSharingPolicyUnitOperation()
	require.NoError(t, err)

	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		configTableReadAccess{table: resourceSharingPolicyUnitTable, absent: true}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, readiness.State)
	assert.Equal(t, []engine.OperationID{"2.0.0/configdb/19_create_resource_sharing_policy"},
		readiness.DeferredUntil)

	readiness, err = operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		configTableReadAccess{
			table: resourceSharingPolicyUnitTable, absent: true,
			presentRelations: map[string]bool{"RESOURCE_SHARING_POLICY": true},
		}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, readiness.State)
}
