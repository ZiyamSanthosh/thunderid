// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestOperationExecutionAccessRestrictsWritesToOwningStore(t *testing.T) {
	entityClient := providermock.NewDBClientInterfaceMock(t)
	configClient := providermock.NewDBClientInterfaceMock(t)
	query := dbmodel.DBQuery{ID: "MIG-TEST-03", PostgresQuery: "UPDATE example SET migrated = TRUE"}
	entityClient.On("ExecuteContext", mock.Anything, query, "argument").Return(int64(2), nil).Once()
	access, err := newOperationExecutionAccess(OperationDefinition{
		StoreID: StoreIDEntityDB, ReadStoreIDs: []StoreID{StoreIDConfigDB},
	}, map[StoreID]ReadStore{
		StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
		StoreIDConfigDB: {DatabaseType: DatabaseTypePostgres, Client: configClient},
	})
	require.NoError(t, err)

	rowsAffected, err := access.Execute(context.Background(), StoreIDEntityDB, query, "argument")
	require.NoError(t, err)
	assert.Equal(t, int64(2), rowsAffected)

	_, err = access.Execute(context.Background(), StoreIDConfigDB, query)
	assert.ErrorContains(t, err, "did not declare write access")
	_, err = access.Execute(context.Background(), StoreIDRuntimePersistent, query)
	assert.ErrorContains(t, err, "did not declare write access")
}

func TestOperationExecutionAccessRejectsMissingProviderQuery(t *testing.T) {
	entityClient := providermock.NewDBClientInterfaceMock(t)
	access, err := newOperationExecutionAccess(OperationDefinition{StoreID: StoreIDEntityDB},
		map[StoreID]ReadStore{
			StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
		})
	require.NoError(t, err)

	_, err = access.Execute(context.Background(), StoreIDEntityDB, dbmodel.DBQuery{ID: "MIG-TEST-04"})
	assert.ErrorContains(t, err, "has no implementation")
}
