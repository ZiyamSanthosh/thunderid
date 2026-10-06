// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// OperationID is the stable identifier of one operation within a migration plan.
type OperationID string

// OperationKind describes how an operation affects its owning store.
type OperationKind string

const (
	// OperationKindSchemaChange changes the physical schema once for the complete store.
	OperationKindSchemaChange OperationKind = "schema_change"
	// OperationKindDataChange changes data for targets resolved from the migration selection.
	OperationKindDataChange OperationKind = "data_change"
)

// OperationPhase identifies when an operation runs in the migration lifecycle.
type OperationPhase string

const (
	// OperationPhaseExpand prepares a representation that source and target versions can share.
	OperationPhaseExpand OperationPhase = "expand"
	// OperationPhaseMigrate moves data and live changes into the target representation.
	OperationPhaseMigrate OperationPhase = "migrate"
	// OperationPhaseContract removes the obsolete representation after old binaries are gone.
	OperationPhaseContract OperationPhase = "contract"
)

// InterruptionMode defines how the engine may recover an interrupted operation.
type InterruptionMode string

const (
	// InterruptionModeAtomic requires the operation to complete in one transaction.
	InterruptionModeAtomic InterruptionMode = "atomic"
	// InterruptionModeResumable allows the operation to continue from durable progress.
	InterruptionModeResumable InterruptionMode = "resumable"
)

// MigrationSelection identifies the deployments selected for one migration run.
// An empty deployment list means that the migration targets the complete store.
type MigrationSelection struct {
	DeploymentIDs []string
}

// OperationDefinition declares the static contract of one migration operation.
type OperationDefinition struct {
	ID               OperationID
	Description      string
	StoreID          StoreID
	ReadStoreIDs     []StoreID
	Kind             OperationKind
	InterruptionMode InterruptionMode
}

// Operation exposes the mandatory lifecycle implemented by every migration operation.
type Operation interface {
	Definition() OperationDefinition
	ResolveTargets(context.Context, ReadAccess, MigrationSelection) ([]ExecutionTarget, error)
	PreVerify(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error)
	PostVerify(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error)
}

// AtomicOperation completes one target in a single transaction.
type AtomicOperation interface {
	Operation
	Execute(context.Context, OperationExecution) error
}

// ResumableBatchResult describes the durable outcome of one committed operation batch.
type ResumableBatchResult struct {
	CheckpointData json.RawMessage
	Completed      bool
}

// ResumableOperation advances one target through bounded independently committed batches.
type ResumableOperation interface {
	Operation
	ExecuteBatch(context.Context, OperationExecution, json.RawMessage) (ResumableBatchResult, error)
}

// ReadinessChecker optionally reports operation-specific prerequisites before execution begins.
type ReadinessChecker interface {
	CheckReadiness(context.Context, ReadAccess, PlanContext) (ReadinessResult, error)
}

// RollbackCapableOperation optionally restores changes made by a non-atomic operation target after execution fails.
// The original execution error is provided so the implementation can select the appropriate recovery behavior.
type RollbackCapableOperation interface {
	Operation
	Rollback(context.Context, OperationExecution, error) error
}

// MigrationPlan declares the ordered operations compiled into one target release.
type MigrationPlan struct {
	TargetVersion string
	Expand        []Operation
	Migrate       []Operation
	Contract      []Operation
}

// OperationsForPhase returns an independent slice containing the operations assigned to one phase.
func (p MigrationPlan) OperationsForPhase(phase OperationPhase) ([]Operation, error) {
	var operations []Operation
	switch phase {
	case OperationPhaseExpand:
		operations = p.Expand
	case OperationPhaseMigrate:
		operations = p.Migrate
	case OperationPhaseContract:
		operations = p.Contract
	default:
		return nil, fmt.Errorf("unsupported operation phase %q", phase)
	}
	return append([]Operation(nil), operations...), nil
}

// Operations returns all operations in canonical phase and declaration order.
func (p MigrationPlan) Operations() []Operation {
	planned := p.plannedOperations()
	operations := make([]Operation, 0, len(planned))
	for _, entry := range planned {
		operations = append(operations, entry.operation)
	}
	return operations
}

type plannedOperation struct {
	phase     OperationPhase
	index     int
	order     int
	operation Operation
}

func (p MigrationPlan) plannedOperations() []plannedOperation {
	operations := make([]plannedOperation, 0, len(p.Expand)+len(p.Migrate)+len(p.Contract))
	for _, phase := range []struct {
		id         OperationPhase
		operations []Operation
	}{
		{id: OperationPhaseExpand, operations: p.Expand},
		{id: OperationPhaseMigrate, operations: p.Migrate},
		{id: OperationPhaseContract, operations: p.Contract},
	} {
		for index, operation := range phase.operations {
			operations = append(operations, plannedOperation{
				phase: phase.id, index: index, order: len(operations), operation: operation,
			})
		}
	}
	return operations
}

// ReleaseDefinition binds one validated manifest to its compiled migration plan.
type ReleaseDefinition struct {
	Manifest ReleaseManifest
	Plan     MigrationPlan
}

// NewReleaseDefinition parses and validates one immutable release definition.
func NewReleaseDefinition(manifestData []byte, plan MigrationPlan) (*ReleaseDefinition, error) {
	manifest, err := LoadReleaseManifest(manifestData)
	if err != nil {
		return nil, err
	}
	registry, err := NewPlanRegistry(*manifest, plan)
	if err != nil {
		return nil, err
	}
	return &ReleaseDefinition{Manifest: *manifest, Plan: registry.Plan()}, nil
}

// PlanRegistry holds one manifest-validated migration plan.
type PlanRegistry struct {
	plan MigrationPlan
}

// NewPlanRegistry validates and registers the plan compiled for a release.
func NewPlanRegistry(manifest ReleaseManifest, plan MigrationPlan) (*PlanRegistry, error) {
	if err := validateMigrationPlan(manifest, plan); err != nil {
		return nil, fmt.Errorf("register migration plan: %w", err)
	}
	return &PlanRegistry{plan: cloneMigrationPlan(plan)}, nil
}

// Plan returns the registered plan with an independent operation slice.
func (r *PlanRegistry) Plan() MigrationPlan {
	return cloneMigrationPlan(r.plan)
}

func cloneMigrationPlan(plan MigrationPlan) MigrationPlan {
	plan.Expand = append([]Operation(nil), plan.Expand...)
	plan.Migrate = append([]Operation(nil), plan.Migrate...)
	plan.Contract = append([]Operation(nil), plan.Contract...)
	return plan
}

func validateMigrationPlan(manifest ReleaseManifest, plan MigrationPlan) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid release manifest: %w", err)
	}
	if !isCanonicalVersion(plan.TargetVersion) {
		return fmt.Errorf("target version %q is not canonical semantic version", plan.TargetVersion)
	}
	if plan.TargetVersion != manifest.ReleaseVersion {
		return fmt.Errorf("target version %q does not match manifest release version %q",
			plan.TargetVersion, manifest.ReleaseVersion)
	}

	manifestStores := make(map[StoreID]StoreRequirement, len(manifest.Stores))
	operationCountByStore := make(map[StoreID]int, len(manifest.Stores))
	for _, requirement := range manifest.Stores {
		manifestStores[requirement.ID] = requirement
	}

	plannedOperations := plan.plannedOperations()
	operationIDs := make(map[OperationID]struct{}, len(plannedOperations))
	for _, planned := range plannedOperations {
		operation := planned.operation
		if operation == nil {
			return fmt.Errorf("%s operation at index %d is nil", planned.phase, planned.index)
		}
		definition := operation.Definition()
		if err := validateOperationDefinition(definition, manifestStores); err != nil {
			return fmt.Errorf("%s operation at index %d: %w",
				planned.phase, planned.index, err)
		}
		switch definition.InterruptionMode {
		case InterruptionModeAtomic:
			if _, ok := operation.(AtomicOperation); !ok {
				return fmt.Errorf("operation %q declares atomic execution but does not implement AtomicOperation",
					definition.ID)
			}
		case InterruptionModeResumable:
			if _, ok := operation.(ResumableOperation); !ok {
				return fmt.Errorf("operation %q declares resumable execution but does not implement ResumableOperation",
					definition.ID)
			}
		}
		if _, supportsRollback := operation.(RollbackCapableOperation); supportsRollback &&
			definition.InterruptionMode == InterruptionModeAtomic {
			return fmt.Errorf("operation %q: atomic operations cannot define rollback behavior", definition.ID)
		}
		if _, exists := operationIDs[definition.ID]; exists {
			return fmt.Errorf("duplicate operation ID %q", definition.ID)
		}
		operationIDs[definition.ID] = struct{}{}
		operationCountByStore[definition.StoreID]++
	}

	for _, requirement := range manifest.Stores {
		operationCount := operationCountByStore[requirement.ID]
		if requirement.Changed && operationCount == 0 {
			return fmt.Errorf("changed store %q requires at least one migration operation", requirement.ID)
		}
		if !requirement.Changed && operationCount > 0 {
			return fmt.Errorf("unchanged store %q cannot have migration operations", requirement.ID)
		}
	}
	return nil
}

func validateOperationDefinition(
	definition OperationDefinition,
	manifestStores map[StoreID]StoreRequirement,
) error {
	if strings.TrimSpace(string(definition.ID)) == "" {
		return errors.New("operation ID is required")
	}
	if len(definition.ID) > maxOperationIDLength {
		return fmt.Errorf("operation ID exceeds %d bytes", maxOperationIDLength)
	}
	if strings.TrimSpace(definition.Description) == "" {
		return fmt.Errorf("operation %q description is required", definition.ID)
	}
	if _, exists := manifestStores[definition.StoreID]; !exists {
		return fmt.Errorf("operation %q targets undeclared store %q", definition.ID, definition.StoreID)
	}
	if definition.Kind != OperationKindSchemaChange && definition.Kind != OperationKindDataChange {
		return fmt.Errorf("operation %q has unsupported operation kind %q", definition.ID, definition.Kind)
	}
	if definition.InterruptionMode != InterruptionModeAtomic &&
		definition.InterruptionMode != InterruptionModeResumable {
		return fmt.Errorf("operation %q has unsupported interruption mode %q",
			definition.ID, definition.InterruptionMode)
	}
	if definition.InterruptionMode == InterruptionModeResumable &&
		definition.Kind != OperationKindDataChange {
		return fmt.Errorf("operation %q: resumable execution requires a data-change operation", definition.ID)
	}
	readStores := make(map[StoreID]struct{}, len(definition.ReadStoreIDs))
	for _, storeID := range definition.ReadStoreIDs {
		if _, exists := manifestStores[storeID]; !exists {
			return fmt.Errorf("operation %q reads undeclared store %q", definition.ID, storeID)
		}
		if storeID == definition.StoreID {
			return fmt.Errorf("operation %q repeats its writable store %q as a read store", definition.ID, storeID)
		}
		if _, exists := readStores[storeID]; exists {
			return fmt.Errorf("operation %q has duplicate read store %q", definition.ID, storeID)
		}
		readStores[storeID] = struct{}{}
	}
	return nil
}
