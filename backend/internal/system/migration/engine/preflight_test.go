// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

type readinessTestOperation struct {
	definition OperationDefinition
	check      func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error)
}

func (o readinessTestOperation) Definition() OperationDefinition {
	return o.definition
}

func (o readinessTestOperation) CheckReadiness(
	ctx context.Context,
	access ReadAccess,
	planContext PlanContext,
) (ReadinessResult, error) {
	return o.check(ctx, access, planContext)
}

func (o readinessTestOperation) ResolveTargets(
	context.Context,
	ReadAccess,
	MigrationSelection,
) ([]ExecutionTarget, error) {
	return []ExecutionTarget{{}}, nil
}

func (o readinessTestOperation) PreVerify(
	context.Context,
	ReadAccess,
	ExecutionTarget,
) (VerificationResult, error) {
	return VerificationResult{State: VerificationSource, Summary: "Test source is valid"}, nil
}

func (o readinessTestOperation) Execute(context.Context, OperationExecution) error {
	return nil
}

func (o readinessTestOperation) PostVerify(
	context.Context,
	ReadAccess,
	ExecutionTarget,
) (VerificationResult, error) {
	return VerificationResult{State: VerificationTarget, Summary: "Test target is valid"}, nil
}

func TestPlanContextDescribesPrecedingOperations(t *testing.T) {
	plan := MigrationPlan{
		TargetVersion: "2.0.0",
		Expand: []Operation{
			testOperation{definition: OperationDefinition{ID: "first"}},
			testOperation{definition: OperationDefinition{ID: "second"}},
		},
		Migrate: []Operation{testOperation{definition: OperationDefinition{ID: "third"}}},
	}

	planContext := newPlanContext(plan, 2)

	assert.Equal(t, "2.0.0", planContext.TargetVersion())
	assert.Equal(t, OperationPhaseMigrate, planContext.Phase())
	assert.Equal(t, 2, planContext.OperationIndex())
	assert.Equal(t, []OperationID{"first", "second"}, planContext.PrecedingOperationIDs())
	assert.True(t, planContext.Precedes("first"))
	assert.False(t, planContext.Precedes("third"))

	preceding := planContext.PrecedingOperationIDs()
	preceding[0] = "changed"
	assert.Equal(t, []OperationID{"first", "second"}, planContext.PrecedingOperationIDs())
}

func TestReadinessResultAcceptsValidStates(t *testing.T) {
	planContext := PlanContext{precedingOperationIDs: []OperationID{"first"}}
	tests := []ReadinessResult{
		{State: ReadinessReady, Summary: "All observable requirements are satisfied"},
		{
			State: ReadinessReady, Summary: "Ready with an advisory",
			Findings: []Finding{{Code: "large_table", Message: "The operation may take several minutes"}},
		},
		{
			State: ReadinessDeferred, Summary: "The target column is created by an earlier operation",
			DeferredUntil: []OperationID{"first"},
		},
		{
			State: ReadinessBlocked, Summary: "Duplicate source values prevent the unique constraint",
			Findings: []Finding{{
				Code: "duplicate_values", Message: "Duplicate values were found",
				Remediation: "Remove duplicate values before applying the migration",
			}},
		},
	}

	for _, result := range tests {
		require.NoError(t, result.validate(planContext))
	}
}

func TestReadinessResultRejectsInvalidResults(t *testing.T) {
	planContext := PlanContext{precedingOperationIDs: []OperationID{"first"}}
	tests := []struct {
		name       string
		result     ReadinessResult
		errorMatch string
	}{
		{
			name: "unsupported state", result: ReadinessResult{State: "unknown", Summary: "Unknown"},
			errorMatch: "unsupported readiness state",
		},
		{
			name: "missing summary", result: ReadinessResult{State: ReadinessReady},
			errorMatch: "summary is required",
		},
		{
			name: "missing finding code",
			result: ReadinessResult{State: ReadinessBlocked, Summary: "Blocked",
				Findings: []Finding{{Message: "A problem was found"}}},
			errorMatch: "code is required",
		},
		{
			name: "missing finding message",
			result: ReadinessResult{State: ReadinessBlocked, Summary: "Blocked",
				Findings: []Finding{{Code: "problem"}}},
			errorMatch: "message is required",
		},
		{
			name:       "blocked without finding",
			result:     ReadinessResult{State: ReadinessBlocked, Summary: "Blocked"},
			errorMatch: "requires at least one finding",
		},
		{
			name: "ready with deferred operation",
			result: ReadinessResult{State: ReadinessReady, Summary: "Ready",
				DeferredUntil: []OperationID{"first"}},
			errorMatch: "cannot declare deferred operations",
		},
		{
			name:       "deferred without operation",
			result:     ReadinessResult{State: ReadinessDeferred, Summary: "Deferred"},
			errorMatch: "requires at least one preceding operation",
		},
		{
			name: "deferred until nonpreceding operation",
			result: ReadinessResult{State: ReadinessDeferred, Summary: "Deferred",
				DeferredUntil: []OperationID{"later"}},
			errorMatch: "does not precede",
		},
		{
			name: "duplicate deferred operation",
			result: ReadinessResult{State: ReadinessDeferred, Summary: "Deferred",
				DeferredUntil: []OperationID{"first", "first"}},
			errorMatch: "repeats operation",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.result.validate(planContext)

			require.Error(t, err)
			assert.ErrorContains(t, err, test.errorMatch)
		})
	}
}

func TestOperationReadAccessEnforcesDeclaredStoresAndProviderQueries(t *testing.T) {
	entityClient := providermock.NewDBClientInterfaceMock(t)
	configClient := providermock.NewDBClientInterfaceMock(t)
	query := dbmodel.DBQuery{ID: "MIG-TEST-01", PostgresQuery: "SELECT 1"}
	entityClient.On("QueryContext", mock.Anything, query, "argument").
		Return([]map[string]interface{}{{"value": int64(1)}}, nil).Once()
	access, err := newOperationReadAccess(OperationDefinition{
		StoreID: StoreIDEntityDB, ReadStoreIDs: []StoreID{StoreIDConfigDB},
	}, map[StoreID]ReadStore{
		StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
		StoreIDConfigDB: {DatabaseType: DatabaseTypePostgres, Client: configClient},
	})
	require.NoError(t, err)

	rows, err := access.Query(context.Background(), StoreIDEntityDB, query, "argument")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows[0]["value"])
	databaseType, err := access.DatabaseType(StoreIDConfigDB)
	require.NoError(t, err)
	assert.Equal(t, DatabaseTypePostgres, databaseType)

	_, err = access.Query(context.Background(), StoreIDRuntimePersistent, query)
	assert.ErrorContains(t, err, "did not declare read access")
	_, err = access.DatabaseType(StoreIDRuntimePersistent)
	assert.ErrorContains(t, err, "did not declare read access")

	unsupportedQuery := dbmodel.DBQuery{ID: "MIG-TEST-02"}
	_, err = access.Query(context.Background(), StoreIDEntityDB, unsupportedQuery)
	assert.ErrorContains(t, err, "has no implementation")
}

func TestNewOperationReadAccessRejectsMissingConfiguration(t *testing.T) {
	definition := OperationDefinition{StoreID: StoreIDEntityDB, ReadStoreIDs: []StoreID{StoreIDConfigDB}}
	entityClient := providermock.NewDBClientInterfaceMock(t)

	access, err := newOperationReadAccess(definition, map[StoreID]ReadStore{
		StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
	})
	require.Error(t, err)
	assert.Nil(t, access)
	assert.ErrorContains(t, err, string(StoreIDConfigDB))

	access, err = newOperationReadAccess(OperationDefinition{StoreID: StoreIDEntityDB}, map[StoreID]ReadStore{
		StoreIDEntityDB: {Client: entityClient},
	})
	require.Error(t, err)
	assert.Nil(t, access)
	assert.ErrorContains(t, err, "database type")
}

func TestPreflightCheckerAggregatesReadinessAndDependencyFailures(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	called := 0
	ready := readinessTestOperation{
		definition: readinessDefinition("first"),
		check: func(_ context.Context, _ ReadAccess, planContext PlanContext) (ReadinessResult, error) {
			called++
			assert.Equal(t, 0, planContext.OperationIndex())
			return ReadinessResult{State: ReadinessReady, Summary: "Ready"}, nil
		},
	}
	blocked := readinessTestOperation{
		definition: readinessDefinition("second"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			called++
			return ReadinessResult{
				State: ReadinessBlocked, Summary: "Source data is invalid",
				Findings: []Finding{{Code: "invalid_source", Message: "Invalid source data was found"}},
			}, nil
		},
	}
	deferred := readinessTestOperation{
		definition: readinessDefinition("third"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			called++
			return ReadinessResult{
				State: ReadinessDeferred, Summary: "Requires the second operation",
				DeferredUntil: []OperationID{"second"},
			}, nil
		},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Migrate:       []Operation{ready, blocked, deferred},
	}}
	checker, err := newReadinessEvaluator(release, map[StoreID]ReadStore{
		StoreIDEntityDB: {
			DatabaseType: DatabaseTypePostgres,
			Client:       providermock.NewDBClientInterfaceMock(t),
		},
	})
	require.NoError(t, err)

	report := checker.check(context.Background())

	assert.False(t, report.Ready())
	assert.Equal(t, manifest.ReleaseVersion, report.TargetVersion)
	assert.Equal(t, 3, called)
	require.Len(t, report.Operations, 3)
	assert.True(t, report.Operations[0].Ready())
	assert.Equal(t, ReadinessBlocked, report.Operations[1].State)
	assert.Equal(t, "invalid_source", report.Operations[1].Findings[0].Code)
	assert.Equal(t, ReadinessBlocked, report.Operations[2].State)
	assert.Equal(t, "preceding_operation_blocked", report.Operations[2].Findings[0].Code)
}

func TestPreflightCheckerConvertsCheckErrorsAndInvalidResultsToBlockers(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	checkFailure := readinessTestOperation{
		definition: readinessDefinition("first"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			return ReadinessResult{}, errors.New("query failed")
		},
	}
	invalidResult := readinessTestOperation{
		definition: readinessDefinition("second"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			return ReadinessResult{State: ReadinessReady}, nil
		},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Migrate:       []Operation{checkFailure, invalidResult},
	}}
	checker, err := newReadinessEvaluator(release, map[StoreID]ReadStore{
		StoreIDEntityDB: {
			DatabaseType: DatabaseTypePostgres,
			Client:       providermock.NewDBClientInterfaceMock(t),
		},
	})
	require.NoError(t, err)

	report := checker.check(context.Background())

	assert.False(t, report.Ready())
	require.Len(t, report.Operations, 2)
	assert.ErrorContains(t, report.Operations[0].Err, "query failed")
	assert.Equal(t, "readiness_check_failed", report.Operations[0].Findings[0].Code)
	assert.ErrorContains(t, report.Operations[1].Err, "summary is required")
	assert.Equal(t, "invalid_readiness_result", report.Operations[1].Findings[0].Code)
}

func TestPreflightCheckerAcceptsEmptyPlan(t *testing.T) {
	manifest := validTestManifest()
	checker, err := newReadinessEvaluator(ReleaseDefinition{
		Manifest: manifest,
		Plan:     MigrationPlan{TargetVersion: manifest.ReleaseVersion},
	}, nil)
	require.NoError(t, err)

	report := checker.check(context.Background())

	assert.True(t, report.Ready())
	assert.Empty(t, report.Operations)
}

func TestReadinessEvaluatorDefaultsOperationWithoutCheckerToReady(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Migrate:       []Operation{validPlanOperation(StoreIDEntityDB)},
	}}
	evaluator, err := newReadinessEvaluator(release, nil)
	require.NoError(t, err)

	report := evaluator.check(context.Background())

	assert.True(t, report.Ready())
	require.Len(t, report.Operations, 1)
	assert.Equal(t, ReadinessReady, report.Operations[0].State)
	assert.Equal(t, "No additional operation readiness check is required", report.Operations[0].Summary)
}

func TestMigrationPreflightCheckerAcceptsSourceAndRunsAffectedOperations(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	operationCalled := false
	operation := readinessTestOperation{
		definition: readinessDefinition("migrate-entitydb"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			operationCalled = true
			return ReadinessResult{State: ReadinessReady, Summary: "Entity migration is ready"}, nil
		},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion, Migrate: []Operation{operation},
	}}
	metadataStores, stores := preflightStores(t, manifest, map[StoreID]string{
		StoreIDEntityDB: "1.0.0",
	})
	checker, err := NewPreflightChecker(release, stores)
	require.NoError(t, err)

	report := checker.Check(context.Background(), metadataStores)

	assert.True(t, report.Ready())
	assert.True(t, operationCalled)
	require.Len(t, report.Stores, 4)
	assert.Equal(t, StoreAssessmentMigrationRequired, report.Stores[1].State)
	require.Len(t, report.Operations, 1)
	assert.Equal(t, ReadinessReady, report.Operations[0].State)
}

func TestMigrationPreflightCheckerChecksSafeWorkflowWithoutContract(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	expandCalled := false
	migrateCalled := false
	contractCalled := false
	operation := func(id OperationID, called *bool) readinessTestOperation {
		definition := readinessDefinition(id)
		return readinessTestOperation{
			definition: definition,
			check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
				*called = true
				return ReadinessResult{State: ReadinessReady, Summary: "Operation is ready"}, nil
			},
		}
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{operation("expand-entitydb", &expandCalled)},
		Migrate:       []Operation{operation("migrate-entitydb", &migrateCalled)},
		Contract:      []Operation{operation("contract-entitydb", &contractCalled)},
	}}
	metadataStores, stores := preflightStores(t, manifest, map[StoreID]string{
		StoreIDEntityDB: "1.0.0",
	})
	checker, err := NewPreflightChecker(release, stores)
	require.NoError(t, err)

	report := checker.CheckSafeWorkflow(context.Background(), metadataStores)

	assert.True(t, report.Ready())
	assert.True(t, expandCalled)
	assert.True(t, migrateCalled)
	assert.False(t, contractCalled)
	require.Len(t, report.Phases, 2)
	assert.Equal(t, OperationPhaseExpand, report.Phases[0].Phase)
	require.Len(t, report.Phases[0].Operations, 1)
	assert.Equal(t, OperationID("expand-entitydb"), report.Phases[0].Operations[0].OperationID)
	assert.Equal(t, OperationPhaseMigrate, report.Phases[1].Phase)
	require.Len(t, report.Phases[1].Operations, 1)
	assert.Equal(t, OperationID("migrate-entitydb"), report.Phases[1].Operations[0].OperationID)
}

func TestMigrationPreflightCheckerSkipsOperationsWhenStoreIsBlocked(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	operationCalled := false
	operation := readinessTestOperation{
		definition: readinessDefinition("migrate-entitydb"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			operationCalled = true
			return ReadinessResult{State: ReadinessReady, Summary: "Entity migration is ready"}, nil
		},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion, Migrate: []Operation{operation},
	}}
	metadataStores, stores := preflightStores(t, manifest, map[StoreID]string{
		StoreIDEntityDB: "0.9.0",
	})
	checker, err := NewPreflightChecker(release, stores)
	require.NoError(t, err)

	report := checker.Check(context.Background(), metadataStores)

	assert.False(t, report.Ready())
	assert.False(t, operationCalled)
	assert.Equal(t, StoreAssessmentBlocked, report.Stores[1].State)
	assert.Equal(t, CompatibilityReasonLevelMismatch, report.Stores[1].Reason)
	assert.Empty(t, report.Operations)
}

func TestMigrationPreflightCheckerContinuesIndependentOperations(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	for index := range manifest.Stores {
		if manifest.Stores[index].ID == StoreIDConfigDB {
			manifest.Stores[index].Changed = true
			manifest.Stores[index].RequiredLevel = manifest.ReleaseVersion
		}
	}
	entityOperationCalled := false
	configOperation := readinessTestOperation{
		definition: OperationDefinition{
			ID: "migrate-configdb", Description: "Migrate configdb", StoreID: StoreIDConfigDB,
			Kind:             OperationKindDataChange,
			InterruptionMode: InterruptionModeAtomic,
		},
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			return ReadinessResult{State: ReadinessReady, Summary: "Config migration is ready"}, nil
		},
	}
	entityOperation := readinessTestOperation{
		definition: readinessDefinition("migrate-entitydb"),
		check: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			entityOperationCalled = true
			return ReadinessResult{State: ReadinessReady, Summary: "Entity migration is ready"}, nil
		},
	}
	release := ReleaseDefinition{Manifest: manifest, Plan: MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Migrate:       []Operation{configOperation, entityOperation},
	}}
	metadataStores, stores := preflightStores(t, manifest, map[StoreID]string{
		StoreIDConfigDB: "0.9.0",
		StoreIDEntityDB: "1.0.0",
	})
	checker, err := NewPreflightChecker(release, stores)
	require.NoError(t, err)

	report := checker.Check(context.Background(), metadataStores)

	assert.False(t, report.Ready())
	assert.True(t, entityOperationCalled)
	require.Len(t, report.Operations, 1)
	assert.Equal(t, OperationID("migrate-entitydb"), report.Operations[0].OperationID)
}

func preflightStores(
	t *testing.T,
	manifest ReleaseManifest,
	levelOverrides map[StoreID]string,
) (map[StoreID]*MetadataStore, map[StoreID]ReadStore) {
	t.Helper()
	metadataStores := make(map[StoreID]*MetadataStore, len(manifest.Stores))
	stores := make(map[StoreID]ReadStore, len(manifest.Stores))
	for _, requirement := range manifest.Stores {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		expectReadySchema(dbClient)
		level := requirement.RequiredLevel
		if override, exists := levelOverrides[requirement.ID]; exists {
			level = override
		}
		dbClient.On("QueryContext", mock.Anything, queryGetMigrationState, string(requirement.ID)).
			Return([]map[string]interface{}{compatibilityStateRow(requirement.ID, level)}, nil).Once()
		metadataStores[requirement.ID] = newTestMetadataStore(t, dbClient)
		stores[requirement.ID] = ReadStore{DatabaseType: DatabaseTypePostgres, Client: dbClient}
	}
	return metadataStores, stores
}

func readinessDefinition(operationID OperationID) OperationDefinition {
	return OperationDefinition{
		ID: operationID, Description: "Test operation", StoreID: StoreIDEntityDB,
		Kind:             OperationKindDataChange,
		InterruptionMode: InterruptionModeAtomic,
	}
}
