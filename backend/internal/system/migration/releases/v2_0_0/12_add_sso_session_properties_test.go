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

func TestAddSSOSessionPropertiesOperation(t *testing.T) {
	operation, err := newAddSSOSessionPropertiesOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/runtime_persistent/12_add_sso_session_properties"), definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `ADD COLUMN PROPERTIES JSONB`)

	source, err := operation.PreVerify(context.Background(), ssoSessionPropertiesAccess(nil), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(),
		ssoSessionPropertiesAccess([]engine.QueryRow{ssoSessionPropertiesColumnRow("YES", "jsonb", "no")}),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(),
		ssoSessionPropertiesAccess([]engine.QueryRow{ssoSessionPropertiesColumnRow("NO", "jsonb", "no")}),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestAddSSOSessionPropertiesReadiness(t *testing.T) {
	operation, err := newAddSSOSessionPropertiesOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		access        entityRuntimeMigrationReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{name: "source schema", access: ssoSessionPropertiesAccess(nil),
			expectedState: engine.ReadinessReady, expectedText: "ready to be added"},
		{name: "target schema", access: ssoSessionPropertiesAccess(
			[]engine.QueryRow{ssoSessionPropertiesColumnRow("YES", "jsonb", "no")}),
			expectedState: engine.ReadinessReady, expectedText: "already present"},
		{name: "missing table", access: entityRuntimeMigrationReadAccess{
			rows: map[engine.StoreID]map[string][]engine.QueryRow{
				engine.StoreIDRuntimePersistent: {querySSOSessionTable.GetID(): {}},
			}}, expectedState: engine.ReadinessBlocked, expectedText: "found 0", findingCount: 1},
		{name: "wrong primary key", access: ssoSessionPropertiesAccessWithPrimaryKey(nil, "DEPLOYMENT_ID,SESSION_ID"),
			expectedState: engine.ReadinessBlocked, expectedText: "primary key", findingCount: 1},
		{name: "wrong column type", access: ssoSessionPropertiesAccess(
			[]engine.QueryRow{ssoSessionPropertiesColumnRow("YES", "text", "no")}),
			expectedState: engine.ReadinessBlocked, expectedText: "definition", findingCount: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := checker.CheckReadiness(context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			assert.Equal(t, test.expectedState, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, test.findingCount)
		})
	}
}

func TestAddSSOSessionPropertiesReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newAddSSOSessionPropertiesOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		entityRuntimeMigrationReadAccess{err: errors.New("database unavailable")}, engine.PlanContext{})
	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func ssoSessionPropertiesAccess(columnRows []engine.QueryRow) entityRuntimeMigrationReadAccess {
	return ssoSessionPropertiesAccessWithPrimaryKey(columnRows, "SESSION_ID,DEPLOYMENT_ID")
}

func ssoSessionPropertiesAccessWithPrimaryKey(
	columnRows []engine.QueryRow,
	primaryKey string,
) entityRuntimeMigrationReadAccess {
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDRuntimePersistent: {
			querySSOSessionTable.GetID():            {{"relation_kind": "r"}},
			querySSOSessionPrimaryKey.GetID():       {{"column_names": primaryKey}},
			querySSOSessionPropertiesColumn.GetID(): columnRows,
		},
	}}
}

func ssoSessionPropertiesColumnRow(nullable string, dataType string, hasDefault string) engine.QueryRow {
	return engine.QueryRow{"is_nullable": nullable, "data_type": dataType, "has_default": hasDefault}
}
