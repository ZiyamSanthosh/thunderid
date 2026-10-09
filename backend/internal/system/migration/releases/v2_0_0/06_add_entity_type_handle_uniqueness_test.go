// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestAddEntityTypeHandleUniquenessOperation(t *testing.T) {
	operation, err := newAddEntityTypeHandleUniquenessOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/06_add_entity_type_handle_uniqueness"), definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `UNIQUE (HANDLE, CATEGORY, DEPLOYMENT_ID)`)

	source, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), legacyEntityTypeUniqueColumns, targetEntityTypeUniqueColumns),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeContractAccess(
		entityTypeExpandedColumnRows(), targetEntityTypeUniqueColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func entityTypeContractAccess(
	columns []engine.QueryRow,
	uniqueColumns ...string,
) authorizationPDPReadAccess {
	constraintRows := make([]engine.QueryRow, 0, len(uniqueColumns))
	for index, columns := range uniqueColumns {
		constraintRows = append(constraintRows, engine.QueryRow{
			"constraint_name": fmt.Sprintf("entity_type_unique_%d", index),
			"column_names":    columns,
		})
	}
	return authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
		queryEntityTypeMigrationColumns.GetID():  columns,
		queryEntityTypeUniqueConstraints.GetID(): constraintRows,
	}}
}
