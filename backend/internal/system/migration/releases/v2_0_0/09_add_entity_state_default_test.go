// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestAddEntityStateDefaultOperation(t *testing.T) {
	operation, err := newAddEntityStateDefaultOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, addEntityStateDefaultOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `SET DEFAULT 'ACTIVE'`)
}

func TestAddEntityStateDefaultVerification(t *testing.T) {
	operation, err := newAddEntityStateDefaultOperation()
	require.NoError(t, err)

	source, err := operation.PreVerify(context.Background(), entityStateAccess("none", false), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(),
		entityStateAccess("'ACTIVE'::character varying", false), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityStateAccess("none", true), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
	assert.Contains(t, invalid.Summary, "other than ACTIVE")

	absent, err := operation.PreVerify(context.Background(), entityStateAbsentAccess(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, absent.State)
}

func TestAddEntityStateDefaultReadinessBlocksNonActiveState(t *testing.T) {
	operation, err := newAddEntityStateDefaultOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), entityStateAccess("none", true), engine.PlanContext{})

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, result.State)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "unexpected_entity_state_schema", result.Findings[0].Code)
}

func entityStateAccess(defaultValue string, hasNonActiveState bool) entityRuntimeMigrationReadAccess {
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDEntityDB: {
			queryEntityStateColumn.GetID(): {{
				"is_nullable": "NO", "data_type": "character varying",
				"maximum_length": "50", "column_default": defaultValue,
			}},
			queryNonActiveEntityState.GetID(): {{"has_non_active_state": hasNonActiveState}},
		},
	}}
}

func entityStateAbsentAccess() entityRuntimeMigrationReadAccess {
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDEntityDB: {queryEntityStateColumn.GetID(): nil},
	}}
}
