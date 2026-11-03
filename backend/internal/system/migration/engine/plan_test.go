// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testOperation struct {
	definition OperationDefinition
}

type resumableTestOperation struct {
	testOperation
}

func (resumableTestOperation) ExecuteBatch(
	context.Context,
	OperationExecution,
	json.RawMessage,
) (ResumableBatchResult, error) {
	return ResumableBatchResult{Completed: true}, nil
}

type rollbackTestOperation struct {
	resumableTestOperation
}

func (rollbackTestOperation) Rollback(context.Context, OperationExecution, error) error {
	return nil
}

func (o testOperation) Definition() OperationDefinition {
	return o.definition
}

func (o testOperation) ResolveTargets(
	context.Context,
	ReadAccess,
	MigrationSelection,
) ([]ExecutionTarget, error) {
	return []ExecutionTarget{{}}, nil
}

func (o testOperation) PreVerify(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error) {
	return VerificationResult{State: VerificationSource, Summary: "Test source is valid"}, nil
}

func (o testOperation) Execute(context.Context, OperationExecution) error {
	return nil
}

func (o testOperation) PostVerify(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error) {
	return VerificationResult{State: VerificationTarget, Summary: "Test target is valid"}, nil
}

func TestPlanRegistryAcceptsMatchingPlan(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	operation := validPlanOperation(StoreIDEntityDB)
	plan := MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand:        []Operation{operation},
	}

	registry, err := NewPlanRegistry(manifest, plan)

	require.NoError(t, err)
	require.NotNil(t, registry)
	registered := registry.Plan()
	assert.Equal(t, plan.TargetVersion, registered.TargetVersion)
	require.Len(t, registered.Expand, 1)
	assert.Equal(t, operation.Definition(), registered.Expand[0].Definition())

	registered.Expand = nil
	assert.Len(t, registry.Plan().Expand, 1)
}

func TestPlanRegistryAcceptsNoChangePlan(t *testing.T) {
	manifest := validTestManifest()

	registry, err := NewPlanRegistry(manifest, MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
	})

	require.NoError(t, err)
	assert.Empty(t, registry.Plan().Operations())
}

func TestMigrationPlanReturnsOperationsForPhase(t *testing.T) {
	expand := validPlanOperation(StoreIDEntityDB)
	migrate := validPlanOperation(StoreIDEntityDB)
	migrate.definition.ID = "1.1.0/entitydb/migrate"
	contract := validPlanOperation(StoreIDEntityDB)
	contract.definition.ID = "1.1.0/entitydb/contract"
	plan := MigrationPlan{
		Expand: []Operation{expand}, Migrate: []Operation{migrate}, Contract: []Operation{contract},
	}

	operations, err := plan.OperationsForPhase(OperationPhaseMigrate)

	require.NoError(t, err)
	require.Len(t, operations, 1)
	assert.Equal(t, migrate.Definition(), operations[0].Definition())
	operations[0] = nil
	assert.NotNil(t, plan.Migrate[0])
	assert.Equal(t, []Operation{expand, migrate, contract}, plan.Operations())

	operations, err = plan.OperationsForPhase("unknown")
	require.Error(t, err)
	assert.Nil(t, operations)
}

func TestPlanRegistryAcceptsOptionalRollbackForResumableOperation(t *testing.T) {
	manifest := changedStoreManifest(StoreIDEntityDB)
	operation := validPlanOperation(StoreIDEntityDB)
	operation.definition.InterruptionMode = InterruptionModeResumable

	registry, err := NewPlanRegistry(manifest, MigrationPlan{
		TargetVersion: manifest.ReleaseVersion,
		Expand: []Operation{rollbackTestOperation{
			resumableTestOperation: resumableTestOperation{testOperation: operation},
		}},
	})

	require.NoError(t, err)
	require.Len(t, registry.Plan().Expand, 1)
	_, supportsRollback := registry.Plan().Expand[0].(RollbackCapableOperation)
	assert.True(t, supportsRollback)
}

func TestPlanRegistryRejectsInvalidPlan(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ReleaseManifest, *MigrationPlan)
		errorMatch string
	}{
		{
			name: "invalid manifest",
			mutate: func(manifest *ReleaseManifest, _ *MigrationPlan) {
				manifest.ManifestVersion++
			},
			errorMatch: "invalid release manifest",
		},
		{
			name: "noncanonical target version",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				plan.TargetVersion = "1.1"
			},
			errorMatch: "target version",
		},
		{
			name: "target version mismatch",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				plan.TargetVersion = "1.2.0"
			},
			errorMatch: "manifest release version",
		},
		{
			name: "nil operation",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				plan.Migrate = []Operation{nil}
			},
			errorMatch: "migrate operation at index 0 is nil",
		},
		{
			name: "duplicate operation ID",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				plan.Contract = append(plan.Contract, plan.Expand[0])
			},
			errorMatch: "duplicate operation ID",
		},
		{
			name: "changed store without operation",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				plan.Expand = nil
			},
			errorMatch: "requires at least one",
		},
		{
			name: "operation for unchanged store",
			mutate: func(manifest *ReleaseManifest, plan *MigrationPlan) {
				manifest.Stores[0].Changed = false
				manifest.Stores[0].RequiredLevel = manifest.Stores[0].AcceptedSourceLevel
			},
			errorMatch: "unchanged store",
		},
		{
			name: "rollback on atomic operation",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				operation := plan.Expand[0].(testOperation)
				plan.Expand[0] = rollbackTestOperation{
					resumableTestOperation: resumableTestOperation{testOperation: operation},
				}
			},
			errorMatch: "atomic operations cannot define rollback behavior",
		},
		{
			name: "resumable declaration without implementation",
			mutate: func(_ *ReleaseManifest, plan *MigrationPlan) {
				operation := plan.Expand[0].(testOperation)
				operation.definition.InterruptionMode = InterruptionModeResumable
				plan.Expand[0] = operation
			},
			errorMatch: "does not implement ResumableOperation",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := changedStoreManifest(StoreIDConfigDB)
			plan := MigrationPlan{
				TargetVersion: manifest.ReleaseVersion,
				Expand:        []Operation{validPlanOperation(StoreIDConfigDB)},
			}
			test.mutate(&manifest, &plan)

			registry, err := NewPlanRegistry(manifest, plan)

			require.Error(t, err)
			assert.Nil(t, registry)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func TestPlanRegistryRejectsInvalidOperationDefinition(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*OperationDefinition)
		errorMatch string
	}{
		{name: "missing ID", mutate: func(definition *OperationDefinition) {
			definition.ID = " "
		}, errorMatch: "ID is required"},
		{name: "long ID", mutate: func(definition *OperationDefinition) {
			definition.ID = OperationID(string(make([]byte, maxOperationIDLength+1)))
		}, errorMatch: "ID exceeds"},
		{name: "missing description", mutate: func(definition *OperationDefinition) {
			definition.Description = ""
		}, errorMatch: "description"},
		{name: "unknown writable store", mutate: func(definition *OperationDefinition) {
			definition.StoreID = "unknown"
		}, errorMatch: "targets undeclared"},
		{name: "invalid operation kind", mutate: func(definition *OperationDefinition) {
			definition.Kind = "unknown"
		}, errorMatch: "operation kind"},
		{name: "invalid interruption mode", mutate: func(definition *OperationDefinition) {
			definition.InterruptionMode = "unknown"
		}, errorMatch: "interruption mode"},
		{name: "resumable schema change", mutate: func(definition *OperationDefinition) {
			definition.Kind = OperationKindSchemaChange
			definition.InterruptionMode = InterruptionModeResumable
		}, errorMatch: "resumable execution requires a data-change operation"},
		{name: "unknown read store", mutate: func(definition *OperationDefinition) {
			definition.ReadStoreIDs = []StoreID{"unknown"}
		}, errorMatch: "reads undeclared"},
		{name: "writable store repeated", mutate: func(definition *OperationDefinition) {
			definition.ReadStoreIDs = []StoreID{StoreIDConfigDB}
		}, errorMatch: "repeats its writable store"},
		{name: "duplicate read store", mutate: func(definition *OperationDefinition) {
			definition.ReadStoreIDs = []StoreID{StoreIDEntityDB, StoreIDEntityDB}
		}, errorMatch: "duplicate read store"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := changedStoreManifest(StoreIDConfigDB)
			operation := validPlanOperation(StoreIDConfigDB)
			definition := operation.Definition()
			test.mutate(&definition)
			plan := MigrationPlan{
				TargetVersion: manifest.ReleaseVersion,
				Expand:        []Operation{testOperation{definition: definition}},
			}

			registry, err := NewPlanRegistry(manifest, plan)

			require.Error(t, err)
			assert.Nil(t, registry)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func changedStoreManifest(storeID StoreID) ReleaseManifest {
	manifest := validTestManifest()
	for index := range manifest.Stores {
		if manifest.Stores[index].ID == storeID {
			manifest.Stores[index].Changed = true
			manifest.Stores[index].RequiredLevel = manifest.ReleaseVersion
		}
	}
	return manifest
}

func validPlanOperation(storeID StoreID) testOperation {
	readStoreID := StoreIDEntityDB
	if storeID == readStoreID {
		readStoreID = StoreIDConfigDB
	}
	return testOperation{definition: OperationDefinition{
		ID:               OperationID("1.1.0/" + string(storeID) + "/example"),
		Description:      "Apply the example migration",
		StoreID:          storeID,
		ReadStoreIDs:     []StoreID{readStoreID},
		Kind:             OperationKindDataChange,
		InterruptionMode: InterruptionModeAtomic,
	}}
}
