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

func TestRollbackEntityTypeHandleBackfillOperation(t *testing.T) {
	operation := newRollbackEntityTypeHandleBackfillOperation()
	definition := operation.Definition()

	assert.Equal(t, engine.OperationID("2.0.0/configdb/05_backfill_entity_type_handle_rollback"),
		definition.ID)
	assert.Equal(t, engine.OperationID("2.0.0/configdb/05_backfill_entity_type_handle"),
		operation.ForwardOperationID())
	assert.Equal(t,
		"Remove the entity-type handle representation before returning to the source version",
		definition.Description)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)
	assert.Implements(t, (*engine.ResumableOperation)(nil), operation)

	targets, err := operation.ResolveTargets(context.Background(), nil,
		engine.MigrationSelection{DeploymentIDs: []string{"deployment-a", "deployment-b"}})
	require.NoError(t, err)
	assert.Equal(t, []engine.ExecutionTarget{
		{DeploymentID: "deployment-a"},
		{DeploymentID: "deployment-b"},
	}, targets)

	source, err := operation.PreVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "Legacy Person", "legacy-person"),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)
	assert.Equal(t, "The entity-type handle representation requires rollback", source.Summary)

	target, err := operation.PostVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "Legacy Person", nil),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
	assert.Equal(t, "The entity-type handle representation has been removed", target.Summary)
}
