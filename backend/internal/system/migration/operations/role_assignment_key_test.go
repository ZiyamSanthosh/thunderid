// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// roleAssignmentAccess simulates the ROLE_ASSIGNMENT catalog and records every statement executed against it.
type roleAssignmentAccess struct {
	relationKind  string
	columns       []engine.QueryRow
	primaryKey    string
	indexes       map[string]engine.QueryRow
	err           error
	executed      []string
	executedQuery []string
}

func newRoleAssignmentAccess() *roleAssignmentAccess {
	return &roleAssignmentAccess{
		relationKind: "r",
		columns:      roleAssignmentColumns("NO", "character varying", "36", "''::character varying"),
		primaryKey:   roleAssignmentLegacyPrimaryKey,
		indexes:      map[string]engine.QueryRow{},
	}
}

func (a *roleAssignmentAccess) Query(
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
	switch query.GetID() {
	case queryTableRelation.GetID():
		if a.relationKind == "" {
			return []engine.QueryRow{}, nil
		}
		return []engine.QueryRow{{"relation_kind": a.relationKind}}, nil
	case queryTableColumns.GetID():
		return a.columns, nil
	case queryRoleAssignmentPrimaryKey.GetID():
		return []engine.QueryRow{{"constraint_name": "ROLE_ASSIGNMENT_pkey", "column_names": a.primaryKey}}, nil
	case queryRoleAssignmentIndex.GetID():
		name, _ := args[0].(string)
		if row, exists := a.indexes[name]; exists {
			return []engine.QueryRow{row}, nil
		}
		return []engine.QueryRow{}, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (*roleAssignmentAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func (a *roleAssignmentAccess) Execute(
	_ context.Context,
	_ engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) (int64, error) {
	a.executed = append(a.executed, query.GetID())
	a.executedQuery = append(a.executedQuery, query.PostgresQuery)
	return 0, nil
}

// roleAssignmentColumns reports the two columns the release adds, the second with the given definition.
func roleAssignmentColumns(nullable, dataType, length, defaultValue string) []engine.QueryRow {
	return []engine.QueryRow{
		{
			"column_name": "assignment_ou_id", "is_nullable": "NO", "data_type": "character varying",
			"maximum_length": "36", "column_default": "''::character varying",
		},
		{
			"column_name": "sharing_policy_id", "is_nullable": nullable, "data_type": dataType,
			"maximum_length": length, "column_default": defaultValue,
		},
	}
}

func roleAssignmentIndexRow(unique, valid bool, columns string) engine.QueryRow {
	return engine.QueryRow{
		"is_unique": unique, "is_valid": valid, "is_ready": valid, "has_predicate": false, "column_names": columns,
	}
}

func TestAddRoleAssignmentSharingPolicyIDOperation(t *testing.T) {
	operation, err := NewAddRoleAssignmentSharingPolicyIDOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, addRoleAssignmentSharingPolicyIDOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `ASSIGNMENT_OU_ID VARCHAR(36) NOT NULL DEFAULT ''`)
	assert.Contains(t, implementation.Statements[1].Query, `SHARING_POLICY_ID VARCHAR(36) NOT NULL DEFAULT ''`)

	tests := []struct {
		name   string
		mutate func(*roleAssignmentAccess)
		state  engine.VerificationState
	}{
		{name: "column absent", mutate: func(a *roleAssignmentAccess) { a.columns = nil },
			state: engine.VerificationSource},
		{name: "column present", mutate: func(*roleAssignmentAccess) {}, state: engine.VerificationTarget},
		{name: "column nullable", mutate: func(a *roleAssignmentAccess) {
			a.columns = roleAssignmentColumns("YES", "character varying", "36", "''::character varying")
		}, state: engine.VerificationInvalid},
		{name: "column without default", mutate: func(a *roleAssignmentAccess) {
			a.columns = roleAssignmentColumns("NO", "character varying", "36", "none")
		}, state: engine.VerificationInvalid},
		{name: "table missing", mutate: func(a *roleAssignmentAccess) { a.relationKind = "" },
			state: engine.VerificationInvalid},
		{name: "relation is a view", mutate: func(a *roleAssignmentAccess) { a.relationKind = "v" },
			state: engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newRoleAssignmentAccess()
			test.mutate(access)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)

			readiness, err := operation.(*engine.SQLOperation).CheckReadiness(
				context.Background(), access, engine.PlanContext{})
			require.NoError(t, err)
			if test.state == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
				assert.Len(t, readiness.Findings, 1)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}
}

func TestWidenRoleAssignmentPrimaryKeyDefinition(t *testing.T) {
	operation := NewWidenRoleAssignmentPrimaryKeyOperation()
	definition := operation.Definition()
	assert.Equal(t, widenRoleAssignmentPrimaryKeyOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)
	_, isOnline := operation.(engine.OnlineOperation)
	assert.True(t, isOnline)
}

func TestWidenRoleAssignmentPrimaryKeyVerification(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*roleAssignmentAccess)
		state   engine.VerificationState
		summary string
	}{
		{name: "legacy key", mutate: func(*roleAssignmentAccess) {},
			state: engine.VerificationSource, summary: "ready to include"},
		{name: "widened key without sharing policy index", mutate: func(a *roleAssignmentAccess) {
			a.primaryKey = roleAssignmentTargetPrimaryKey
		}, state: engine.VerificationSource, summary: "sharing policy index is ready"},
		{name: "widened key with sharing policy index", mutate: func(a *roleAssignmentAccess) {
			a.primaryKey = roleAssignmentTargetPrimaryKey
			a.indexes[roleAssignmentSharingPolicyIndex] = roleAssignmentIndexRow(false, true,
				roleAssignmentSharingPolicyColumn)
		}, state: engine.VerificationTarget, summary: "includes SHARING_POLICY_ID"},
		{name: "unexpected key", mutate: func(a *roleAssignmentAccess) {
			a.primaryKey = "ROLE_ID,DEPLOYMENT_ID"
		}, state: engine.VerificationInvalid, summary: "Unexpected ROLE_ASSIGNMENT primary key"},
		{name: "leftover build index after widening", mutate: func(a *roleAssignmentAccess) {
			a.primaryKey = roleAssignmentTargetPrimaryKey
			a.indexes[roleAssignmentPrimaryKeyIndex] = roleAssignmentIndexRow(true, true,
				roleAssignmentTargetPrimaryKey)
		}, state: engine.VerificationInvalid, summary: "remains"},
		{name: "conflicting sharing policy index", mutate: func(a *roleAssignmentAccess) {
			a.indexes[roleAssignmentSharingPolicyIndex] = roleAssignmentIndexRow(true, true,
				roleAssignmentSharingPolicyColumn)
		}, state: engine.VerificationInvalid, summary: "Unexpected idx_role_assignment_sharing_policy"},
		{name: "invalid sharing policy column", mutate: func(a *roleAssignmentAccess) {
			a.columns = roleAssignmentColumns("YES", "text", "none", "none")
		}, state: engine.VerificationInvalid, summary: "sharing_policy_id definition"},
	}
	operation := NewWidenRoleAssignmentPrimaryKeyOperation()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newRoleAssignmentAccess()
			test.mutate(access)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State)
			assert.Contains(t, result.Summary, test.summary)
		})
	}
}

func TestWidenRoleAssignmentPrimaryKeyReadiness(t *testing.T) {
	operation := NewWidenRoleAssignmentPrimaryKeyOperation()
	checker := operation.(engine.ReadinessChecker)

	access := newRoleAssignmentAccess()
	access.columns = nil
	readiness, err := checker.CheckReadiness(context.Background(), access, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, readiness.State)
	assert.Equal(t, []engine.OperationID{addRoleAssignmentSharingPolicyIDOperationID}, readiness.DeferredUntil)

	readiness, err = checker.CheckReadiness(context.Background(), newRoleAssignmentAccess(), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, readiness.State)

	access = newRoleAssignmentAccess()
	access.primaryKey = "ROLE_ID"
	readiness, err = checker.CheckReadiness(context.Background(), access, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
	assert.Len(t, readiness.Findings, 1)
}

func TestBuildRoleAssignmentIndexes(t *testing.T) {
	t.Run("creates both indexes", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		require.NoError(t, buildRoleAssignmentIndexes(context.Background(), access))
		assert.Equal(t, []string{
			roleAssignmentPrimaryKeyIndexSpec.create.GetID(), roleAssignmentSharingPolicyIndexSpec.create.GetID(),
		}, access.executed)
	})
	t.Run("rebuilds an interrupted index and keeps a valid one", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.indexes[roleAssignmentPrimaryKeyIndex] = roleAssignmentIndexRow(true, false,
			roleAssignmentTargetPrimaryKey)
		access.indexes[roleAssignmentSharingPolicyIndex] = roleAssignmentIndexRow(false, true,
			roleAssignmentSharingPolicyColumn)
		require.NoError(t, buildRoleAssignmentIndexes(context.Background(), access))
		assert.Equal(t, []string{
			roleAssignmentPrimaryKeyIndexSpec.drop.GetID(), roleAssignmentPrimaryKeyIndexSpec.create.GetID(),
		}, access.executed)
	})
	t.Run("builds only the sharing policy index once the key is widened", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.primaryKey = roleAssignmentTargetPrimaryKey
		require.NoError(t, buildRoleAssignmentIndexes(context.Background(), access))
		assert.Equal(t, []string{roleAssignmentSharingPolicyIndexSpec.create.GetID()}, access.executed)
	})
	t.Run("refuses an invalid schema", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.primaryKey = "ROLE_ID"
		require.ErrorContains(t, buildRoleAssignmentIndexes(context.Background(), access), "Unexpected")
		assert.Empty(t, access.executed)
	})
}

func TestSwapRoleAssignmentPrimaryKey(t *testing.T) {
	t.Run("swaps the legacy key onto the prebuilt index", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.indexes[roleAssignmentPrimaryKeyIndex] = roleAssignmentIndexRow(true, true,
			roleAssignmentTargetPrimaryKey)
		require.NoError(t, swapRoleAssignmentPrimaryKey(context.Background(), access))
		require.Len(t, access.executedQuery, 2)
		assert.Equal(t, queryRoleAssignmentLockTimeout.GetID(), access.executed[0])
		assert.Contains(t, access.executedQuery[1], `DROP CONSTRAINT "ROLE_ASSIGNMENT_pkey"`)
		assert.Contains(t, access.executedQuery[1],
			`ADD CONSTRAINT "ROLE_ASSIGNMENT_pkey" PRIMARY KEY USING INDEX "idx_role_assignment_pk_new"`)
	})
	t.Run("does nothing once the key is widened", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.primaryKey = roleAssignmentTargetPrimaryKey
		require.NoError(t, swapRoleAssignmentPrimaryKey(context.Background(), access))
		assert.Empty(t, access.executed)
	})
	t.Run("refuses to swap onto an invalid index", func(t *testing.T) {
		access := newRoleAssignmentAccess()
		access.indexes[roleAssignmentPrimaryKeyIndex] = roleAssignmentIndexRow(true, false,
			roleAssignmentTargetPrimaryKey)
		require.ErrorContains(t, swapRoleAssignmentPrimaryKey(context.Background(), access), "not valid")
		assert.Empty(t, access.executed)
	})
}

func TestRoleAssignmentKeyPropagatesInspectionErrors(t *testing.T) {
	operation := NewWidenRoleAssignmentPrimaryKeyOperation()
	access := newRoleAssignmentAccess()
	access.err = errors.New("database unavailable")
	_, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
	require.ErrorContains(t, err, "database unavailable")
}
