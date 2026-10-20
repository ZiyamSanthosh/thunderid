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
			mock.Anything, mock.Anything, 2, "1.0.0", nil, "complete", "clean", nil, nil, mock.Anything).
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
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

func TestRunStartupCompatibilityCheckAcceptsContractReadyTarget(t *testing.T) {
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		row := testMigrationStateRow(storeID)
		if storeID == engine.StoreIDConfigDB || storeID == engine.StoreIDRuntimePersistent {
			row["completed_level"] = "1.0.0"
			row["target_level"] = "2.0.0"
			row["stage"] = string(engine.MigrationStageContractReady)
			row["status"] = string(engine.StatusCompleted)
		}
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}

	report, err := runStartupCompatibilityCheck(context.Background(), cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.True(t, report.Compatible())
}

func TestRunStartupCompatibilityCheckAcceptsExpandedTarget(t *testing.T) {
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
		}, nil).Times(4)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
		engine.StoreIDRuntimeTransient,
		engine.StoreIDRuntimePersistent,
	} {
		row := testMigrationStateRow(storeID)
		if storeID == engine.StoreIDConfigDB || storeID == engine.StoreIDRuntimePersistent {
			row["completed_level"] = "1.0.0"
			row["target_level"] = "2.0.0"
			row["stage"] = string(engine.MigrationStageExpanded)
			row["status"] = string(engine.StatusCompleted)
		}
		dbClient.On("QueryContext", mock.Anything, mock.Anything, string(storeID)).
			Return([]map[string]interface{}{row}, nil).Once()
	}

	report, err := runStartupCompatibilityCheck(context.Background(), cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.True(t, report.Compatible())
	assert.Equal(t, engine.CompatibilityReasonNone, report.Stores[0].Reason)
	assert.Equal(t, engine.CompatibilityReasonNone, report.Stores[3].Reason)
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

func TestRunStartupCompatibilityCheckChecksSupportedStoresInMixedConfiguration(t *testing.T) {
	dataSource := testPostgresDataSource("thunderid")
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config: dataSource, Entity: dataSource,
		RuntimeTransient:  config.DataSource{Type: "sqlite"},
		RuntimePersistent: dataSource,
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{"table_name": "MIGRATION_STATE"},
			{"table_name": "MIGRATION_PROGRESS"},
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
		}, nil).Times(3)
	for _, storeID := range []engine.StoreID{
		engine.StoreIDConfigDB,
		engine.StoreIDEntityDB,
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
	assert.False(t, report.Stores[0].Skipped)
	assert.False(t, report.Stores[1].Skipped)
	assert.True(t, report.Stores[2].Skipped)
	assert.Equal(t, engine.CompatibilityReasonUnsupportedProvider, report.Stores[2].Reason)
	assert.False(t, report.Stores[2].Compatible())
	assert.False(t, report.Stores[3].Skipped)
}

func TestRunStartupCompatibilityCheckRejectsIncompatibleSupportedStoreInMixedConfiguration(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{
		Config:            testPostgresDataSource("configdb"),
		Entity:            config.DataSource{Type: "sqlite"},
		RuntimeTransient:  config.DataSource{Type: "sqlite"},
		RuntimePersistent: config.DataSource{Type: "sqlite"},
	}}
	dbProvider := providermock.NewDBProviderInterfaceMock(t)
	dbClient := providermock.NewDBClientInterfaceMock(t)
	dbProvider.On("GetConfigDBClient").Return(dbClient, nil).Once()
	dbClient.On("QueryContext", mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Once()

	report, err := runStartupCompatibilityCheck(context.Background(), cfg, dbProvider)

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.Compatible())
	assert.Equal(t, engine.CompatibilityReasonMetadataMissing, report.Stores[0].Reason)
	for _, store := range report.Stores[1:] {
		assert.True(t, store.Skipped)
		assert.Equal(t, engine.CompatibilityReasonUnsupportedProvider, store.Reason)
	}
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
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
	assert.Nil(t, result.preflight)
	require.NotNil(t, result.workflowCheck)
	assert.True(t, result.workflowCheck.Ready())
	require.Len(t, result.workflowCheck.Stores, 4)
	require.Len(t, result.workflowCheck.Phases, 2)
	assert.Equal(t, engine.OperationPhaseExpand, result.workflowCheck.Phases[0].Phase)
	assert.Equal(t, engine.OperationPhaseMigrate, result.workflowCheck.Phases[1].Phase)
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
	require.NotNil(t, result.workflowCheck)
	assert.False(t, result.workflowCheck.Ready())
	require.Len(t, result.workflowCheck.Stores, 4)
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
			{"table_name": "MIGRATION_DEPLOYMENT_STATE"},
			{"table_name": "MIGRATION_RUN"},
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
	assert.Nil(t, result.application)
	require.NotNil(t, result.workflow)
	require.Len(t, result.workflow.Phases, 1)
	assert.Equal(t, engine.OperationPhaseExpand, result.workflow.Phases[0].Phase)
	assert.True(t, result.workflow.Phases[0].AlreadyAtTarget)
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
		{name: "unsupported phase", args: []string{migrationSubcommand, migrationApplyAction, "invalid"}, cfg: &config.Config{}},
		{name: "phase on unsupported action", args: []string{migrationSubcommand, migrationStatusAction, "expand"}, cfg: &config.Config{}},
		{name: "phase on rollback", args: []string{migrationSubcommand, migrationRollbackAction, "migrate"}, cfg: &config.Config{}},
		{name: "too many arguments", args: []string{migrationSubcommand, migrationApplyAction, "expand", "extra"}, cfg: &config.Config{}},
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

func TestMigrationCommandPhase(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected engine.OperationPhase
	}{
		{name: "not specified", args: []string{migrationSubcommand, migrationApplyAction}},
		{name: "rollback", args: []string{migrationSubcommand, migrationRollbackAction}},
		{name: "expand", args: []string{migrationSubcommand, migrationApplyAction, "expand"}, expected: engine.OperationPhaseExpand},
		{name: "migrate", args: []string{migrationSubcommand, migrationApplyAction, "migrate"}, expected: engine.OperationPhaseMigrate},
		{name: "contract check", args: []string{migrationSubcommand, migrationCheckAction, "contract"}, expected: engine.OperationPhaseContract},
		{name: "contract apply", args: []string{migrationSubcommand, migrationApplyAction, "contract"}, expected: engine.OperationPhaseContract},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			phase, err := migrationCommandPhase(test.args)

			require.NoError(t, err)
			assert.Equal(t, test.expected, phase)
		})
	}
}

func TestParseMigrationCommandOptionsAcceptsDeploymentSelection(t *testing.T) {
	options, err := parseMigrationCommandOptions([]string{
		migrationSubcommand, migrationApplyAction, string(engine.OperationPhaseMigrate),
		"--deployment-id", "deployment-a", "--deployment-id=deployment-b",
	})

	require.NoError(t, err)
	assert.Equal(t, engine.OperationPhaseMigrate, options.phase)
	assert.Equal(t, []string{"deployment-a", "deployment-b"}, options.selection.DeploymentIDs)
}

func TestParseMigrationCommandOptionsRestrictsDeploymentSelectionToMigrateApply(t *testing.T) {
	_, err := parseMigrationCommandOptions([]string{
		migrationSubcommand, migrationApplyAction, "expand", "--deployment-id", "deployment-a",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "supported only by migration apply migrate")
}

func TestParseMigrationCommandOptionsAcceptsRollbackScopes(t *testing.T) {
	phaseOptions, err := parseMigrationCommandOptions([]string{
		migrationSubcommand, migrationRollbackAction, "--phase", "migrate",
	})
	require.NoError(t, err)
	assert.Equal(t, engine.OperationPhaseMigrate, phaseOptions.phase)
	assert.False(t, phaseOptions.revertAll)

	allOptions, err := parseMigrationCommandOptions([]string{
		migrationSubcommand, migrationRollbackAction, "--revert-all",
	})
	require.NoError(t, err)
	assert.Empty(t, allOptions.phase)
	assert.True(t, allOptions.revertAll)
}

func TestParseMigrationCommandOptionsRejectsConflictingRollbackScopes(t *testing.T) {
	_, err := parseMigrationCommandOptions([]string{
		migrationSubcommand, migrationRollbackAction, "--phase=migrate", "--revert-all",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot combine")
}

func testMigrationStateRow(storeID engine.StoreID) map[string]interface{} {
	completedLevel := "1.0.0"
	if storeID != engine.StoreIDRuntimeTransient {
		completedLevel = "2.0.0"
	}
	return map[string]interface{}{
		"id":               "0199a123-4567-7abc-8def-0123456789ab",
		"store_id":         string(storeID),
		"metadata_version": int64(2),
		"completed_level":  completedLevel,
		"target_level":     nil,
		"stage":            "complete",
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
