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

type entityMigrationReadAccess struct {
	rows []engine.QueryRow
}

func (a entityMigrationReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != engine.StoreIDEntityDB || query.GetID() != queryEntityTypeLength.GetID() {
		return nil, errors.New("unexpected query")
	}
	return a.rows, nil
}

func (entityMigrationReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandEntityTypeLengthOperation(t *testing.T) {
	operation, err := newExpandEntityTypeLengthOperation()
	require.NoError(t, err)

	assert.Equal(t, engine.StoreIDEntityDB, operation.Definition().StoreID)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `VARCHAR(100)`)

	source, err := operation.PreVerify(context.Background(), entityTypeLengthAccess(50), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(context.Background(), entityTypeLengthAccess(100), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	invalid, err := operation.PreVerify(context.Background(), entityTypeLengthAccess(255), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func entityTypeLengthAccess(length int64) entityMigrationReadAccess {
	return entityMigrationReadAccess{rows: []engine.QueryRow{{
		"is_nullable": "NO", "data_type": "character varying", "maximum_length": length,
	}}}
}
