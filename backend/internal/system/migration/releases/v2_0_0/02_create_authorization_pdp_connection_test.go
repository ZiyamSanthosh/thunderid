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

func TestCreateAuthorizationPDPConnectionOperationDefinition(t *testing.T) {
	operation, err := newCreateAuthorizationPDPConnectionOperation()

	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/02_create_authorization_pdp_connection"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[0].Query, `CREATE TABLE "AUTHORIZATION_PDP_CONNECTION"`)
	assert.Contains(t, implementation.Statements[1].Query, "CREATE UNIQUE INDEX")
	assert.Contains(t, implementation.Statements[1].Query, authorizationPDPConnectionIndex)

	targets, err := operation.ResolveTargets(
		context.Background(), authorizationPDPTargetAccess(),
		engine.MigrationSelection{DeploymentIDs: []string{"deployment-a", "deployment-b"}})
	require.NoError(t, err)
	require.Equal(t, []engine.ExecutionTarget{{}}, targets)
}

func TestCreateAuthorizationPDPConnectionVerification(t *testing.T) {
	operation, err := newCreateAuthorizationPDPConnectionOperation()
	require.NoError(t, err)
	target := engine.ExecutionTarget{}

	source, err := operation.PreVerify(context.Background(), authorizationPDPReadAccess{
		rows: map[string][]engine.QueryRow{
			postgresQueryAuthorizationPDPConnectionTable.GetID():     {},
			postgresQueryAuthorizationPDPConnectionIndexName.GetID(): {},
		},
	}, target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	targetResult, err := operation.PostVerify(context.Background(), authorizationPDPTargetAccess(), target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, targetResult.State)

	invalidRows := authorizationPDPTargetRows()
	invalidRows[postgresQueryAuthorizationPDPConnectionPrimaryKey.GetID()] = []engine.QueryRow{{
		"column_name": "deployment_id", "ordinal_position": int64(1),
	}}
	invalid, err := operation.PreVerify(context.Background(), authorizationPDPReadAccess{rows: invalidRows}, target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestCreateAuthorizationPDPConnectionReadiness(t *testing.T) {
	operation, err := newCreateAuthorizationPDPConnectionOperation()
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
			name: "absent table is ready",
			access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
				postgresQueryAuthorizationPDPConnectionTable.GetID():     {},
				postgresQueryAuthorizationPDPConnectionIndexName.GetID(): {},
			}},
			expectedState: engine.ReadinessReady,
			expectedText:  "ready to be created",
		},
		{
			name:          "target table is ready",
			access:        authorizationPDPTargetAccess(),
			expectedState: engine.ReadinessReady,
			expectedText:  "already matches the target schema",
		},
		{
			name: "partial table blocks migration",
			access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
				postgresQueryAuthorizationPDPConnectionTable.GetID():   {{"relation_kind": "r"}},
				postgresQueryAuthorizationPDPConnectionColumns.GetID(): {},
			}},
			expectedState: engine.ReadinessBlocked,
			expectedText:  "expected 8 AUTHORIZATION_PDP_CONNECTION columns",
			findingCount:  1,
		},
		{
			name: "conflicting table relation blocks migration",
			access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
				postgresQueryAuthorizationPDPConnectionTable.GetID(): {{"relation_kind": "v"}},
			}},
			expectedState: engine.ReadinessBlocked,
			expectedText:  "is not an ordinary table",
			findingCount:  1,
		},
		{
			name: "conflicting index name blocks migration",
			access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
				postgresQueryAuthorizationPDPConnectionTable.GetID():     {},
				postgresQueryAuthorizationPDPConnectionIndexName.GetID(): {{"relation_kind": "i"}},
			}},
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
				assert.Equal(t, "unexpected_authorization_pdp_connection_schema", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestCreateAuthorizationPDPConnectionReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newCreateAuthorizationPDPConnectionOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), authorizationPDPReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func TestCreateAuthorizationPDPConnectionPropagatesInspectionFailure(t *testing.T) {
	operation, err := newCreateAuthorizationPDPConnectionOperation()
	require.NoError(t, err)

	result, err := operation.PreVerify(context.Background(), authorizationPDPReadAccess{
		err: errors.New("database unavailable"),
	}, engine.ExecutionTarget{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func authorizationPDPTargetAccess() authorizationPDPReadAccess {
	return authorizationPDPReadAccess{rows: authorizationPDPTargetRows()}
}

func authorizationPDPTargetRows() map[string][]engine.QueryRow {
	return map[string][]engine.QueryRow{
		postgresQueryAuthorizationPDPConnectionTable.GetID(): {{
			"relation_kind": "r",
		}},
		postgresQueryAuthorizationPDPConnectionColumns.GetID():    authorizationPDPColumnRows(),
		postgresQueryAuthorizationPDPConnectionPrimaryKey.GetID(): authorizationPDPPrimaryKeyRows(),
		postgresQueryAuthorizationPDPConnectionIndex.GetID():      authorizationPDPIndexRows(),
	}
}

func authorizationPDPColumnRows() []engine.QueryRow {
	rows := make([]engine.QueryRow, 0, len(authorizationPDPConnectionColumns))
	for _, column := range authorizationPDPConnectionColumns {
		rows = append(rows, engine.QueryRow{
			"column_name": column.name, "is_nullable": column.nullable, "data_type": column.dataType,
			"maximum_length": column.maximumLength, "has_default": column.hasDefault,
		})
	}
	return rows
}

func authorizationPDPPrimaryKeyRows() []engine.QueryRow {
	return []engine.QueryRow{{"column_name": "id", "ordinal_position": int64(1)}}
}

func authorizationPDPIndexRows() []engine.QueryRow {
	return []engine.QueryRow{
		{"is_unique": "true", "column_name": "deployment_id", "column_position": int64(1)},
		{"is_unique": "true", "column_name": "type", "column_position": int64(2)},
		{"is_unique": "true", "column_name": "name", "column_position": int64(3)},
	}
}
