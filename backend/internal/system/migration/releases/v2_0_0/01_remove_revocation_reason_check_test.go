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

type operationReadAccess struct {
	columnRows     []engine.QueryRow
	constraintRows []engine.QueryRow
	err            error
}

func (a operationReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if storeID != engine.StoreIDRuntimePersistent {
		return nil, errors.New("unexpected store")
	}
	if a.err != nil {
		return nil, a.err
	}
	switch query.GetID() {
	case queryRevokedTokenReasonColumn.GetID():
		return a.columnRows, nil
	case queryRevokedTokenReasonConstraints.GetID():
		return a.constraintRows, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (operationReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestRemoveRevocationReasonCheckOperationDefinition(t *testing.T) {
	operation, err := newRemoveRevocationReasonCheckOperation()

	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[0].Query, "lock_timeout")
	assert.Contains(t, implementation.Statements[1].Query, "pg_constraint")
	assert.Contains(t, implementation.Statements[1].Query, legacyRevokedTokenReasonConstraint)

	targets, err := operation.ResolveTargets(
		context.Background(), operationReadAccess{},
		engine.MigrationSelection{DeploymentIDs: []string{"deployment-a", "deployment-b"}})
	require.NoError(t, err)
	require.Equal(t, []engine.ExecutionTarget{{}}, targets)
}

func TestRemoveRevocationReasonCheckVerification(t *testing.T) {
	operation, err := newRemoveRevocationReasonCheckOperation()
	require.NoError(t, err)
	target := engine.ExecutionTarget{}

	source, err := operation.PreVerify(context.Background(), operationReadAccess{
		columnRows: revokedTokenReasonColumnRows(),
		constraintRows: []engine.QueryRow{{
			"constraint_name": legacyRevokedTokenReasonConstraint,
		}},
	}, target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	targetResult, err := operation.PostVerify(context.Background(), operationReadAccess{
		columnRows: revokedTokenReasonColumnRows(),
	}, target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, targetResult.State)

	invalid, err := operation.PreVerify(context.Background(), operationReadAccess{
		columnRows: []engine.QueryRow{{
			"is_nullable": "YES", "data_type": "character varying",
			"character_maximum_length": int64(30),
		}},
	}, target)
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
}

func TestRemoveRevocationReasonCheckPropagatesInspectionFailure(t *testing.T) {
	operation, err := newRemoveRevocationReasonCheckOperation()
	require.NoError(t, err)

	result, err := operation.PreVerify(context.Background(), operationReadAccess{
		err: errors.New("database unavailable"),
	}, engine.ExecutionTarget{})

	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func revokedTokenReasonColumnRows() []engine.QueryRow {
	return []engine.QueryRow{{
		"is_nullable": "NO", "data_type": "character varying",
		"character_maximum_length": int64(30),
	}}
}
