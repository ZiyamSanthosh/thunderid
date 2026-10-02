// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// ErrFinalizationConflict means that migration state changed before it could be finalized.
var ErrFinalizationConflict = errors.New("migration store finalization conflict")

// FinalizationResult describes one store after a finalization attempt.
type FinalizationResult struct {
	State           State
	Finalized       bool
	AlreadyAtTarget bool
}

// StoreFinalizer advances one fully completed logical store to its target level.
type StoreFinalizer struct {
	manifest      ReleaseManifest
	plan          MigrationPlan
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	now           func() time.Time
}

// NewStoreFinalizer creates a finalizer for one writable database namespace.
func NewStoreFinalizer(
	release ReleaseDefinition,
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
) (*StoreFinalizer, error) {
	registry, err := NewPlanRegistry(release.Manifest, release.Plan)
	if err != nil {
		return nil, fmt.Errorf("create migration store finalizer: %w", err)
	}
	if metadataStore == nil {
		return nil, errors.New("create migration store finalizer: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create migration store finalizer: transactioner is required")
	}
	return &StoreFinalizer{
		manifest: release.Manifest, plan: registry.Plan(), metadataStore: metadataStore,
		transactioner: transactioner, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Finalize verifies all operation progress and conditionally completes one reserved store.
func (f *StoreFinalizer) Finalize(
	ctx context.Context,
	storeID StoreID,
	runID string,
) (FinalizationResult, error) {
	if !isUUIDv7(runID) {
		return FinalizationResult{}, errors.New("finalize migration store: run ID must be a canonical UUIDv7")
	}
	requirement, expectedProgress, err := f.finalizationRequirements(storeID)
	if err != nil {
		return FinalizationResult{}, err
	}

	result := FinalizationResult{}
	err = f.transactioner.Transact(ctx, func(txCtx context.Context) error {
		state, err := f.metadataStore.GetState(txCtx, storeID)
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("finalize migration store %q: migration state is missing", storeID)
		}
		if state.Status == StatusClean && state.CompletedLevel == requirement.RequiredLevel {
			result = FinalizationResult{State: *state, AlreadyAtTarget: true}
			return nil
		}
		if err := validateFinalizationReservation(storeID, state, runID); err != nil {
			return err
		}
		if state.TargetLevel != requirement.RequiredLevel {
			return fmt.Errorf("finalize migration store %q: target level %q does not match required level %q",
				storeID, state.TargetLevel, requirement.RequiredLevel)
		}

		progress, err := f.metadataStore.ListProgress(txCtx, state.ID, runID)
		if err != nil {
			return err
		}
		if err := validateCompletedProgress(storeID, state, progress, expectedProgress); err != nil {
			return err
		}
		finalizedAt := f.now()
		finalized, err := f.metadataStore.FinalizeState(txCtx, *state, finalizedAt)
		if err != nil {
			return err
		}
		if !finalized {
			return fmt.Errorf("%w for store %q", ErrFinalizationConflict, storeID)
		}
		state.CompletedLevel = state.TargetLevel
		state.TargetLevel = ""
		state.Status = StatusClean
		state.ActiveRunID = ""
		state.FailureSummary = ""
		state.UpdatedAt = finalizedAt
		result = FinalizationResult{State: *state, Finalized: true}
		return nil
	})
	if err != nil {
		return FinalizationResult{}, fmt.Errorf("finalize migration store %q: %w", storeID, err)
	}
	return result, nil
}

func validateFinalizationReservation(storeID StoreID, state *State, runID string) error {
	if state.Status != StatusInProgress {
		return fmt.Errorf("finalize migration store %q: status is %q instead of %q",
			storeID, state.Status, StatusInProgress)
	}
	if state.ActiveRunID != runID {
		return fmt.Errorf("finalize migration store %q: store is reserved by a different run", storeID)
	}
	return nil
}

func (f *StoreFinalizer) finalizationRequirements(
	storeID StoreID,
) (StoreRequirement, map[string]int, error) {
	var requirement StoreRequirement
	found := false
	for _, candidate := range f.manifest.Stores {
		if candidate.ID == storeID {
			requirement = candidate
			found = true
			break
		}
	}
	if !found {
		return StoreRequirement{}, nil, fmt.Errorf("finalize migration store: store %q is not declared", storeID)
	}
	if !requirement.Changed {
		return StoreRequirement{}, nil, fmt.Errorf("finalize migration store: store %q does not require migration", storeID)
	}

	expected := make(map[string]int)
	for index, operation := range f.plan.Operations {
		definition := operation.Definition()
		if definition.StoreID != storeID {
			continue
		}
		if definition.InterruptionMode != InterruptionModeAtomic || definition.ScopeType != ScopeTypeStore {
			return StoreRequirement{}, nil, fmt.Errorf(
				"finalize migration store %q: operation %q is not supported by the basic executor",
				storeID, definition.ID)
		}
		expected[string(definition.ID)] = index
	}
	if len(expected) == 0 {
		return StoreRequirement{}, nil, fmt.Errorf("finalize migration store %q: no operations are declared", storeID)
	}
	return requirement, expected, nil
}

func validateCompletedProgress(
	storeID StoreID,
	state *State,
	progress []Progress,
	expected map[string]int,
) error {
	if len(progress) != len(expected) {
		return fmt.Errorf("finalize migration store %q: found %d completed progress records, expected %d",
			storeID, len(progress), len(expected))
	}
	seen := make(map[string]struct{}, len(progress))
	for _, item := range progress {
		expectedOrder, exists := expected[item.OperationID]
		if !exists {
			return fmt.Errorf("finalize migration store %q: progress references unknown operation %q",
				storeID, item.OperationID)
		}
		if _, duplicate := seen[item.OperationID]; duplicate {
			return fmt.Errorf("finalize migration store %q: progress repeats operation %q",
				storeID, item.OperationID)
		}
		if item.MigrationStateID != state.ID || item.RunID != state.ActiveRunID ||
			item.TargetLevel != state.TargetLevel {
			return fmt.Errorf("finalize migration store %q: operation %q progress does not belong to the active run",
				storeID, item.OperationID)
		}
		if item.OperationOrder != expectedOrder {
			return fmt.Errorf("finalize migration store %q: operation %q has order %d instead of %d",
				storeID, item.OperationID, item.OperationOrder, expectedOrder)
		}
		if item.Status != StatusCompleted {
			return fmt.Errorf("finalize migration store %q: operation %q status is %q instead of %q",
				storeID, item.OperationID, item.Status, StatusCompleted)
		}
		seen[item.OperationID] = struct{}{}
	}
	return nil
}
