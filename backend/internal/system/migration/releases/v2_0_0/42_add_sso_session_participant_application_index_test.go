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

type ssoParticipantIndexAccess struct {
	rows map[string][]engine.QueryRow
	err  error
}

func (a *ssoParticipantIndexAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	if storeID != engine.StoreIDRuntimePersistent {
		return nil, errors.New("unexpected store")
	}
	rows, ok := a.rows[query.GetID()]
	if !ok {
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}

func (*ssoParticipantIndexAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestAddSSOSessionParticipantApplicationIndexOperation(t *testing.T) {
	operation, err := newAddSSOSessionParticipantApplicationIndexOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID(
		"2.0.0/runtime_persistent/42_add_sso_session_participant_application_index"), definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)
	onlineOperation := operation.(*ssoSessionParticipantApplicationIndexOperation)
	assert.Contains(t, onlineOperation.createIndexQuery.PostgresQuery, "CREATE INDEX CONCURRENTLY")
	assert.Contains(t, onlineOperation.dropIndexQuery.PostgresQuery, "DROP INDEX CONCURRENTLY")

	source, err := operation.PreVerify(context.Background(),
		ssoParticipantApplicationIndexAccess(nil), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(),
		ssoParticipantApplicationIndexAccess(ssoParticipantApplicationIndexRow(false, true, true, false, "btree",
			ssoSessionParticipantApplicationIndexColumns)), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestAddSSOSessionParticipantApplicationIndexReadiness(t *testing.T) {
	operation, err := newAddSSOSessionParticipantApplicationIndexOperation()
	require.NoError(t, err)
	checker := operation.(*ssoSessionParticipantApplicationIndexOperation)

	tests := []struct {
		name          string
		access        *ssoParticipantIndexAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{name: "index absent", access: ssoParticipantApplicationIndexAccess(nil),
			expectedState: engine.ReadinessReady, expectedText: "ready to be created"},
		{name: "index valid", access: ssoParticipantApplicationIndexAccess(
			ssoParticipantApplicationIndexRow(false, true, true, false, "btree",
				ssoSessionParticipantApplicationIndexColumns)),
			expectedState: engine.ReadinessReady, expectedText: "present and valid"},
		{name: "interrupted index", access: ssoParticipantApplicationIndexAccess(
			ssoParticipantApplicationIndexRow(false, false, false, false, "btree",
				ssoSessionParticipantApplicationIndexColumns)),
			expectedState: engine.ReadinessReady, expectedText: "ready to be rebuilt"},
		{name: "wrong index columns", access: ssoParticipantApplicationIndexAccess(
			ssoParticipantApplicationIndexRow(false, true, true, false, "btree", "APP_ID,DEPLOYMENT_ID")),
			expectedState: engine.ReadinessBlocked, expectedText: "Unexpected", findingCount: 1},
		{name: "partial index", access: ssoParticipantApplicationIndexAccess(
			ssoParticipantApplicationIndexRow(false, true, true, true, "btree",
				ssoSessionParticipantApplicationIndexColumns)),
			expectedState: engine.ReadinessBlocked, expectedText: "Unexpected", findingCount: 1},
		{name: "missing table columns", access: &ssoParticipantIndexAccess{rows: map[string][]engine.QueryRow{
			querySSOSessionParticipantApplicationColumns.GetID(): {},
			querySSOSessionParticipantApplicationIndex.GetID():   {},
		}}, expectedState: engine.ReadinessBlocked, expectedText: "found 0", findingCount: 1},
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

func TestAddSSOSessionParticipantApplicationIndexReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newAddSSOSessionParticipantApplicationIndexOperation()
	require.NoError(t, err)

	result, err := operation.(*ssoSessionParticipantApplicationIndexOperation).CheckReadiness(
		context.Background(), &ssoParticipantIndexAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})
	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func ssoParticipantApplicationIndexAccess(indexRows []engine.QueryRow) *ssoParticipantIndexAccess {
	return &ssoParticipantIndexAccess{rows: map[string][]engine.QueryRow{
		querySSOSessionParticipantApplicationColumns.GetID(): {
			{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO"},
			{"column_name": "APP_ID", "is_nullable": "NO"},
		},
		querySSOSessionParticipantApplicationIndex.GetID(): indexRows,
	}}
}

func ssoParticipantApplicationIndexRow(
	unique bool,
	valid bool,
	ready bool,
	hasPredicate bool,
	accessMethod string,
	columns string,
) []engine.QueryRow {
	return []engine.QueryRow{{
		"is_unique": unique, "is_valid": valid, "is_ready": ready,
		"has_predicate": hasPredicate, "access_method": accessMethod, "column_names": columns,
	}}
}
