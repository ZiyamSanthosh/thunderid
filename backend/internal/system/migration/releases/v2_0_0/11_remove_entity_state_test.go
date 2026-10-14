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

func TestRemoveEntityStateOperation(t *testing.T) {
	operation, err := newRemoveEntityStateOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, removeEntityStateOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, []engine.StoreID{engine.StoreIDRuntimePersistent}, definition.ReadStoreIDs)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `DROP COLUMN STATE`)
}

func TestRemoveEntityStateVerification(t *testing.T) {
	operation, err := newRemoveEntityStateOperation()
	require.NoError(t, err)

	source, err := operation.PreVerify(context.Background(), entityStateContractAccess(false), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityStateContractAccess(true), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestRemoveEntityStateReadinessRequiresRuntimeTable(t *testing.T) {
	operation, err := newRemoveEntityStateOperation()
	require.NoError(t, err)
	access := entityStateContractAccess(false)
	access.rows[engine.StoreIDRuntimePersistent] = map[string][]engine.QueryRow{
		queryEntityRuntimeDataTable.GetID(): nil,
	}

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), access, engine.PlanContext{})

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, result.State)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "entity_state_contract_not_ready", result.Findings[0].Code)
}

func TestRemoveEntityStateReadinessRequiresTemporaryDefault(t *testing.T) {
	operation, err := newRemoveEntityStateOperation()
	require.NoError(t, err)
	access := entityStateContractAccess(false)
	access.rows[engine.StoreIDEntityDB][queryEntityStateColumn.GetID()][0]["column_default"] = "none"

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), access, engine.PlanContext{})

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, result.State)
	assert.Contains(t, result.Summary, "temporary ACTIVE default")
}

func entityStateContractAccess(stateAbsent bool) entityRuntimeMigrationReadAccess {
	entityRows := map[string][]engine.QueryRow{}
	if stateAbsent {
		entityRows[queryEntityStateColumn.GetID()] = nil
	} else {
		entityRows[queryEntityStateColumn.GetID()] = []engine.QueryRow{{
			"is_nullable": "NO", "data_type": "character varying", "maximum_length": "50",
			"column_default": "'ACTIVE'::character varying",
		}}
		entityRows[queryNonActiveEntityState.GetID()] = []engine.QueryRow{{"has_non_active_state": false}}
	}
	return entityRuntimeMigrationReadAccess{rows: map[engine.StoreID]map[string][]engine.QueryRow{
		engine.StoreIDEntityDB:          entityRows,
		engine.StoreIDRuntimePersistent: entityRuntimeTargetRows(),
	}}
}
