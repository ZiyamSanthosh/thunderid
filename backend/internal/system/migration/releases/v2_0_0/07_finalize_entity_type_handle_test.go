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

func TestFinalizeEntityTypeHandleOperation(t *testing.T) {
	operation, err := newFinalizeEntityTypeHandleOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/07_finalize_entity_type_handle"), definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 3)
	assert.Contains(t, implementation.Statements[1].Query, `ALTER COLUMN HANDLE SET NOT NULL`)
	assert.NotContains(t, implementation.Statements[2].Query, `DROP COLUMN NAME`)

	source, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeContractAccess(
		entityTypeFinalColumnRows(), targetEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}
