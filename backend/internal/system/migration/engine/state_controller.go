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

// ErrReservationConflict means that migration state changed before ownership could be reserved.
var ErrReservationConflict = errors.New("migration store reservation conflict")

// ErrRunStateConflict means that a reserved store changed before a run-state transition.
var ErrRunStateConflict = errors.New("migration run state conflict")

// ErrFinalizationConflict means that migration state changed before it could be finalized.
var ErrFinalizationConflict = errors.New("migration store finalization conflict")

// ReservationResult describes one store after a reservation attempt.
type ReservationResult struct {
	State           State
	Reserved        bool
	AlreadyAtTarget bool
}

// ResumeResult describes one failed store returned to its existing migration run.
type ResumeResult struct {
	State   State
	Resumed bool
}

// FinalizationResult describes one store after a finalization attempt.
type FinalizationResult struct {
	State           State
	Finalized       bool
	AlreadyAtTarget bool
}

// MigrationStateController manages reservation, cancellation and failure state transitions.
type MigrationStateController struct {
	manifest      ReleaseManifest
	plan          MigrationPlan
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	now           func() time.Time
}

// NewMigrationStateController creates a state controller for one physical database namespace.
func NewMigrationStateController(
	release ReleaseDefinition,
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
) (*MigrationStateController, error) {
	registry, err := NewPlanRegistry(release.Manifest, release.Plan)
	if err != nil {
		return nil, fmt.Errorf("create migration state controller: %w", err)
	}
	if metadataStore == nil {
		return nil, errors.New("create migration state controller: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create migration state controller: transactioner is required")
	}
	return &MigrationStateController{
		manifest: release.Manifest, plan: registry.Plan(), metadataStore: metadataStore, transactioner: transactioner,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Reserve assigns the requested changed stores to one run within a namespace transaction.
func (r *MigrationStateController) Reserve(
	ctx context.Context,
	storeIDs []StoreID,
	runID string,
) ([]ReservationResult, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("reserve migration stores: run ID must be a canonical UUIDv7")
	}
	requirements, err := r.requirementsFor(storeIDs)
	if err != nil {
		return nil, err
	}

	results := make([]ReservationResult, 0, len(requirements))
	err = r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		reservedAt := r.now()
		for _, requirement := range requirements {
			state, err := r.metadataStore.GetState(txCtx, requirement.ID)
			if err != nil {
				return err
			}
			if state == nil {
				return fmt.Errorf("reserve migration store %q: migration state is missing", requirement.ID)
			}
			if state.MetadataVersion != r.manifest.MigrationMetadataVersion {
				return fmt.Errorf("reserve migration store %q: metadata version %d is not supported",
					requirement.ID, state.MetadataVersion)
			}
			if state.Status != StatusClean {
				return fmt.Errorf("reserve migration store %q: status is %q instead of %q",
					requirement.ID, state.Status, StatusClean)
			}
			if state.CompletedLevel == requirement.RequiredLevel {
				results = append(results, ReservationResult{State: *state, AlreadyAtTarget: true})
				continue
			}
			if state.CompletedLevel != requirement.AcceptedSourceLevel {
				return fmt.Errorf("reserve migration store %q: completed level %q is neither accepted source %q nor target %q",
					requirement.ID, state.CompletedLevel, requirement.AcceptedSourceLevel, requirement.RequiredLevel)
			}

			reserved, err := r.metadataStore.ReserveState(
				txCtx, *state, requirement.RequiredLevel, runID, reservedAt)
			if err != nil {
				return err
			}
			if !reserved {
				return fmt.Errorf("%w for store %q", ErrReservationConflict, requirement.ID)
			}
			state.TargetLevel = requirement.RequiredLevel
			state.Status = StatusInProgress
			state.ActiveRunID = runID
			state.FailureSummary = ""
			state.UpdatedAt = reservedAt
			results = append(results, ReservationResult{State: *state, Reserved: true})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve migration stores: %w", err)
	}
	return results, nil
}

func (r *MigrationStateController) requirementsFor(storeIDs []StoreID) ([]StoreRequirement, error) {
	if len(storeIDs) == 0 {
		return nil, errors.New("reserve migration stores: at least one logical store is required")
	}
	requirementsByID := make(map[StoreID]StoreRequirement, len(r.manifest.Stores))
	for _, requirement := range r.manifest.Stores {
		requirementsByID[requirement.ID] = requirement
	}

	seen := make(map[StoreID]struct{}, len(storeIDs))
	requirements := make([]StoreRequirement, 0, len(storeIDs))
	for _, storeID := range storeIDs {
		if _, duplicate := seen[storeID]; duplicate {
			return nil, fmt.Errorf("reserve migration stores: duplicate store %q", storeID)
		}
		requirement, exists := requirementsByID[storeID]
		if !exists {
			return nil, fmt.Errorf("reserve migration stores: store %q is not declared", storeID)
		}
		if !requirement.Changed {
			return nil, fmt.Errorf("reserve migration stores: store %q does not require migration", storeID)
		}
		seen[storeID] = struct{}{}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

// CancelReservations releases reservations only when no operation progress exists for the run.
func (r *MigrationStateController) CancelReservations(
	ctx context.Context,
	states []State,
	runID string,
) ([]State, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("cancel migration reservations: run ID must be a canonical UUIDv7")
	}
	if len(states) == 0 {
		return nil, errors.New("cancel migration reservations: at least one store state is required")
	}

	cancelled := make([]State, 0, len(states))
	err := r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		updatedAt := r.now()
		seen := make(map[StoreID]struct{}, len(states))
		for _, state := range states {
			if _, duplicate := seen[state.StoreID]; duplicate {
				return fmt.Errorf("cancel migration reservations: duplicate store %q", state.StoreID)
			}
			if state.Status != StatusInProgress || state.ActiveRunID != runID {
				return fmt.Errorf("cancel migration reservation for store %q: state is not reserved by this run",
					state.StoreID)
			}
			changed, err := r.metadataStore.CancelState(txCtx, state, updatedAt)
			if err != nil {
				return err
			}
			if !changed {
				return fmt.Errorf("%w while cancelling store %q", ErrRunStateConflict, state.StoreID)
			}
			state.TargetLevel = ""
			state.Status = StatusClean
			state.ActiveRunID = ""
			state.FailureSummary = ""
			state.UpdatedAt = updatedAt
			cancelled = append(cancelled, state)
			seen[state.StoreID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cancel migration reservations: %w", err)
	}
	return cancelled, nil
}

// MarkFailed marks every still-reserved store in this namespace as failed.
func (r *MigrationStateController) MarkFailed(
	ctx context.Context,
	storeIDs []StoreID,
	runID string,
	failureSummary string,
) ([]State, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("fail migration stores: run ID must be a canonical UUIDv7")
	}
	if len(storeIDs) == 0 {
		return nil, errors.New("fail migration stores: at least one logical store is required")
	}

	failed := make([]State, 0, len(storeIDs))
	err := r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		updatedAt := r.now()
		seen := make(map[StoreID]struct{}, len(storeIDs))
		for _, storeID := range storeIDs {
			if _, duplicate := seen[storeID]; duplicate {
				return fmt.Errorf("fail migration stores: duplicate store %q", storeID)
			}
			state, err := r.metadataStore.GetState(txCtx, storeID)
			if err != nil {
				return err
			}
			if state == nil {
				return fmt.Errorf("fail migration store %q: migration state is missing", storeID)
			}
			if state.Status == StatusClean {
				seen[storeID] = struct{}{}
				continue
			}
			if state.ActiveRunID != runID {
				return fmt.Errorf("fail migration store %q: store is reserved by a different run", storeID)
			}
			if state.Status == StatusFailed {
				failed = append(failed, *state)
				seen[storeID] = struct{}{}
				continue
			}
			if state.Status != StatusInProgress {
				return fmt.Errorf("fail migration store %q: unsupported status %q", storeID, state.Status)
			}
			changed, err := r.metadataStore.FailState(txCtx, *state, failureSummary, updatedAt)
			if err != nil {
				return err
			}
			if !changed {
				return fmt.Errorf("%w while failing store %q", ErrRunStateConflict, storeID)
			}
			state.Status = StatusFailed
			state.FailureSummary = failureSummary
			state.UpdatedAt = updatedAt
			failed = append(failed, *state)
			seen[storeID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fail migration stores: %w", err)
	}
	return failed, nil
}

// Resume returns failed stores to in-progress state without changing their owning run.
func (r *MigrationStateController) Resume(
	ctx context.Context,
	states []State,
	runID string,
) ([]ResumeResult, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("resume migration stores: run ID must be a canonical UUIDv7")
	}
	if len(states) == 0 {
		return nil, errors.New("resume migration stores: at least one store state is required")
	}

	results := make([]ResumeResult, 0, len(states))
	err := r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		updatedAt := r.now()
		seen := make(map[StoreID]struct{}, len(states))
		for _, expected := range states {
			if _, duplicate := seen[expected.StoreID]; duplicate {
				return fmt.Errorf("resume migration stores: duplicate store %q", expected.StoreID)
			}
			state, err := r.metadataStore.GetState(txCtx, expected.StoreID)
			if err != nil {
				return err
			}
			if state == nil {
				return fmt.Errorf("resume migration store %q: migration state is missing", expected.StoreID)
			}
			if *state != expected {
				return fmt.Errorf("%w while resuming store %q", ErrRunStateConflict, expected.StoreID)
			}
			if state.Status != StatusFailed || state.ActiveRunID != runID {
				return fmt.Errorf("resume migration store %q: state is not failed for run %q",
					expected.StoreID, runID)
			}
			resumed, err := r.metadataStore.ResumeState(txCtx, *state, updatedAt)
			if err != nil {
				return err
			}
			if !resumed {
				return fmt.Errorf("%w while resuming store %q", ErrRunStateConflict, expected.StoreID)
			}
			state.Status = StatusInProgress
			state.FailureSummary = ""
			state.UpdatedAt = updatedAt
			results = append(results, ResumeResult{State: *state, Resumed: true})
			seen[state.StoreID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resume migration stores: %w", err)
	}
	return results, nil
}

// Finalize verifies all operation progress and conditionally completes one reserved store.
func (r *MigrationStateController) Finalize(
	ctx context.Context,
	storeID StoreID,
	runID string,
) (FinalizationResult, error) {
	if !isUUIDv7(runID) {
		return FinalizationResult{}, errors.New("finalize migration store: run ID must be a canonical UUIDv7")
	}
	requirement, expectedProgress, err := r.finalizationRequirements(storeID)
	if err != nil {
		return FinalizationResult{}, err
	}

	result := FinalizationResult{}
	err = r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		state, err := r.metadataStore.GetState(txCtx, storeID)
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

		progress, err := r.metadataStore.ListProgress(txCtx, state.ID, runID)
		if err != nil {
			return err
		}
		if err := validateCompletedProgress(storeID, state, progress, expectedProgress); err != nil {
			return err
		}
		finalizedAt := r.now()
		finalized, err := r.metadataStore.FinalizeState(txCtx, *state, finalizedAt)
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

func (r *MigrationStateController) finalizationRequirements(
	storeID StoreID,
) (StoreRequirement, map[string]int, error) {
	var requirement StoreRequirement
	found := false
	for _, candidate := range r.manifest.Stores {
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
	for index, operation := range r.plan.Operations {
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
