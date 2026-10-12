// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoMigrationToRollback means that no failed or abandoned migration run is available for rollback.
var ErrNoMigrationToRollback = errors.New("no migration run is available to rollback")

// ErrRollbackUnavailable means that the failed operation does not define an available rollback operation.
var ErrRollbackUnavailable = errors.New(
	"failed migration operation does not define an available rollback operation; repair the cause and resume the migration")

// ErrRollbackAlreadyCompleted means that the failed operation was already rolled back.
var ErrRollbackAlreadyCompleted = errors.New(
	"failed migration operation has already been rolled back; repair the cause and resume the migration")

// RollbackScope identifies how much previously executed migration work an administrator selected.
type RollbackScope string

const (
	RollbackScopeFailedOperation RollbackScope = "failed_operation"
	RollbackScopePhase           RollbackScope = "phase"
	RollbackScopeAll             RollbackScope = "all"
)

// MigrationRollbackRequest declares an explicit administrator rollback selection.
type MigrationRollbackRequest struct {
	Phase     OperationPhase
	RevertAll bool
}

// MigrationRollbackResult describes one explicit rollback of a failed migration run.
type MigrationRollbackResult struct {
	RunID         string
	Phase         OperationPhase
	Scope         RollbackScope
	Operations    []OperationExecutionResult
	Finalizations []FinalizationResult
}

type rollbackCandidate struct {
	operation       RollbackOperation
	selection       MigrationSelection
	operationOrder  int
	progress        *Progress
	forwardProgress Progress
}

type migrationRollbackContext struct {
	runID             string
	phase             OperationPhase
	selection         MigrationSelection
	statesByNamespace map[int][]State
	stores            map[StoreID]struct{}
	candidates        []rollbackCandidate
	failure           *MigrationFailure
}

// Rollback reverts the explicitly selected portion of previously executed migration work.
func (c *MigrationCoordinator) Rollback(
	ctx context.Context,
	request MigrationRollbackRequest,
) (MigrationRollbackResult, error) {
	var result MigrationRollbackResult
	err := c.withExecutionLocks(ctx, 0, func(lockCtx context.Context) error {
		var rollbackErr error
		result, rollbackErr = c.rollback(lockCtx, request)
		return rollbackErr
	})
	return result, err
}

func (c *MigrationCoordinator) rollback(
	ctx context.Context,
	request MigrationRollbackRequest,
) (MigrationRollbackResult, error) {
	if request.RevertAll {
		if request.Phase != "" {
			return MigrationRollbackResult{}, errors.New("migration rollback cannot combine phase and revert-all")
		}
		return c.revertAll(ctx)
	}
	rollback, err := c.inspectRollbackRun(ctx)
	scope := RollbackScopeFailedOperation
	result := MigrationRollbackResult{RunID: rollback.runID, Phase: rollback.phase, Scope: scope}
	if err != nil {
		return result, err
	}
	if request.Phase != "" {
		if request.Phase != rollback.phase {
			return MigrationRollbackResult{}, fmt.Errorf(
				"migration rollback phase %q does not match failed run phase %q", request.Phase, rollback.phase)
		}
		scope = RollbackScopePhase
		result.Scope = scope
	}
	if scope == RollbackScopeFailedOperation {
		rollback.candidates, err = failedOperationCandidates(rollback)
		if err != nil {
			return result, err
		}
		failedStoreID := rollback.candidates[0].operation.Definition().StoreID
		failedNamespace := c.namespaceIndexes()[failedStoreID]
		states := rollback.statesByNamespace[failedNamespace]
		rollback.statesByNamespace = map[int][]State{}
		rollback.stores = map[StoreID]struct{}{failedStoreID: {}}
		for _, state := range states {
			if state.StoreID == failedStoreID {
				rollback.statesByNamespace[failedNamespace] = []State{state}
				break
			}
		}
	}

	begunStores := make(map[StoreID]struct{}, len(rollback.stores))
	for namespaceIndex := range c.namespaces {
		states := rollback.statesByNamespace[namespaceIndex]
		if len(states) == 0 {
			continue
		}
		_, beginErr := c.namespaces[namespaceIndex].controller.BeginRollbackScope(
			ctx, states, rollback.phase, rollback.runID, scope)
		if beginErr != nil {
			return result, errors.Join(beginErr, c.markFailed(ctx, begunStores, rollback.runID, beginErr))
		}
		for _, state := range states {
			begunStores[state.StoreID] = struct{}{}
		}
	}

	for _, candidate := range rollback.candidates {
		definition := candidate.operation.Definition()
		namespaceIndex := c.namespaceIndexes()[definition.StoreID]
		execution, executeErr := c.executeOperation(
			ctx, namespaceIndex, candidate.operation, candidate.selection, rollback.runID,
			candidate.operationOrder, candidate.progress)
		if executeErr != nil {
			failure := operationFailure{
				storeID: definition.StoreID, forwardOperationID: candidate.operation.ForwardOperationID(),
				currentOperationID: definition.ID,
				executionTargetID:  candidate.forwardProgress.ExecutionTargetID,
				executionDirection: ExecutionDirectionRollback,
			}
			return result, errors.Join(executeErr,
				c.failRun(ctx, begunStores, rollback.runID, failure, executeErr))
		}
		result.Operations = append(result.Operations, execution)
	}

	if scope == RollbackScopeFailedOperation {
		candidate := rollback.candidates[0]
		definition := candidate.operation.Definition()
		namespaceIndex := c.namespaceIndexes()[definition.StoreID]
		finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompleteOperationRollback(
			ctx, definition.StoreID, rollback.runID, candidate.forwardProgress,
			candidate.operation.Definition().ID, rollback.failure)
		if finalizeErr != nil {
			return result, errors.Join(finalizeErr,
				c.markFailed(ctx, begunStores, rollback.runID, finalizeErr))
		}
		result.Finalizations = append(result.Finalizations, finalization)
		return result, nil
	}

	for namespaceIndex := range c.namespaces {
		states := rollback.statesByNamespace[namespaceIndex]
		if len(states) == 0 {
			continue
		}
		for _, state := range states {
			finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompleteRollback(
				ctx, state.StoreID, rollback.phase, rollback.runID, rollback.selection)
			if finalizeErr != nil {
				return result, errors.Join(finalizeErr,
					c.markFailed(ctx, begunStores, rollback.runID, finalizeErr))
			}
			delete(begunStores, state.StoreID)
			result.Finalizations = append(result.Finalizations, finalization)
		}
	}
	return result, nil
}

func (c *MigrationCoordinator) inspectRollbackRun(ctx context.Context) (migrationRollbackContext, error) {
	rollback := migrationRollbackContext{
		statesByNamespace: make(map[int][]State),
		stores:            make(map[StoreID]struct{}),
	}
	states := make(map[StoreID]State, len(c.release.Manifest.Stores))
	namespaceByStore := c.namespaceIndexes()
	for _, requirement := range c.release.Manifest.Stores {
		namespaceIndex := namespaceByStore[requirement.ID]
		state, err := c.namespaces[namespaceIndex].metadataStore.GetState(ctx, requirement.ID)
		if err != nil {
			return rollback, fmt.Errorf("inspect migration rollback state for store %q: %w", requirement.ID, err)
		}
		if state == nil {
			return rollback, fmt.Errorf("inspect migration rollback state for store %q: migration state is missing",
				requirement.ID)
		}
		states[requirement.ID] = *state
		if state.Status != StatusFailed && state.Status != StatusInProgress {
			continue
		}
		if state.ActiveRunID == "" {
			return rollback, fmt.Errorf("inspect migration rollback state for store %q: active run is missing",
				requirement.ID)
		}
		phase, err := phaseForExecutionStage(state.Stage)
		if err != nil {
			return rollback, fmt.Errorf("inspect migration rollback state for store %q: %w", requirement.ID, err)
		}
		if rollback.runID == "" {
			rollback.runID = state.ActiveRunID
			rollback.phase = phase
		} else if rollback.runID != state.ActiveRunID || rollback.phase != phase {
			return rollback, errors.New("inspect migration rollback state: active stores belong to different runs or phases")
		}
	}
	if rollback.runID == "" {
		return rollback, ErrNoMigrationToRollback
	}
	for index := range c.namespaces {
		run, err := c.namespaces[index].metadataStore.GetRun(ctx, rollback.runID)
		if err != nil {
			return rollback, err
		}
		if run == nil {
			continue
		}
		if run.Phase != rollback.phase || run.Mode == MigrationRunModeRevertAll {
			return rollback, errors.New("inspect migration rollback: active run context does not match the store state")
		}
		if err := mergeRollbackRunContext(&rollback, *run); err != nil {
			return rollback, err
		}
	}
	if rollback.selection.DeploymentIDs == nil {
		return rollback, fmt.Errorf("inspect migration rollback: run %q is missing", rollback.runID)
	}

	forwardByID := make(map[OperationID]plannedOperation)
	rollbackByID := make(map[OperationID]RollbackOperation, len(c.plan.Rollback))
	rollbackByForward := make(map[OperationID]RollbackOperation, len(c.plan.Rollback))
	for _, planned := range c.plan.plannedOperations() {
		if planned.phase == rollback.phase {
			forwardByID[planned.operation.Definition().ID] = planned
		}
	}
	for _, operation := range c.plan.Rollback {
		rollbackByID[operation.Definition().ID] = operation
		rollbackByForward[operation.ForwardOperationID()] = operation
	}

	forwardProgress := make(map[string]Progress)
	rollbackProgress := make(map[string]Progress)
	for _, requirement := range c.release.Manifest.Stores {
		state := states[requirement.ID]
		progress, err := c.namespaces[namespaceByStore[requirement.ID]].metadataStore.ListProgress(
			ctx, state.ID, rollback.runID)
		if err != nil {
			return rollback, fmt.Errorf("inspect migration rollback progress for store %q: %w", requirement.ID, err)
		}
		hasPhaseProgress := false
		for _, item := range progress {
			operationID := OperationID(item.OperationID)
			key := operationTargetKey(operationID, item.ExecutionTargetID)
			if planned, exists := forwardByID[operationID]; exists {
				if planned.operation.Definition().StoreID != requirement.ID {
					return rollback, fmt.Errorf("inspect migration rollback: operation %q belongs to another store",
						operationID)
				}
				forwardProgress[key] = item
				hasPhaseProgress = true
				continue
			}
			if operation, exists := rollbackByID[operationID]; exists {
				if operation.Definition().StoreID != requirement.ID {
					return rollback, fmt.Errorf("inspect migration rollback: operation %q belongs to another store",
						operationID)
				}
				rollbackProgress[key] = item
				continue
			}
			return rollback, fmt.Errorf("inspect migration rollback: operation %q does not match the plan", operationID)
		}
		if !hasPhaseProgress && state.ActiveRunID != rollback.runID {
			continue
		}
		if state.ActiveRunID == "" {
			completed, err := c.storeCompletedPhase(requirement, state, rollback.phase)
			if err != nil {
				return rollback, err
			}
			if rollback.phase == OperationPhaseMigrate && len(rollback.selection.DeploymentIDs) > 0 &&
				state.Stage == MigrationStageExpanded && state.Status == StatusCompleted &&
				state.TargetLevel == requirement.RequiredLevel {
				completed = true
			}
			if !completed {
				return rollback, fmt.Errorf("inspect migration rollback: store %q is not at the completed phase boundary",
					requirement.ID)
			}
		} else if state.ActiveRunID != rollback.runID {
			return rollback, fmt.Errorf("inspect migration rollback: store %q is owned by another run", requirement.ID)
		}
		rollback.statesByNamespace[namespaceByStore[requirement.ID]] = append(
			rollback.statesByNamespace[namespaceByStore[requirement.ID]], state)
		rollback.stores[requirement.ID] = struct{}{}
	}

	planned := c.plan.plannedOperations()
	for index := len(planned) - 1; index >= 0; index-- {
		forward := planned[index]
		if forward.phase != rollback.phase {
			continue
		}
		forwardID := forward.operation.Definition().ID
		rollbackOperation, exists := rollbackByForward[forwardID]
		if !exists {
			continue
		}
		matchingProgress := make([]Progress, 0)
		for _, progress := range forwardProgress {
			if progress.OperationID != string(forwardID) {
				continue
			}
			matchingProgress = append(matchingProgress, progress)
		}
		sort.Slice(matchingProgress, func(i, j int) bool {
			return matchingProgress[i].ExecutionTargetID < matchingProgress[j].ExecutionTargetID
		})
		for _, progress := range matchingProgress {
			if progress.Status != StatusCompleted && progress.Status != StatusInProgress {
				return rollback, fmt.Errorf("inspect migration rollback: forward operation %q has status %q",
					forwardID, progress.Status)
			}
			rollbackKey := operationTargetKey(rollbackOperation.Definition().ID, progress.ExecutionTargetID)
			existing, hasExisting := rollbackProgress[rollbackKey]
			if hasExisting && existing.Status == StatusCompleted {
				continue
			}
			var resumableProgress *Progress
			if hasExisting {
				if existing.Status != StatusInProgress {
					return rollback, fmt.Errorf("inspect migration rollback: rollback operation %q has status %q",
						rollbackOperation.Definition().ID, existing.Status)
				}
				if rollbackOperation.Definition().InterruptionMode != InterruptionModeResumable {
					return rollback, fmt.Errorf("inspect migration rollback: atomic rollback operation %q is in progress",
						rollbackOperation.Definition().ID)
				}
				copy := existing
				resumableProgress = &copy
			}
			selection := MigrationSelection{}
			if progress.ExecutionTargetID != "" {
				selection.DeploymentIDs = []string{progress.ExecutionTargetID}
			}
			rollback.candidates = append(rollback.candidates, rollbackCandidate{
				operation: rollbackOperation, selection: selection,
				operationOrder: forward.order, progress: resumableProgress, forwardProgress: progress,
			})
		}
	}
	sortRollbackCandidates(rollback.candidates)
	return rollback, nil
}

func sortRollbackCandidates(candidates []rollbackCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].operationOrder != candidates[j].operationOrder {
			return candidates[i].operationOrder > candidates[j].operationOrder
		}
		return candidates[i].forwardProgress.ExecutionTargetID > candidates[j].forwardProgress.ExecutionTargetID
	})
}

func mergeRollbackRunContext(rollback *migrationRollbackContext, run MigrationRun) error {
	if rollback.selection.DeploymentIDs == nil {
		rollback.selection.DeploymentIDs = append([]string{}, run.Selection.DeploymentIDs...)
	} else if strings.Join(rollback.selection.DeploymentIDs, "\x00") !=
		strings.Join(run.Selection.DeploymentIDs, "\x00") {
		return errors.New("inspect migration rollback: database namespaces have different run selections")
	}
	if run.Failure == nil {
		return nil
	}
	if rollback.failure == nil {
		copy := *run.Failure
		rollback.failure = &copy
		return nil
	}
	left, right := rollback.failure, run.Failure
	if left.RunID != right.RunID || left.StoreID != right.StoreID ||
		left.ForwardOperationID != right.ForwardOperationID ||
		left.CurrentOperationID != right.CurrentOperationID ||
		left.ExecutionTargetID != right.ExecutionTargetID ||
		left.ExecutionDirection != right.ExecutionDirection || left.RolledBack != right.RolledBack ||
		left.FailureSummary != right.FailureSummary {
		return errors.New("inspect migration rollback: database namespaces have different failure identities")
	}
	return nil
}

func failedOperationCandidates(rollback migrationRollbackContext) ([]rollbackCandidate, error) {
	if rollback.failure == nil {
		inProgress := make([]rollbackCandidate, 0, 1)
		for _, candidate := range rollback.candidates {
			if candidate.forwardProgress.Status == StatusInProgress {
				inProgress = append(inProgress, candidate)
			}
		}
		if len(inProgress) == 0 {
			return nil, ErrRollbackUnavailable
		}
		if len(inProgress) != 1 {
			return nil, errors.New("inspect migration rollback: failed operation identity is ambiguous")
		}
		return inProgress, nil
	}
	if rollback.failure.RolledBack {
		return nil, ErrRollbackAlreadyCompleted
	}
	matching := make([]rollbackCandidate, 0, 1)
	for _, candidate := range rollback.candidates {
		if candidate.operation.ForwardOperationID() != rollback.failure.ForwardOperationID ||
			candidate.forwardProgress.ExecutionTargetID != rollback.failure.ExecutionTargetID {
			continue
		}
		matching = append(matching, candidate)
	}
	if len(matching) == 0 {
		return nil, ErrRollbackUnavailable
	}
	if len(matching) != 1 {
		return nil, errors.New("inspect migration rollback: failed operation target is duplicated")
	}
	return matching, nil
}

func (c *MigrationCoordinator) revertAll(ctx context.Context) (MigrationRollbackResult, error) {
	result := MigrationRollbackResult{Scope: RollbackScopeAll}

	forwardByID := make(map[OperationID]plannedOperation)
	rollbackByForward := make(map[OperationID]RollbackOperation, len(c.plan.Rollback))
	rollbackByID := make(map[OperationID]RollbackOperation, len(c.plan.Rollback))
	for _, planned := range c.plan.plannedOperations() {
		forwardByID[planned.operation.Definition().ID] = planned
	}
	for _, operation := range c.plan.Rollback {
		rollbackByForward[operation.ForwardOperationID()] = operation
		rollbackByID[operation.Definition().ID] = operation
	}

	statesByNamespace := make(map[int][]State)
	stores := make(map[StoreID]struct{})
	candidates := make([]rollbackCandidate, 0)
	rollbackProgress := make(map[string]Progress)
	interruptedRunIDs := make(map[string]struct{})
	namespaceByStore := c.namespaceIndexes()
	for _, requirement := range c.release.Manifest.Stores {
		if !requirement.Changed {
			continue
		}
		namespaceIndex := namespaceByStore[requirement.ID]
		state, err := c.namespaces[namespaceIndex].metadataStore.GetState(ctx, requirement.ID)
		if err != nil {
			return result, err
		}
		if state == nil {
			return result, fmt.Errorf("complete migration rollback: state for store %q is missing", requirement.ID)
		}
		if (state.Status == StatusFailed || state.Status == StatusInProgress) &&
			state.Stage == MigrationStageRollingBack && state.ActiveRunID != "" {
			interruptedRunIDs[state.ActiveRunID] = struct{}{}
		}
		progress, err := c.namespaces[namespaceIndex].metadataStore.ListProgressForTarget(
			ctx, state.ID, c.release.Manifest.ReleaseVersion)
		if err != nil {
			return result, err
		}
		storeHasProgress := false
		for _, item := range progress {
			rollbackOperation, isRollback := rollbackByID[OperationID(item.OperationID)]
			if isRollback && rollbackOperation.Definition().StoreID == requirement.ID {
				rollbackProgress[operationTargetKey(OperationID(item.OperationID), item.ExecutionTargetID)] = item
				continue
			}
			planned, isForward := forwardByID[OperationID(item.OperationID)]
			if !isForward || planned.operation.Definition().StoreID != requirement.ID {
				continue
			}
			storeHasProgress = true
			if item.Status != StatusCompleted && item.Status != StatusInProgress {
				return result, fmt.Errorf("complete migration rollback: forward operation %q has status %q",
					planned.operation.Definition().ID, item.Status)
			}
			rollbackOperation, hasRollback := rollbackByForward[planned.operation.Definition().ID]
			if !hasRollback {
				if planned.phase == OperationPhaseContract && item.Status == StatusCompleted {
					return result, fmt.Errorf(
						"complete migration rollback is blocked by contract operation %q without a rollback operation",
						planned.operation.Definition().ID)
				}
				continue
			}
			selection := MigrationSelection{}
			if item.ExecutionTargetID != "" {
				selection.DeploymentIDs = []string{item.ExecutionTargetID}
			}
			candidates = append(candidates, rollbackCandidate{
				operation: rollbackOperation, selection: selection,
				operationOrder: planned.order, forwardProgress: item,
			})
		}
		if storeHasProgress {
			statesByNamespace[namespaceIndex] = append(statesByNamespace[namespaceIndex], *state)
			stores[requirement.ID] = struct{}{}
		}
	}
	if len(stores) == 0 {
		return result, ErrNoMigrationToRollback
	}
	runID, err := c.interruptedRevertAllRunID(ctx, interruptedRunIDs)
	if err != nil {
		return result, err
	}
	if runID == "" {
		runID, err = c.newID()
		if err != nil {
			return result, fmt.Errorf("start complete migration rollback: generate run ID: %w", err)
		}
	}
	result.RunID = runID
	resumableCandidates := candidates[:0]
	for _, candidate := range candidates {
		key := operationTargetKey(candidate.operation.Definition().ID,
			candidate.forwardProgress.ExecutionTargetID)
		progress, exists := rollbackProgress[key]
		if !exists || progress.RunID != runID {
			resumableCandidates = append(resumableCandidates, candidate)
			continue
		}
		switch progress.Status {
		case StatusCompleted:
			continue
		case StatusInProgress:
			copy := progress
			candidate.progress = &copy
			resumableCandidates = append(resumableCandidates, candidate)
		default:
			return result, fmt.Errorf("complete migration rollback: rollback operation %q has status %q",
				candidate.operation.Definition().ID, progress.Status)
		}
	}
	candidates = resumableCandidates
	sortRollbackCandidates(candidates)

	for namespaceIndex := range c.namespaces {
		states := statesByNamespace[namespaceIndex]
		if len(states) == 0 {
			continue
		}
		if _, err := c.namespaces[namespaceIndex].controller.BeginRevertAll(ctx, states, runID); err != nil {
			return result, err
		}
	}
	for _, candidate := range candidates {
		definition := candidate.operation.Definition()
		namespaceIndex := namespaceByStore[definition.StoreID]
		execution, err := c.executeOperation(ctx, namespaceIndex, candidate.operation,
			candidate.selection, runID, candidate.operationOrder, candidate.progress)
		if err != nil {
			failure := operationFailure{
				storeID: definition.StoreID, forwardOperationID: candidate.operation.ForwardOperationID(),
				currentOperationID: definition.ID,
				executionTargetID:  candidate.forwardProgress.ExecutionTargetID,
				executionDirection: ExecutionDirectionRollback,
			}
			return result, errors.Join(err, c.failRun(ctx, stores, runID, failure, err))
		}
		result.Operations = append(result.Operations, execution)
	}
	for namespaceIndex := range c.namespaces {
		for _, state := range statesByNamespace[namespaceIndex] {
			finalization, err := c.namespaces[namespaceIndex].controller.CompleteRevertAll(
				ctx, state.StoreID, runID)
			if err != nil {
				return result, errors.Join(err, c.markFailed(ctx, stores, runID, err))
			}
			result.Finalizations = append(result.Finalizations, finalization)
		}
	}
	return result, nil
}

func (c *MigrationCoordinator) interruptedRevertAllRunID(
	ctx context.Context,
	runIDs map[string]struct{},
) (string, error) {
	interruptedRunID := ""
	for runID := range runIDs {
		for namespaceIndex := range c.namespaces {
			run, err := c.namespaces[namespaceIndex].metadataStore.GetRun(ctx, runID)
			if err != nil {
				return "", err
			}
			if run == nil || run.Mode != MigrationRunModeRevertAll {
				continue
			}
			if interruptedRunID != "" && interruptedRunID != runID {
				return "", errors.New("complete migration rollback: multiple interrupted rollback runs were found")
			}
			interruptedRunID = runID
		}
	}
	return interruptedRunID, nil
}
