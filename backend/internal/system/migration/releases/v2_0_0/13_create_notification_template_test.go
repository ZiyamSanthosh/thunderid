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

func TestCreateNotificationTemplateOperation(t *testing.T) {
	operation, err := newCreateNotificationTemplateOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/13_create_notification_template"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	assert.Contains(t, implementation.Statements[0].Query, `CREATE TABLE "NOTIFICATION_TEMPLATE"`)
	assert.Contains(t, implementation.Statements[0].Query, `PRIMARY KEY (DEPLOYMENT_ID, ID)`)
	assert.Contains(t, implementation.Statements[0].Query, `UNIQUE (DEPLOYMENT_ID, CHANNEL, HANDLE)`)

	source, err := operation.PreVerify(
		context.Background(), notificationTemplateSourceAccess(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(
		context.Background(), notificationTemplateTargetAccess(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalidRows := notificationTemplateTargetRows()
	invalidRows[queryNotificationTemplateColumns.GetID()][7]["column_default"] = "none"
	invalid, err := operation.PreVerify(context.Background(),
		authorizationPDPReadAccess{rows: invalidRows}, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestCreateNotificationTemplateReadiness(t *testing.T) {
	operation, err := newCreateNotificationTemplateOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	tests := []struct {
		name          string
		access        authorizationPDPReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{name: "source schema", access: notificationTemplateSourceAccess(),
			expectedState: engine.ReadinessReady, expectedText: "ready to be created"},
		{name: "target schema", access: notificationTemplateTargetAccess(),
			expectedState: engine.ReadinessReady, expectedText: "already matches"},
		{name: "conflicting relation", access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
			queryNotificationTemplateTable.GetID(): {{"relation_kind": "v"}},
		}}, expectedState: engine.ReadinessBlocked, expectedText: "not an ordinary table", findingCount: 1},
		{name: "conflicting generated name", access: authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
			queryNotificationTemplateTable.GetID(): {},
			queryNotificationTemplateGeneratedNames.GetID(): {{
				"relation_name": notificationTemplatePrimaryKeyName,
			}},
		}}, expectedState: engine.ReadinessBlocked, expectedText: "already in use", findingCount: 1},
		{name: "wrong unique constraint", access: notificationTemplateAccessWithUnique("ID"),
			expectedState: engine.ReadinessBlocked, expectedText: "unique constraint", findingCount: 1},
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

func TestCreateNotificationTemplateReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newCreateNotificationTemplateOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(),
		authorizationPDPReadAccess{err: errors.New("database unavailable")}, engine.PlanContext{})
	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func notificationTemplateSourceAccess() authorizationPDPReadAccess {
	return authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
		queryNotificationTemplateTable.GetID():          {},
		queryNotificationTemplateGeneratedNames.GetID(): {},
	}}
}

func notificationTemplateTargetAccess() authorizationPDPReadAccess {
	return authorizationPDPReadAccess{rows: notificationTemplateTargetRows()}
}

func notificationTemplateAccessWithUnique(columns string) authorizationPDPReadAccess {
	rows := notificationTemplateTargetRows()
	rows[queryNotificationTemplateUniqueKeys.GetID()] = []engine.QueryRow{{"column_names": columns}}
	return authorizationPDPReadAccess{rows: rows}
}

func notificationTemplateTargetRows() map[string][]engine.QueryRow {
	return map[string][]engine.QueryRow{
		queryNotificationTemplateTable.GetID():      {{"relation_kind": "r"}},
		queryNotificationTemplateColumns.GetID():    notificationTemplateColumnRows(),
		queryNotificationTemplatePrimaryKey.GetID(): {{"column_names": "DEPLOYMENT_ID,ID"}},
		queryNotificationTemplateUniqueKeys.GetID(): {{"column_names": "DEPLOYMENT_ID,CHANNEL,HANDLE"}},
	}
}

func notificationTemplateColumnRows() []engine.QueryRow {
	rows := make([]engine.QueryRow, 0, len(notificationTemplateColumns))
	for _, column := range notificationTemplateColumns {
		rows = append(rows, engine.QueryRow{
			"column_name": column.name, "is_nullable": column.nullable, "data_type": column.dataType,
			"maximum_length": column.maximumLength, "column_default": column.defaultValue,
		})
	}
	return rows
}
