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

func TestCompatibilityGateAcceptsCompatibleStores(t *testing.T) {
	manifest := validTestManifest()
	metadataStores := make(map[StoreID]*MetadataStore, len(manifest.Stores))
	for _, requirement := range manifest.Stores {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		expectReadySchema(dbClient)
		row := compatibilityStateRow(requirement.ID, requirement.RequiredLevel)
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
		metadataStores[requirement.ID] = newTestMetadataStore(t, dbClient)
	}

	gate, err := NewCompatibilityGate(manifest)
	require.NoError(t, err)
	report := gate.Check(context.Background(), metadataStores)

	assert.True(t, report.Compatible())
	require.Len(t, report.Stores, len(manifest.Stores))
	for index, result := range report.Stores {
		assert.Equal(t, manifest.Stores[index].ID, result.StoreID)
		assert.True(t, result.Compatible())
		assert.NoError(t, result.Err)
	}
}

func TestCompatibilityGateCollectsStoreFailures(t *testing.T) {
	manifest := validTestManifest()
	configClient := providermock.NewDBClientInterfaceMock(t)
	configClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
		Return([]map[string]interface{}{}, nil).Once()

	entityClient := providermock.NewDBClientInterfaceMock(t)
	expectReadySchema(entityClient)
	entityRow := compatibilityStateRow(StoreIDEntityDB, "0.9.0")
	entityClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDEntityDB)).
		Return([]map[string]interface{}{entityRow}, nil).Once()

	runtimeTransientClient := providermock.NewDBClientInterfaceMock(t)
	runtimeTransientClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
		Return(nil, errors.New("database unavailable")).Once()

	runtimePersistentClient := providermock.NewDBClientInterfaceMock(t)
	expectReadySchema(runtimePersistentClient)
	runtimePersistentRow := compatibilityStateRow(StoreIDRuntimePersistent, "1.0.0")
	runtimePersistentRow["status"] = string(StatusInProgress)
	runtimePersistentRow["stage"] = string(MigrationStageExpanding)
	runtimePersistentRow["target_level"] = "1.1.0"
	runtimePersistentRow["active_run_id"] = testRunID
	runtimePersistentClient.On("QueryContext", mock.Anything, queryGetMigrationState,
		string(StoreIDRuntimePersistent)).Return([]map[string]interface{}{runtimePersistentRow}, nil).Once()

	gate, err := NewCompatibilityGate(manifest)
	require.NoError(t, err)
	report := gate.Check(context.Background(), map[StoreID]*MetadataStore{
		StoreIDConfigDB:          newTestMetadataStore(t, configClient),
		StoreIDEntityDB:          newTestMetadataStore(t, entityClient),
		StoreIDRuntimeTransient:  newTestMetadataStore(t, runtimeTransientClient),
		StoreIDRuntimePersistent: newTestMetadataStore(t, runtimePersistentClient),
	})

	assert.False(t, report.Compatible())
	require.Len(t, report.Stores, 4)
	assert.Equal(t, CompatibilityReasonMetadataMissing, report.Stores[0].Reason)
	assert.Equal(t, CompatibilityReasonLevelMismatch, report.Stores[1].Reason)
	assert.Equal(t, "0.9.0", report.Stores[1].CompletedLevel)
	assert.Equal(t, CompatibilityReasonUnverifiable, report.Stores[2].Reason)
	assert.ErrorContains(t, report.Stores[2].Err, "database unavailable")
	assert.Equal(t, CompatibilityReasonMigrationIncomplete, report.Stores[3].Reason)
	assert.Equal(t, testRunID, report.Stores[3].ActiveRunID)
	assert.Empty(t, report.NextMigrationRelease)
	assert.Empty(t, report.MigrationRoute)
}

func TestCompatibilityGateReportsNextMigrationRelease(t *testing.T) {
	intermediate := routeManifest("1.1.0", "1.0.0", StoreIDConfigDB)
	target := routeManifest("2.0.0", "1.1.0", StoreIDConfigDB)
	metadataStores := make(map[StoreID]*MetadataStore, len(target.Stores))
	for _, requirement := range target.Stores {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		expectReadySchema(dbClient)
		row := compatibilityStateRow(requirement.ID, "1.0.0")
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{row}, nil).Once()
		metadataStores[requirement.ID] = newTestMetadataStore(t, dbClient)
	}

	gate, err := NewCompatibilityGate(target, intermediate, target)
	require.NoError(t, err)
	report := gate.Check(context.Background(), metadataStores)

	assert.False(t, report.Compatible())
	assert.Equal(t, "1.1.0", report.NextMigrationRelease)
	assert.Equal(t, []string{"1.1.0", "2.0.0"}, report.MigrationRoute)
	assert.NoError(t, report.RouteErr)
}

func TestCompatibilityGateClassifiesMetadataFailures(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*providermock.DBClientInterfaceMock)
		metadataStores func(*MetadataStore) map[StoreID]*MetadataStore
		reason         CompatibilityReason
	}{
		{
			name: "metadataStore not configured",
			metadataStores: func(_ *MetadataStore) map[StoreID]*MetadataStore {
				return map[StoreID]*MetadataStore{}
			},
			reason: CompatibilityReasonUnverifiable,
		},
		{
			name: "partial schema",
			setup: func(dbClient *providermock.DBClientInterfaceMock) {
				dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
					Return([]map[string]interface{}{{"table_name": metadataStateTable}}, nil).Once()
			},
			reason: CompatibilityReasonMetadataPartial,
		},
		{
			name: "missing state row",
			setup: func(dbClient *providermock.DBClientInterfaceMock) {
				expectReadySchema(dbClient)
				dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
					Return([]map[string]interface{}{}, nil).Once()
			},
			reason: CompatibilityReasonMetadataMissing,
		},
		{
			name: "unsupported metadata version",
			setup: func(dbClient *providermock.DBClientInterfaceMock) {
				expectReadySchema(dbClient)
				row := compatibilityStateRow(StoreIDConfigDB, "1.0.0")
				row["metadata_version"] = int64(2)
				dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(StoreIDConfigDB)).
					Return([]map[string]interface{}{row}, nil).Once()
			},
			reason: CompatibilityReasonUnsupportedMetadata,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbClient := providermock.NewDBClientInterfaceMock(t)
			if test.setup != nil {
				test.setup(dbClient)
			}
			metadataStore := newTestMetadataStore(t, dbClient)
			metadataStores := map[StoreID]*MetadataStore{StoreIDConfigDB: metadataStore}
			if test.metadataStores != nil {
				metadataStores = test.metadataStores(metadataStore)
			}

			manifest := validTestManifest()
			gate, err := NewCompatibilityGate(manifest)
			require.NoError(t, err)
			result := gate.checkStore(context.Background(), manifest.Stores[0], metadataStores[StoreIDConfigDB])

			assert.Equal(t, test.reason, result.Reason)
			assert.False(t, result.Compatible())
		})
	}
}

func TestNewCompatibilityGateRejectsInvalidManifest(t *testing.T) {
	manifest := validTestManifest()
	manifest.ManifestVersion++

	gate, err := NewCompatibilityGate(manifest)

	require.Error(t, err)
	assert.Nil(t, gate)
}

func compatibilityStateRow(storeID StoreID, completedLevel string) map[string]interface{} {
	return map[string]interface{}{
		"id": testStateID, "store_id": string(storeID), "metadata_version": int64(1),
		"completed_level": completedLevel, "target_level": nil, "stage": string(MigrationStageComplete),
		"status":        string(StatusClean),
		"active_run_id": nil, "failure_summary": nil,
		"updated_at": time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC),
	}
}
