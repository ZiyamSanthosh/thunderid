// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/system/utils"
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
	newID         func() (string, error)
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
		now:   func() time.Time { return time.Now().UTC() },
		newID: utils.GenerateUUIDv7,
	}, nil
}

// Reserve assigns the requested changed stores to one run within a namespace transaction.
func (r *MigrationStateController) Reserve(
	ctx context.Context,
	storeIDs []StoreID,
	runID string,
) ([]ReservationResult, error) {
	return r.ReservePhase(ctx, storeIDs, OperationPhaseExpand, runID)
}

// ReservePhase assigns the requested stores to one phase run within a namespace transaction.
func (r *MigrationStateController) ReservePhase(
	ctx context.Context,
	storeIDs []StoreID,
	phase OperationPhase,
	runID string,
) ([]ReservationResult, error) {
	if !isUUIDv7(runID) {
		return nil, errors.New("reserve migration stores: run ID must be a canonical UUIDv7")
	}
	executionStage, err := executionStageForPhase(phase)
	if err != nil {
		return nil, fmt.Errorf("reserve migration stores: %w", err)
	}
	requirements, err := r.requirementsFor(storeIDs)
	if err != nil {
		return nil, err
	}

	results := make([]ReservationResult, 0, len(requirements))
	err = r.transactioner.Transact(ctx, func(txCtx context.Context) error {
		reservedAt := r.now()
		for _, requirement := range requirements {
			phasePlan, err := phasePlanForStore(r.plan, requirement.ID, phase)
			if err != nil {
				return err
			}
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
			if state.CompletedLevel == requirement.RequiredLevel {
				results = append(results, ReservationResult{State: *state, AlreadyAtTarget: true})
				continue
			}
			if state.CompletedLevel != requirement.AcceptedSourceLevel {
				return fmt.Errorf("reserve migration store %q: completed level %q is neither accepted source %q nor target %q",
					requirement.ID, state.CompletedLevel, requirement.AcceptedSourceLevel, requirement.RequiredLevel)
			}

			if err := validatePhaseBoundary(
				requirement.ID, phase, phasePlan.previous, requirement.RequiredLevel, state); err != nil {
				return err
			}

			reserved := false
			if phasePlan.previous == "" {
				reserved, err = r.metadataStore.ReserveState(
					txCtx, *state, requirement.RequiredLevel, executionStage, runID, reservedAt)
			} else {
				next := *state
				next.Stage = executionStage
				next.Status = StatusInProgress
				next.ActiveRunID = runID
				next.FailureSummary = ""
				next.UpdatedAt = reservedAt
				reserved, err = r.metadataStore.TransitionState(txCtx, *state, next)
			}
			if err != nil {
				return err
			}
			if !reserved {
				return fmt.Errorf("%w for store %q", ErrReservationConflict, requirement.ID)
			}
			state.TargetLevel = requirement.RequiredLevel
			state.Stage = executionStage
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
			phase, err := phaseForExecutionStage(state.Stage)
			if err != nil {
				return fmt.Errorf("cancel migration reservation for store %q: %w", state.StoreID, err)
			}
			phasePlan, err := phasePlanForStore(r.plan, state.StoreID, phase)
			if err != nil {
				return err
			}
			cancelledState := state
			var changed bool
			if phasePlan.previous == "" {
				changed, err = r.metadataStore.CancelState(txCtx, state, updatedAt)
				cancelledState.TargetLevel = ""
				cancelledState.Stage = MigrationStageComplete
				cancelledState.Status = StatusClean
			} else {
				cancelledState.Stage, err = completedStageBeforePhase(phase)
				if err != nil {
					return err
				}
				cancelledState.Status = StatusCompleted
				cancelledState.ActiveRunID = ""
				cancelledState.FailureSummary = ""
				cancelledState.UpdatedAt = updatedAt
				changed, err = r.metadataStore.CancelPhaseState(txCtx, state, cancelledState)
			}
			if err != nil {
				return err
			}
			if !changed {
				return fmt.Errorf("%w while cancelling store %q", ErrRunStateConflict, state.StoreID)
			}
			cancelledState.ActiveRunID = ""
			cancelledState.FailureSummary = ""
			cancelledState.UpdatedAt = updatedAt
			cancelled = append(cancelled, cancelledState)
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
	return r.CompletePhase(ctx, storeID, OperationPhaseExpand, runID)
}

// CompletePhase verifies phase-local operation progress and advances the store to its next boundary.
func (r *MigrationStateController) CompletePhase(
	ctx context.Context,
	storeID StoreID,
	phase OperationPhase,
	runID string,
) (FinalizationResult, error) {
	return r.CompletePhaseSelection(ctx, storeID, phase, runID, MigrationSelection{})
}

// CompletePhaseSelection finalizes a complete-store phase or records deployment-selective migrate completion.
func (r *MigrationStateController) CompletePhaseSelection(
	ctx context.Context,
	storeID StoreID,
	phase OperationPhase,
	runID string,
	selection MigrationSelection,
) (FinalizationResult, error) {
	if !isUUIDv7(runID) {
		return FinalizationResult{}, errors.New("finalize migration store: run ID must be a canonical UUIDv7")
	}
	requirement, expectedProgress, err := r.finalizationRequirements(storeID, phase)
	if err != nil {
		return FinalizationResult{}, err
	}
	phasePlan, err := phasePlanForStore(r.plan, storeID, phase)
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
		expectedStage, err := executionStageForPhase(phase)
		if err != nil {
			return err
		}
		if state.Stage != expectedStage {
			return fmt.Errorf("finalize migration store %q: stage is %q instead of %q",
				storeID, state.Stage, expectedStage)
		}
		if state.TargetLevel != requirement.RequiredLevel {
			return fmt.Errorf("finalize migration store %q: target level %q does not match required level %q",
				storeID, state.TargetLevel, requirement.RequiredLevel)
		}

		progress, err := r.metadataStore.ListProgress(txCtx, state.ID, runID)
		if err != nil {
			return err
		}
		reusedProgress := make(map[string]struct{})
		progress, err = r.appendReusableSchemaProgress(
			txCtx, storeID, phase, state, progress, reusedProgress)
		if err != nil {
			return err
		}
		if err := validateCompletedProgress(
			storeID, state, progress, expectedProgress, reusedProgress); err != nil {
			return err
		}
		finalizedAt := r.now()
		finalized := false
		completedStage := MigrationStageComplete
		selective := len(selection.DeploymentIDs) > 0
		if selective {
			if phase != OperationPhaseMigrate {
				return errors.New("deployment-selective finalization is supported only for the migrate phase")
			}
			for _, deploymentID := range selection.DeploymentIDs {
				deploymentStateID, err := r.newID()
				if err != nil {
					return fmt.Errorf("generate deployment migration state ID: %w", err)
				}
				if err := r.metadataStore.UpsertDeploymentState(txCtx, DeploymentState{
					ID: deploymentStateID, MigrationStateID: state.ID, DeploymentID: deploymentID,
					CompletedLevel: requirement.RequiredLevel, UpdatedAt: finalizedAt,
				}); err != nil {
					return err
				}
			}
			next := *state
			next.Stage = MigrationStageExpanded
			next.Status = StatusCompleted
			next.ActiveRunID = ""
			next.FailureSummary = ""
			next.UpdatedAt = finalizedAt
			finalized, err = r.metadataStore.TransitionState(txCtx, *state, next)
			completedStage = MigrationStageExpanded
		} else if phasePlan.next == "" {
			finalized, err = r.metadataStore.FinalizeState(txCtx, *state, finalizedAt)
		} else {
			next := *state
			completedStage, err = completedStageBeforePhase(phasePlan.next)
			if err != nil {
				return err
			}
			next.Stage = completedStage
			next.Status = StatusCompleted
			next.ActiveRunID = ""
			next.FailureSummary = ""
			next.UpdatedAt = finalizedAt
			finalized, err = r.metadataStore.TransitionState(txCtx, *state, next)
		}
		if err != nil {
			return err
		}
		if !finalized {
			return fmt.Errorf("%w for store %q", ErrFinalizationConflict, storeID)
		}
		if selective {
			state.Stage = MigrationStageExpanded
			state.Status = StatusCompleted
		} else if phasePlan.next == "" {
			state.CompletedLevel = state.TargetLevel
			state.TargetLevel = ""
			state.Stage = MigrationStageComplete
			state.Status = StatusClean
		} else {
			state.Stage = completedStage
			state.Status = StatusCompleted
		}
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

func (r *MigrationStateController) appendReusableSchemaProgress(
	ctx context.Context,
	storeID StoreID,
	phase OperationPhase,
	state *State,
	progress []Progress,
	reused map[string]struct{},
) ([]Progress, error) {
	completed := make(map[string]struct{}, len(progress))
	for _, item := range progress {
		completed[item.OperationID] = struct{}{}
	}
	operations, err := r.plan.OperationsForPhase(phase)
	if err != nil {
		return nil, err
	}
	for _, operation := range operations {
		definition := operation.Definition()
		if definition.StoreID != storeID || definition.Kind != OperationKindSchemaChange {
			continue
		}
		operationID := string(definition.ID)
		if _, found := completed[operationID]; found {
			continue
		}
		existing, err := r.metadataStore.GetProgress(
			ctx, state.ID, state.TargetLevel, definition.ID, "")
		if err != nil {
			return nil, err
		}
		if existing == nil {
			continue
		}
		progress = append(progress, *existing)
		reused[operationID] = struct{}{}
	}
	return progress, nil
}

type storePhasePlan struct {
	previous OperationPhase
	next     OperationPhase
}

func phasePlanForStore(
	plan MigrationPlan,
	storeID StoreID,
	phase OperationPhase,
) (storePhasePlan, error) {
	if _, err := executionStageForPhase(phase); err != nil {
		return storePhasePlan{}, err
	}
	phases := []OperationPhase{OperationPhaseExpand, OperationPhaseMigrate, OperationPhaseContract}
	declared := make([]OperationPhase, 0, len(phases))
	for _, candidate := range phases {
		operations, _ := plan.OperationsForPhase(candidate)
		for _, operation := range operations {
			if operation.Definition().StoreID == storeID {
				declared = append(declared, candidate)
				break
			}
		}
	}
	for index, candidate := range declared {
		if candidate != phase {
			continue
		}
		result := storePhasePlan{}
		if index > 0 {
			result.previous = declared[index-1]
		}
		if index+1 < len(declared) {
			result.next = declared[index+1]
		}
		return result, nil
	}
	return storePhasePlan{}, fmt.Errorf(
		"migration store %q has no %q operations", storeID, phase)
}

func executionStageForPhase(phase OperationPhase) (MigrationStage, error) {
	switch phase {
	case OperationPhaseExpand:
		return MigrationStageExpanding, nil
	case OperationPhaseMigrate:
		return MigrationStageMigrating, nil
	case OperationPhaseContract:
		return MigrationStageContracting, nil
	default:
		return "", fmt.Errorf("unsupported operation phase %q", phase)
	}
}

func phaseForExecutionStage(stage MigrationStage) (OperationPhase, error) {
	switch stage {
	case MigrationStageExpanding:
		return OperationPhaseExpand, nil
	case MigrationStageMigrating:
		return OperationPhaseMigrate, nil
	case MigrationStageContracting:
		return OperationPhaseContract, nil
	default:
		return "", fmt.Errorf("unsupported executing migration stage %q", stage)
	}
}

func completedStageBeforePhase(phase OperationPhase) (MigrationStage, error) {
	switch phase {
	case OperationPhaseMigrate:
		return MigrationStageExpanded, nil
	case OperationPhaseContract:
		return MigrationStageContractReady, nil
	default:
		return "", fmt.Errorf("operation phase %q has no preceding completed stage", phase)
	}
}

func validatePhaseBoundary(
	storeID StoreID,
	phase OperationPhase,
	previousPhase OperationPhase,
	targetLevel string,
	state *State,
) error {
	if previousPhase == "" {
		if state.Stage != MigrationStageComplete || state.Status != StatusClean || state.TargetLevel != "" {
			return fmt.Errorf("reserve migration store %q: first phase requires a clean complete state", storeID)
		}
		return nil
	}
	expectedStage, err := completedStageBeforePhase(phase)
	if err != nil {
		return err
	}
	if state.Stage != expectedStage || state.Status != StatusCompleted || state.TargetLevel != targetLevel {
		return fmt.Errorf(
			"reserve migration store %q: phase after %q requires stage %q, status %q and target level %q",
			storeID, previousPhase, expectedStage, StatusCompleted, targetLevel)
	}
	return nil
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
	phase OperationPhase,
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
	for index, planned := range r.plan.plannedOperations() {
		definition := planned.operation.Definition()
		if planned.phase != phase {
			continue
		}
		if definition.StoreID != storeID {
			continue
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
	reused map[string]struct{},
) error {
	if len(progress) < len(expected) {
		return fmt.Errorf("finalize migration store %q: found %d completed progress records, expected at least %d",
			storeID, len(progress), len(expected))
	}
	seen := make(map[string]struct{}, len(progress))
	completedOperations := make(map[string]struct{}, len(expected))
	for _, item := range progress {
		expectedOrder, exists := expected[item.OperationID]
		if !exists {
			return fmt.Errorf("finalize migration store %q: progress references unknown operation %q",
				storeID, item.OperationID)
		}
		progressKey := item.OperationID + "\x00" + item.ExecutionTargetID
		if _, duplicate := seen[progressKey]; duplicate {
			return fmt.Errorf("finalize migration store %q: progress repeats operation %q target %q",
				storeID, item.OperationID, item.ExecutionTargetID)
		}
		_, reusable := reused[item.OperationID]
		belongsToRun := item.RunID == state.ActiveRunID ||
			(reusable && item.ExecutionTargetID == "")
		if item.MigrationStateID != state.ID || !belongsToRun || item.TargetLevel != state.TargetLevel {
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
		seen[progressKey] = struct{}{}
		completedOperations[item.OperationID] = struct{}{}
	}
	for operationID := range expected {
		if _, completed := completedOperations[operationID]; !completed {
			return fmt.Errorf("finalize migration store %q: operation %q has no completed progress",
				storeID, operationID)
		}
	}
	return nil
}
