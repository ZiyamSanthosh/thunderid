// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestInitializerCreatesFixedPreFrameworkBaseline(t *testing.T) {
	manifest := validTestManifest()
	for index := range manifest.Stores {
		manifest.Stores[index].AcceptedSourceLevel = "1.0.1"
		manifest.Stores[index].RequiredLevel = "1.0.1"
	}

	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
		Return([]map[string]interface{}{}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryCreateMetadataSchema).Return(int64(0), nil).Once()

	initializedAt := time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
	stateIDs := []string{testStateID, "0199a123-4570-7abc-8def-0123456789ab"}
	for index, storeID := range []StoreID{StoreIDConfigDB, StoreIDEntityDB} {
		dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationState,
			stateIDs[index], string(storeID), 1, preFrameworkBaselineLevel, nil, string(StatusClean), nil, nil,
			initializedAt).Return(int64(1), nil).Once()
	}

	transactioner := &testTransactioner{}
	initializer := newTestInitializer(t, manifest, dbClient, transactioner)
	initializer.now = func() time.Time { return initializedAt }
	initializer.newID = sequenceIDs(stateIDs...)

	results, err := initializer.Initialize(context.Background(), []StoreID{StoreIDConfigDB, StoreIDEntityDB})

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.True(t, results[0].Created)
	assert.Equal(t, preFrameworkBaselineLevel, results[0].State.CompletedLevel)
	assert.Equal(t, 1, transactioner.calls)
}

func TestInitializerRegistersManifestTargetInExistingSchema(t *testing.T) {
	manifest := validTestManifest()
	manifest.Stores[0].Changed = true
	manifest.Stores[0].RequiredLevel = manifest.ReleaseVersion

	dbClient := providermock.NewDBClientInterfaceMock(t)
	expectReadySchema(dbClient)
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{}, nil).Once()
	initializedAt := time.Date(2026, time.September, 30, 11, 0, 0, 0, time.UTC)
	dbClient.On("ExecuteContext", mock.Anything, queryInsertMigrationState,
		testStateID, string(StoreIDConfigDB), 1, manifest.ReleaseVersion, nil, string(StatusClean), nil, nil,
		initializedAt).Return(int64(1), nil).Once()

	initializer := newTestInitializer(t, manifest, dbClient, &testTransactioner{})
	initializer.now = func() time.Time { return initializedAt }
	initializer.newID = sequenceIDs(testStateID)

	results, err := initializer.Initialize(context.Background(), []StoreID{StoreIDConfigDB})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Created)
	assert.Equal(t, manifest.ReleaseVersion, results[0].State.CompletedLevel)
}

func TestInitializerPreservesExistingState(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	expectReadySchema(dbClient)
	updatedAt := time.Now().UTC()
	dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
		Return([]map[string]interface{}{validStateRow(updatedAt)}, nil).Once()

	initializer := newTestInitializer(t, validTestManifest(), dbClient, &testTransactioner{})
	initializer.newID = func() (string, error) {
		t.Fatal("existing state must not be replaced")
		return "", nil
	}

	results, err := initializer.Initialize(context.Background(), []StoreID{StoreIDConfigDB})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Created)
	assert.Equal(t, updatedAt, results[0].State.UpdatedAt)
}

func TestInitializerRejectsUnsafeState(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*providermock.DBClientInterfaceMock)
		errorMatch string
	}{
		{
			name: "partial schema",
			setup: func(dbClient *providermock.DBClientInterfaceMock) {
				dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
					Return([]map[string]interface{}{{"table_name": metadataStateTable}}, nil).Once()
			},
			errorMatch: "partial metadata schema",
		},
		{
			name: "unsupported metadata version",
			setup: func(dbClient *providermock.DBClientInterfaceMock) {
				expectReadySchema(dbClient)
				row := validStateRow(time.Now().UTC())
				row["metadata_version"] = int64(2)
				dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
					Return([]map[string]interface{}{row}, nil).Once()
			},
			errorMatch: "metadata version 2 is not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbClient := providermock.NewDBClientInterfaceMock(t)
			test.setup(dbClient)
			initializer := newTestInitializer(t, validTestManifest(), dbClient, &testTransactioner{})

			results, err := initializer.Initialize(context.Background(), []StoreID{StoreIDConfigDB})

			require.Error(t, err)
			assert.Nil(t, results)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func TestInitializerValidatesStoresBeforeTransaction(t *testing.T) {
	tests := []struct {
		name       string
		stores     []StoreID
		errorMatch string
	}{
		{name: "empty", errorMatch: "at least one"},
		{name: "duplicate", stores: []StoreID{StoreIDConfigDB, StoreIDConfigDB}, errorMatch: "duplicate"},
		{name: "unknown", stores: []StoreID{"unknown"}, errorMatch: "not declared"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transactioner := &testTransactioner{}
			initializer := newTestInitializer(t, validTestManifest(),
				providermock.NewDBClientInterfaceMock(t), transactioner)

			_, err := initializer.Initialize(context.Background(), test.stores)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorMatch)
			assert.Zero(t, transactioner.calls)
		})
	}
}

func TestInitializerReturnsIDGenerationFailure(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
		Return([]map[string]interface{}{}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, queryCreateMetadataSchema).Return(int64(0), nil).Once()

	initializer := newTestInitializer(t, validTestManifest(), dbClient, &testTransactioner{})
	initializer.newID = func() (string, error) { return "", errors.New("random source failed") }

	results, err := initializer.Initialize(context.Background(), []StoreID{StoreIDConfigDB})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.ErrorContains(t, err, "random source failed")
}

func TestNewInitializerValidation(t *testing.T) {
	dbClient := providermock.NewDBClientInterfaceMock(t)
	metadataStore := newTestMetadataStore(t, dbClient)
	transactioner := &testTransactioner{}

	invalidManifest := validTestManifest()
	invalidManifest.ManifestVersion = 2
	_, manifestErr := NewInitializer(invalidManifest, metadataStore, transactioner)
	_, metadataStoreErr := NewInitializer(validTestManifest(), nil, transactioner)
	_, transactionerErr := NewInitializer(validTestManifest(), metadataStore, nil)

	require.Error(t, manifestErr)
	require.Error(t, metadataStoreErr)
	require.Error(t, transactionerErr)
}

func expectReadySchema(dbClient *providermock.DBClientInterfaceMock) {
	dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
		Return([]map[string]interface{}{
			{"table_name": metadataStateTable},
			{"table_name": metadataProgressTable},
		}, nil).Once()
}

func newTestInitializer(
	t *testing.T,
	manifest ReleaseManifest,
	dbClient *providermock.DBClientInterfaceMock,
	transactioner *testTransactioner,
) *Initializer {
	t.Helper()
	initializer, err := NewInitializer(manifest, newTestMetadataStore(t, dbClient), transactioner)
	require.NoError(t, err)
	return initializer
}

func sequenceIDs(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		id := ids[index]
		index++
		return id, nil
	}
}

type testTransactioner struct {
	calls int
}

func (t *testTransactioner) Transact(ctx context.Context, txFunc func(context.Context) error) error {
	t.calls++
	return txFunc(ctx)
}
