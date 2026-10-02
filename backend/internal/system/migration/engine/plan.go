// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// OperationID is the stable identifier of one operation within a migration plan.
type OperationID string

// InterruptionMode defines how the engine may recover an interrupted operation.
type InterruptionMode string

const (
	// InterruptionModeAtomic requires the operation to complete in one transaction.
	InterruptionModeAtomic InterruptionMode = "atomic"
	// InterruptionModeResumable allows the operation to continue from durable progress.
	InterruptionModeResumable InterruptionMode = "resumable"
)

// ScopeType defines the unit of progress tracked for an operation.
type ScopeType string

const (
	// ScopeTypeStore tracks one operation for its complete logical store.
	ScopeTypeStore ScopeType = "store"
	// ScopeTypeDeployment tracks independent operation progress for each deployment.
	ScopeTypeDeployment ScopeType = "deployment"
	// ScopeTypeBatch tracks independent operation progress for stable data partitions.
	ScopeTypeBatch ScopeType = "batch"
)

// OperationDefinition declares the static contract of one migration operation.
type OperationDefinition struct {
	ID               OperationID
	Description      string
	StoreID          StoreID
	ReadStoreIDs     []StoreID
	InterruptionMode InterruptionMode
	ScopeType        ScopeType
}

// Operation exposes the static definition and read-only readiness check required by the engine.
type Operation interface {
	Definition() OperationDefinition
	CheckReadiness(context.Context, ReadAccess, PlanContext) (ReadinessResult, error)
	ResolveScopes(context.Context, ReadAccess) ([]Scope, error)
	PreVerify(context.Context, ReadAccess, Scope) (VerificationResult, error)
	Execute(context.Context, OperationExecution) error
	PostVerify(context.Context, ReadAccess, Scope) (VerificationResult, error)
}

// MigrationPlan declares the ordered operations compiled into one target release.
type MigrationPlan struct {
	TargetVersion string
	Operations    []Operation
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
	plan.Operations = append([]Operation(nil), plan.Operations...)
	return &PlanRegistry{plan: plan}, nil
}

// Plan returns the registered plan with an independent operation slice.
func (r *PlanRegistry) Plan() MigrationPlan {
	plan := r.plan
	plan.Operations = append([]Operation(nil), r.plan.Operations...)
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

	operationIDs := make(map[OperationID]struct{}, len(plan.Operations))
	for index, operation := range plan.Operations {
		if operation == nil {
			return fmt.Errorf("operation at index %d is nil", index)
		}
		definition := operation.Definition()
		if err := validateOperationDefinition(definition, manifestStores); err != nil {
			return fmt.Errorf("operation at index %d: %w", index, err)
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
	if definition.InterruptionMode != InterruptionModeAtomic &&
		definition.InterruptionMode != InterruptionModeResumable {
		return fmt.Errorf("operation %q has unsupported interruption mode %q",
			definition.ID, definition.InterruptionMode)
	}
	if definition.ScopeType != ScopeTypeStore && definition.ScopeType != ScopeTypeDeployment &&
		definition.ScopeType != ScopeTypeBatch {
		return fmt.Errorf("operation %q has unsupported scope type %q", definition.ID, definition.ScopeType)
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
