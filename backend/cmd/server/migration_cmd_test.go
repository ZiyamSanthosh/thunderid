// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/config"
	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestRunMigrationInitializeForSharedNamespace(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbProvider.On("GetConfigDBTransactioner").Return(immediateTransactioner{}, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Once()
	dbClient.On("ExecuteContext", mock.Anything, mock.Anything).
		Return(int64(0), nil).Once()
	for range 4 {
		dbClient.On("ExecuteContext", mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, 1, "1.0.0", nil, "clean", nil, nil, mock.Anything).
			Return(int64(1), nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationInitializeAction}, cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.initialization, 4)
	assert.Nil(t, result.compatibility)
	for index, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		assert.Equal(t, storeID, result.initialization[index].State.StoreID)
		assert.Equal(t, "1.0.0", result.initialization[index].State.CompletedLevel)
		assert.True(t, result.initialization[index].Created)
	}
}

func TestRunMigrationStatusForSharedNamespace(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{testMigrationStateRow(storeID)}, nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationStatusAction}, cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.initialization)
	require.NotNil(t, result.compatibility)
	assert.True(t, result.compatibility.Compatible())
	require.Len(t, result.compatibility.Stores, 4)
}

func TestRunMigrationStatusReportsMissingMetadata(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Times(4)

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationStatusAction}, cfg, dbProvider)

	require.ErrorIs(t, err, errMigrationStatusIncompatible)
	require.NotNil(t, result)
	require.NotNil(t, result.compatibility)
	assert.False(t, result.compatibility.Compatible())
	for _, store := range result.compatibility.Stores {
		assert.Equal(t, engine.CompatibilityReasonMetadataMissing, store.Reason)
	}
}

func TestRunMigrationStatusReportsDirectMigrationRoute(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		row := testMigrationStateRow(storeID)
		row["completed_level"] = "1.0.0"
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationStatusAction}, cfg, dbProvider)

	require.ErrorIs(t, err, errMigrationStatusIncompatible)
	require.NotNil(t, result)
	require.NotNil(t, result.compatibility)
	assert.Equal(t, "2.0.0", result.compatibility.NextMigrationRelease)
	assert.Equal(t, []string{"2.0.0"}, result.compatibility.MigrationRoute)
	assert.NoError(t, result.compatibility.RouteErr)
}

func TestRunStartupCompatibilityCheckAcceptsCompatibleSharedNamespace(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{testMigrationStateRow(storeID)}, nil).Once()
	}

	report, err := runStartupCompatibilityCheck(context.Background(), cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.True(t, report.Compatible())
	require.Len(t, report.Stores, 4)
}

func TestRunStartupCompatibilityCheckReportsIncompatibleMetadata(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Times(4)

	report, err := runStartupCompatibilityCheck(context.Background(), cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.Compatible())
	for _, store := range report.Stores {
		assert.Equal(t, engine.CompatibilityReasonMetadataMissing, store.Reason)
	}
}

func TestRunStartupCompatibilityCheckValidatesDependencies(t *testing.T) {
	dbProvider := providermock.NewDBProviderInterfaceMock(t)

	_, configErr := runStartupCompatibilityCheck(context.Background(), nil, dbProvider)
	_, providerErr := runStartupCompatibilityCheck(context.Background(), &config.Config{}, nil)

	require.Error(t, configErr)
	require.Error(t, providerErr)
}

func TestRunStartupCompatibilityCheckSkipsUnsupportedProviders(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: config.DataSource{Type: "sqlite"}, Entity: config.DataSource{Type: "sqlite"},
		RuntimeTransient:  config.DataSource{Type: "sqlite"},
		RuntimePersistent: config.DataSource{Type: "sqlite"},
	}}

	report, err := runStartupCompatibilityCheck(
		context.Background(), cfg, providermock.NewDBProviderInterfaceMock(t))

	require.NoError(t, err)
	assert.Nil(t, report)
}

func TestRunMigrationCheckForSharedNamespace(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{testMigrationStateRow(storeID)}, nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationCheckAction}, cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.initialization)
	assert.Nil(t, result.compatibility)
	require.NotNil(t, result.preflight)
	assert.True(t, result.preflight.Ready())
	require.Len(t, result.preflight.Stores, 4)
	assert.Empty(t, result.preflight.Operations)
}

func TestRunMigrationCheckReturnsBlockedReport(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Times(4)

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationCheckAction}, cfg, dbProvider)

	require.ErrorIs(t, err, errMigrationCheckBlocked)
	require.NotNil(t, result)
	require.NotNil(t, result.preflight)
	assert.False(t, result.preflight.Ready())
	require.Len(t, result.preflight.Stores, 4)
}

func TestRunMigrationApplyReturnsNoOpWhenStoresAreAtTarget(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbProvider.On("GetConfigDBTransactioner").Return(immediateTransactioner{}, nil).Once()
	expectMigrationExecutionLock(t, dbClient)
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{testMigrationStateRow(storeID)}, nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationApplyAction}, cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.application)
	assert.True(t, result.application.AlreadyAtTarget)
	assert.Empty(t, result.application.RunID)
	assert.Empty(t, result.application.Operations)
	assert.Empty(t, result.application.Finalizations)
	assert.Nil(t, result.compatibility)
	assert.Nil(t, result.preflight)
}

func TestRunMigrationResumeReportsNoFailedRun(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbProvider.On("GetConfigDBTransactioner").Return(immediateTransactioner{}, nil).Once()
	expectMigrationExecutionLock(t, dbClient)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{testMigrationStateRow(storeID)}, nil).Once()
	}

	result, err := runMigrationCommand(
		context.Background(), []string{migrationSubcommand, migrationResumeAction}, cfg, dbProvider)

	require.Error(t, err)
	assert.ErrorIs(t, err, engine.ErrNoMigrationToResume)
	require.NotNil(t, result)
	require.NotNil(t, result.application)
	assert.True(t, result.application.Resumed)
}

func TestMigrationNamespacesPreserveSeparateStores(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config:            testPostgresDataSource("config"),
		Entity:            testPostgresDataSource("entity"),
		RuntimeTransient:  testPostgresDataSource("runtime_transient"),
		RuntimePersistent: testPostgresDataSource("runtime_persistent"),
	}}

	namespaces, err := migrationNamespaces(cfg, providermock.NewDBProviderInterfaceMock(t))

	require.NoError(t, err)
	require.Len(t, namespaces, 4)
	for _, namespace := range namespaces {
		assert.Len(t, namespace.storeIDs, 1)
	}
}

func expectMigrationExecutionLock(t *testing.T, dbClient *providermock.DBClientInterfaceMock) {
	t.Helper()
	session := providermock.NewDBSessionInterfaceMock(t)
	session.On("QueryContext", mock.Anything, mock.Anything, mock.Anything).
		Return([]map[string]interface{}{{"acquired": true}}, nil).Once()
	session.On("QueryContext", mock.Anything, mock.Anything, mock.Anything).
		Return([]map[string]interface{}{{"released": true}}, nil).Once()
	dbClient.On("WithSession", mock.Anything, mock.Anything).
		Return(func(ctx context.Context, fn func(dbprovider.DBSessionInterface) error) error {
			return fn(session)
		}).Once()
}

func TestMigrationNamespacesRejectUnsupportedDatabase(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	cfg.Database.Entity.Type = "sqlite"

	namespaces, err := migrationNamespaces(cfg, providermock.NewDBProviderInterfaceMock(t))

	require.Error(t, err)
	assert.Nil(t, namespaces)
	assert.ErrorContains(t, err, "unsupported migration database type")
}

func TestMigrationNamespacesRejectDifferentUsersForSharedDatabase(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource, RuntimeTransient: dataSource, RuntimePersistent: dataSource,
	}}
	cfg.Database.Entity.Postgres.Username = "entity_user"

	namespaces, err := migrationNamespaces(cfg, providermock.NewDBProviderInterfaceMock(t))

	require.Error(t, err)
	assert.Nil(t, namespaces)
	assert.ErrorContains(t, err, "different connection identities")
}

func TestRunMigrationCommandValidatesInvocation(t *testing.T) {
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	tests := []struct {
		name string
		args []string
		cfg  *config.Config
	}{
		{name: "missing action", args: []string{migrationSubcommand}, cfg: &config.Config{}},
		{name: "unknown action", args: []string{migrationSubcommand, "unknown"}, cfg: &config.Config{}},
		{name: "missing configuration", args: []string{migrationSubcommand, migrationInitializeAction}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := runMigrationCommand(context.Background(), test.args, test.cfg, dbProvider)

			require.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func testMigrationStateRow(storeID engine.StoreID) map[string]interface{} {
	completedLevel := "1.0.0"
	if storeID == engine.StoreIDRuntimePersistent || storeID == engine.StoreIDConfigDB {
		completedLevel = "2.0.0"
	}
	return map[string]interface{}{
		"id":               "0199a123-4567-7abc-8def-0123456789ab",
		"store_id":         string(storeID),
		"metadata_version": int64(1),
		"completed_level":  completedLevel,
		"target_level":     nil,
		"status":           "clean",
		"active_run_id":    nil,
		"failure_summary":  nil,
		"updated_at":       time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC),
	}
}

func testPostgresDataSource(name string) config.DataSource {
	return config.DataSource{
		Type: string(engine.DatabaseTypePostgres),
		Postgres: config.PostgresDataSource{
			Hostname: "localhost",
			Port:     5432,
			Name:     name,
			Username: "thunderid",
		},
	}
}

type immediateTransactioner struct{}

func (immediateTransactioner) Transact(ctx context.Context, txFunc func(context.Context) error) error {
	return txFunc(ctx)
}
