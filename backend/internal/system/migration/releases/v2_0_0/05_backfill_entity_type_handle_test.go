// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	systemutils "github.com/thunder-id/thunderid/internal/system/utils"
)

func TestBackfillEntityTypeHandleOperation(t *testing.T) {
	operation := newBackfillEntityTypeHandleOperation()
	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/05_backfill_entity_type_handle"), definition.ID)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)
	assert.Implements(t, (*engine.ResumableOperation)(nil), operation)

	targets, err := operation.ResolveTargets(context.Background(), nil,
		engine.MigrationSelection{DeploymentIDs: []string{"deployment-a"}})
	require.NoError(t, err)
	assert.Equal(t, []engine.ExecutionTarget{{}}, targets)

	source, err := operation.PreVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "Legacy Person", nil),
		entityTypeHandleRow("id-2", "Customer", nil),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "Legacy Person", "legacy-person"),
		entityTypeHandleRow("id-2", "Customer", "customer"),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "Legacy Person", "Invalid Handle"),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestConvertEntityTypeNameToHandle(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{name: "customer", expected: "customer"},
		{name: "Customer Accounts", expected: "customer-accounts"},
		{name: "  Customer.API  ", expected: "customer-api"},
		{name: "_Customer_", expected: "customer"},
		{name: "Pérson", expected: "p-rson"},
		{name: "***", expected: "entity-type"},
		{name: strings.Repeat("A", 101), expected: strings.Repeat("a", 100)},
	}
	for _, test := range tests {
		handle := convertEntityTypeNameToHandle(test.name)
		assert.Equal(t, test.expected, handle)
		assert.True(t, systemutils.IsValidHandle(handle))
	}
}

func TestNextAvailableEntityTypeHandle(t *testing.T) {
	usedHandles := map[string]struct{}{
		entityTypeHandleKey("deployment-a", "USER", "apple"):   {},
		entityTypeHandleKey("deployment-a", "USER", "apple_1"): {},
	}
	handle, err := nextAvailableEntityTypeHandle("apple", "deployment-a", "USER", usedHandles)
	require.NoError(t, err)
	assert.Equal(t, "apple_2", handle)

	longHandle, err := nextAvailableEntityTypeHandle(
		strings.Repeat("a", 100), "deployment-a", "USER",
		map[string]struct{}{entityTypeHandleKey("deployment-a", "USER", strings.Repeat("a", 100)): {}},
	)
	require.NoError(t, err)
	assert.True(t, systemutils.IsValidHandle(longHandle))
	assert.Equal(t, strings.Repeat("a", 98)+"_1", longHandle)
	assert.Len(t, longHandle, 100)
}

func TestEntityTypeHandleBatchResult(t *testing.T) {
	result, err := entityTypeHandleBatchResult(entityTypeHandleCheckpoint{LastID: "id-100"}, false)
	require.NoError(t, err)
	assert.JSONEq(t, `{"lastID":"id-100"}`, string(result.CheckpointData))
	assert.False(t, result.Completed)

	result, err = entityTypeHandleBatchResult(entityTypeHandleCheckpoint{LastID: "id-101"}, true)
	require.NoError(t, err)
	assert.True(t, result.Completed)
}

func entityTypeHandleAccess(rows ...engine.QueryRow) authorizationPDPReadAccess {
	return authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
		queryEntityTypesForHandleBackfill.GetID(): rows,
	}}
}

func entityTypeHandleRow(id string, name string, handle interface{}) engine.QueryRow {
	return engine.QueryRow{
		"id": id, "name": name, "category": "USER", "handle": handle,
		"deployment_id": "deployment-a",
	}
}
