// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/config"
	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/internal/system/migration/current"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const (
	migrationSubcommand       = "migration"
	migrationInitializeAction = "initialize"
	migrationStatusAction     = "status"
	migrationCheckAction      = "check"
	migrationApplyAction      = "apply"
	migrationResumeAction     = "resume"
)

var (
	errMigrationStatusIncompatible = errors.New("database stores are not compatible with this ThunderID release")
	errMigrationCheckBlocked       = errors.New("migration preflight is blocked")
)

type migrationCommandResult struct {
	action         string
	initialization []engine.InitializationResult
	compatibility  *engine.CompatibilityReport
	preflight      *engine.PreflightReport
	application    *engine.MigrationRunResult
	workflow       *engine.MigrationWorkflowResult
}

type migrationDatabaseAdapter struct {
	databaseType       engine.DatabaseType
	namespaceKey       func(config.DataSource) migrationNamespaceKey
	connectionIdentity func(config.DataSource) string
}

type migrationNamespaceKey struct {
	databaseType engine.DatabaseType
	location     string
}

var migrationDatabaseAdapters = map[string]migrationDatabaseAdapter{
	string(engine.DatabaseTypePostgres): {
		databaseType: engine.DatabaseTypePostgres,
		namespaceKey: func(dataSource config.DataSource) migrationNamespaceKey {
			postgres := dataSource.Postgres
			return migrationNamespaceKey{
				databaseType: engine.DatabaseTypePostgres,
				location:     fmt.Sprintf("%s:%d/%s", postgres.Hostname, postgres.Port, postgres.Name),
			}
		},
		connectionIdentity: func(dataSource config.DataSource) string {
			return dataSource.Postgres.Username
		},
	},
}

type migrationStoreBinding struct {
	storeID       engine.StoreID
	dataSource    config.DataSource
	client        func() (dbprovider.DBClientInterface, error)
	transactioner func() (providers.Transactioner, error)
}

type migrationNamespace struct {
	storeIDs           []engine.StoreID
	databaseType       engine.DatabaseType
	connectionIdentity string
	client             func() (dbprovider.DBClientInterface, error)
	transactioner      func() (providers.Transactioner, error)
}

// isMigrationInvocation reports whether the process was started in migration-management mode.
func isMigrationInvocation() bool {
	return flag.Arg(0) == migrationSubcommand
}

// runMigrationCommand executes one migration-management action without initializing normal services.
func runMigrationCommand(
	ctx context.Context,
	args []string,
	cfg *config.Config,
	dbProvider dbprovider.DBProviderInterface,
) (*migrationCommandResult, error) {
	if len(args) < 2 || len(args) > 3 || args[0] != migrationSubcommand {
		return nil, errors.New(migrationCommandUsage())
	}
	if args[1] != migrationInitializeAction && args[1] != migrationStatusAction &&
		args[1] != migrationCheckAction && args[1] != migrationApplyAction &&
		args[1] != migrationResumeAction {
		return nil, fmt.Errorf(
			"unsupported migration action %q; %s", args[1], migrationCommandUsage())
	}
	phase, err := migrationCommandPhase(args)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("run migration %s: configuration is required", args[1])
	}
	if dbProvider == nil {
		return nil, fmt.Errorf("run migration %s: database provider is required", args[1])
	}

	releaseDefinition, err := current.DefinitionWithDependencies(current.Dependencies{})
	if err != nil {
		return nil, fmt.Errorf("run migration %s: %w", args[1], err)
	}
	releaseCatalog, err := current.Catalog()
	if err != nil {
		return nil, fmt.Errorf("run migration %s: load release catalog: %w", args[1], err)
	}
	namespaces, err := migrationNamespaces(cfg, dbProvider)
	if err != nil {
		return nil, fmt.Errorf("run migration %s: %w", args[1], err)
	}

	switch args[1] {
	case migrationInitializeAction:
		results, err := runMigrationInitialize(ctx, releaseDefinition.Manifest, namespaces)
		result := &migrationCommandResult{action: args[1], initialization: results}
		if err != nil {
			return result, err
		}
		return result, nil
	case migrationStatusAction:
		report, err := runMigrationStatus(ctx, releaseDefinition.Manifest, namespaces, releaseCatalog...)
		if err != nil {
			return nil, err
		}
		result := &migrationCommandResult{action: args[1], compatibility: &report}
		if !report.Compatible() {
			return result, errMigrationStatusIncompatible
		}
		return result, nil
	case migrationCheckAction:
		report, err := runMigrationCheck(ctx, *releaseDefinition, namespaces, phase)
		if err != nil {
			return nil, err
		}
		result := &migrationCommandResult{action: args[1], preflight: &report}
		if !report.Ready() {
			return result, errMigrationCheckBlocked
		}
		return result, nil
	case migrationApplyAction:
		if len(args) == 2 {
			workflow, err := runMigrationApplySafe(ctx, *releaseDefinition, namespaces)
			result := &migrationCommandResult{action: args[1], workflow: &workflow}
			if err != nil {
				return result, err
			}
			return result, nil
		}
		var application engine.MigrationRunResult
		if phase == engine.OperationPhaseContract {
			application, err = runMigrationContract(ctx, *releaseDefinition, namespaces)
		} else {
			application, err = runMigrationApply(ctx, *releaseDefinition, namespaces, phase)
		}
		result := &migrationCommandResult{action: args[1], application: &application}
		if err != nil {
			return result, err
		}
		return result, nil
	case migrationResumeAction:
		application, err := runMigrationResume(ctx, *releaseDefinition, namespaces)
		result := &migrationCommandResult{action: args[1], application: &application}
		if err != nil {
			return result, err
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported migration action %q", args[1])
	}
}

func runMigrationApplySafe(
	ctx context.Context,
	release engine.ReleaseDefinition,
	namespaces []migrationNamespace,
) (engine.MigrationWorkflowResult, error) {
	executionNamespaces, err := migrationExecutionNamespaces(namespaces, "apply")
	if err != nil {
		return engine.MigrationWorkflowResult{}, err
	}
	coordinator, err := engine.NewMigrationCoordinator(release, executionNamespaces)
	if err != nil {
		return engine.MigrationWorkflowResult{}, fmt.Errorf("create migration coordinator: %w", err)
	}
	result, err := coordinator.ApplySafe(ctx, engine.MigrationSelection{})
	if err != nil {
		return result, fmt.Errorf("apply safe migration: %w", err)
	}
	return result, nil
}

func migrationCommandUsage() string {
	return "usage: thunderid migration <initialize|status|check|apply|resume> [expand|migrate|contract]"
}

func migrationCommandPhase(args []string) (engine.OperationPhase, error) {
	if len(args) == 2 {
		return engine.OperationPhaseExpand, nil
	}
	if args[1] != migrationCheckAction && args[1] != migrationApplyAction {
		return "", fmt.Errorf("migration action %q does not accept a phase; %s", args[1], migrationCommandUsage())
	}
	phase := engine.OperationPhase(args[2])
	switch phase {
	case engine.OperationPhaseExpand, engine.OperationPhaseMigrate:
		if len(args) != 3 {
			return "", errors.New(migrationCommandUsage())
		}
		return phase, nil
	case engine.OperationPhaseContract:
		if len(args) == 3 {
			return phase, nil
		}
		return "", errors.New(migrationCommandUsage())
	default:
		return "", fmt.Errorf("unsupported migration phase %q; %s", args[2], migrationCommandUsage())
	}
}

func runMigrationContract(
	ctx context.Context,
	release engine.ReleaseDefinition,
	namespaces []migrationNamespace,
) (engine.MigrationRunResult, error) {
	executionNamespaces, err := migrationExecutionNamespaces(namespaces, "contract")
	if err != nil {
		return engine.MigrationRunResult{}, err
	}
	coordinator, err := engine.NewMigrationCoordinator(release, executionNamespaces)
	if err != nil {
		return engine.MigrationRunResult{}, fmt.Errorf("create migration coordinator: %w", err)
	}
	result, err := coordinator.ApplyContract(ctx, engine.MigrationSelection{})
	if err != nil {
		return result, fmt.Errorf("apply contract migration: %w", err)
	}
	return result, nil
}

func runMigrationResume(
	ctx context.Context,
	release engine.ReleaseDefinition,
	namespaces []migrationNamespace,
) (engine.MigrationRunResult, error) {
	executionNamespaces, err := migrationExecutionNamespaces(namespaces, "resume")
	if err != nil {
		return engine.MigrationRunResult{}, err
	}
	coordinator, err := engine.NewMigrationCoordinator(release, executionNamespaces)
	if err != nil {
		return engine.MigrationRunResult{}, fmt.Errorf("create migration coordinator: %w", err)
	}
	result, err := coordinator.Resume(ctx)
	if err != nil {
		return result, fmt.Errorf("resume migration: %w", err)
	}
	return result, nil
}

func runMigrationApply(
	ctx context.Context,
	release engine.ReleaseDefinition,
	namespaces []migrationNamespace,
	phase engine.OperationPhase,
) (engine.MigrationRunResult, error) {
	executionNamespaces, err := migrationExecutionNamespaces(namespaces, "apply")
	if err != nil {
		return engine.MigrationRunResult{}, err
	}

	coordinator, err := engine.NewMigrationCoordinator(release, executionNamespaces)
	if err != nil {
		return engine.MigrationRunResult{}, fmt.Errorf("create migration coordinator: %w", err)
	}
	result, err := coordinator.ApplyPhase(ctx, phase, engine.MigrationSelection{})
	if err != nil {
		return result, fmt.Errorf("apply migration: %w", err)
	}
	return result, nil
}

func migrationExecutionNamespaces(
	namespaces []migrationNamespace,
	action string,
) ([]engine.MigrationNamespace, error) {
	executionNamespaces := make([]engine.MigrationNamespace, 0, len(namespaces))
	for _, namespace := range namespaces {
		client, err := namespace.client()
		if err != nil {
			return nil, fmt.Errorf("%s migration for stores %v: get database client: %w", action,
				namespace.storeIDs, err)
		}
		transactioner, err := namespace.transactioner()
		if err != nil {
			return nil, fmt.Errorf("%s migration for stores %v: get transactioner: %w", action,
				namespace.storeIDs, err)
		}
		locker, err := engine.NewMigrationExecutionLocker(client, namespace.databaseType)
		if err != nil {
			return nil, fmt.Errorf("%s migration for stores %v: get execution locker: %w", action,
				namespace.storeIDs, err)
		}
		executionNamespaces = append(executionNamespaces, engine.MigrationNamespace{
			StoreIDs:     append([]engine.StoreID(nil), namespace.storeIDs...),
			DatabaseType: namespace.databaseType, Client: client, Transactioner: transactioner, Locker: locker,
		})
	}
	return executionNamespaces, nil
}

func runStartupCompatibilityCheck(
	ctx context.Context,
	cfg *config.Config,
	dbProvider dbprovider.DBProviderInterface,
) (*engine.CompatibilityReport, error) {
	if cfg == nil {
		return nil, errors.New("check startup migration compatibility: configuration is required")
	}
	if dbProvider == nil {
		return nil, errors.New("check startup migration compatibility: database provider is required")
	}
	if !startupCompatibilityCheckSupported(cfg) {
		return nil, nil
	}
	releaseDefinition, err := current.Definition()
	if err != nil {
		return nil, fmt.Errorf("check startup migration compatibility: %w", err)
	}
	releaseCatalog, err := current.Catalog()
	if err != nil {
		return nil, fmt.Errorf("check startup migration compatibility: load release catalog: %w", err)
	}
	namespaces, err := migrationNamespaces(cfg, dbProvider)
	if err != nil {
		return nil, fmt.Errorf("check startup migration compatibility: %w", err)
	}
	report, err := runMigrationStatus(ctx, releaseDefinition.Manifest, namespaces, releaseCatalog...)
	if err != nil {
		return nil, fmt.Errorf("check startup migration compatibility: %w", err)
	}
	return &report, nil
}

func startupCompatibilityCheckSupported(cfg *config.Config) bool {
	dataSources := []config.DataSource{
		cfg.Database.Config,
		cfg.Database.Entity,
		cfg.Database.RuntimeTransient,
		cfg.Database.RuntimePersistent,
	}
	for _, dataSource := range dataSources {
		if _, supported := migrationDatabaseAdapters[dataSource.Type]; !supported {
			return false
		}
	}
	return true
}

func runMigrationInitialize(
	ctx context.Context,
	manifest engine.ReleaseManifest,
	namespaces []migrationNamespace,
) ([]engine.InitializationResult, error) {
	results := make([]engine.InitializationResult, 0, len(manifest.Stores))
	for _, namespace := range namespaces {
		client, err := namespace.client()
		if err != nil {
			return results, fmt.Errorf("initialize stores %v: get database client: %w", namespace.storeIDs, err)
		}
		transactioner, err := namespace.transactioner()
		if err != nil {
			return results, fmt.Errorf("initialize stores %v: get transactioner: %w", namespace.storeIDs, err)
		}
		metadataStore, err := engine.NewMetadataStore(client, namespace.databaseType)
		if err != nil {
			return results, fmt.Errorf("initialize stores %v: %w", namespace.storeIDs, err)
		}
		initializer, err := engine.NewInitializer(manifest, metadataStore, transactioner)
		if err != nil {
			return results, fmt.Errorf("initialize stores %v: %w", namespace.storeIDs, err)
		}
		namespaceResults, err := initializer.Initialize(ctx, namespace.storeIDs)
		if err != nil {
			return results, fmt.Errorf("initialize stores %v: %w", namespace.storeIDs, err)
		}
		results = append(results, namespaceResults...)
	}
	return results, nil
}

func runMigrationStatus(
	ctx context.Context,
	manifest engine.ReleaseManifest,
	namespaces []migrationNamespace,
	releaseCatalog ...engine.ReleaseManifest,
) (engine.CompatibilityReport, error) {
	metadataStores, _, err := migrationReadResources(namespaces)
	if err != nil {
		return engine.CompatibilityReport{}, err
	}

	gate, err := engine.NewCompatibilityGate(manifest, releaseCatalog...)
	if err != nil {
		return engine.CompatibilityReport{}, fmt.Errorf("create migration compatibility gate: %w", err)
	}
	return gate.Check(ctx, metadataStores), nil
}

func runMigrationCheck(
	ctx context.Context,
	release engine.ReleaseDefinition,
	namespaces []migrationNamespace,
	phase engine.OperationPhase,
) (engine.PreflightReport, error) {
	metadataStores, stores, err := migrationReadResources(namespaces)
	if err != nil {
		return engine.PreflightReport{}, err
	}
	checker, err := engine.NewPreflightChecker(release, stores)
	if err != nil {
		return engine.PreflightReport{}, fmt.Errorf("create migration preflight checker: %w", err)
	}
	return checker.CheckPhase(ctx, metadataStores, phase)
}

func migrationReadResources(
	namespaces []migrationNamespace,
) (map[engine.StoreID]*engine.MetadataStore, map[engine.StoreID]engine.ReadStore, error) {
	metadataStores := make(map[engine.StoreID]*engine.MetadataStore)
	stores := make(map[engine.StoreID]engine.ReadStore)
	for _, namespace := range namespaces {
		client, err := namespace.client()
		if err != nil {
			return nil, nil, fmt.Errorf("inspect stores %v: get database client: %w",
				namespace.storeIDs, err)
		}
		metadataStore, err := engine.NewMetadataStore(client, namespace.databaseType)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect stores %v: %w", namespace.storeIDs, err)
		}
		for _, storeID := range namespace.storeIDs {
			metadataStores[storeID] = metadataStore
			stores[storeID] = engine.ReadStore{DatabaseType: namespace.databaseType, Client: client}
		}
	}
	return metadataStores, stores, nil
}

func migrationNamespaces(
	cfg *config.Config,
	dbProvider dbprovider.DBProviderInterface,
) ([]migrationNamespace, error) {
	bindings := []migrationStoreBinding{
		{
			storeID: engine.StoreIDConfigDB, dataSource: cfg.Database.Config,
			client: dbProvider.GetConfigDBClient, transactioner: dbProvider.GetConfigDBTransactioner,
		},
		{
			storeID: engine.StoreIDEntityDB, dataSource: cfg.Database.Entity,
			client: dbProvider.GetEntityDBClient, transactioner: dbProvider.GetEntityDBTransactioner,
		},
		{
			storeID: engine.StoreIDRuntimeTransient, dataSource: cfg.Database.RuntimeTransient,
			client:        dbProvider.GetRuntimeTransientDBClient,
			transactioner: dbProvider.GetRuntimeTransientDBTransactioner,
		},
		{
			storeID: engine.StoreIDRuntimePersistent, dataSource: cfg.Database.RuntimePersistent,
			client:        dbProvider.GetRuntimePersistentDBClient,
			transactioner: dbProvider.GetRuntimePersistentDBTransactioner,
		},
	}

	namespaces := make([]migrationNamespace, 0, len(bindings))
	namespaceIndexes := make(map[migrationNamespaceKey]int, len(bindings))
	for _, binding := range bindings {
		adapter, supported := migrationDatabaseAdapters[binding.dataSource.Type]
		if !supported {
			return nil, fmt.Errorf("store %q uses unsupported migration database type %q",
				binding.storeID, binding.dataSource.Type)
		}
		key := adapter.namespaceKey(binding.dataSource)
		if index, exists := namespaceIndexes[key]; exists {
			if namespaces[index].connectionIdentity != adapter.connectionIdentity(binding.dataSource) {
				return nil, fmt.Errorf(
					"stores %v and %q share database namespace %q with different connection identities",
					namespaces[index].storeIDs, binding.storeID, key.location)
			}
			namespaces[index].storeIDs = append(namespaces[index].storeIDs, binding.storeID)
			continue
		}
		namespaceIndexes[key] = len(namespaces)
		namespaces = append(namespaces, migrationNamespace{
			storeIDs: []engine.StoreID{binding.storeID}, databaseType: adapter.databaseType,
			connectionIdentity: adapter.connectionIdentity(binding.dataSource),
			client:             binding.client, transactioner: binding.transactioner,
		})
	}
	return namespaces, nil
}
