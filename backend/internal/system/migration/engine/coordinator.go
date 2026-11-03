// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/internal/system/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// ErrMigrationPreflightBlocked means that a migration run did not start because preflight found a blocker.
var ErrMigrationPreflightBlocked = errors.New("migration preflight is blocked")

// ErrNoMigrationToResume means that no failed or abandoned run owned by this release was found.
var ErrNoMigrationToResume = errors.New("no migration run is available to resume")

// ErrMigrationRollbackInterrupted means that rollback must be resumed with the rollback command.
var ErrMigrationRollbackInterrupted = errors.New(
	"migration run failed during rollback; rerun the applicable migration rollback command")

// ErrSafeWorkflowUnsupported means that the release plan cannot reach a target-serving boundary
// without executing contract operations.
var ErrSafeWorkflowUnsupported = errors.New("migration plan is not supported by the safe workflow")

// ErrContractRequiresDedicatedExecution means that contract was requested through the generic phase API.
var ErrContractRequiresDedicatedExecution = errors.New("contract requires dedicated execution")

// MigrationNamespace contains the write resources for logical stores sharing one physical database.
type MigrationNamespace struct {
	StoreIDs      []StoreID
	DatabaseType  DatabaseType
	Client        dbprovider.DBClientInterface
	Transactioner providers.Transactioner
	Locker        MigrationExecutionLocker
}

// MigrationRunResult describes one attempted migration application.
type MigrationRunResult struct {
	RunID           string
	Phase           OperationPhase
	Preflight       PreflightReport
	Reservations    []ReservationResult
	Resumptions     []ResumeResult
	Operations      []OperationExecutionResult
	Finalizations   []FinalizationResult
	AlreadyAtTarget bool
	Resumed         bool
}

// MigrationWorkflowResult contains the phase results completed before the serving cutover.
type MigrationWorkflowResult struct {
	Phases []MigrationRunResult
}

type migrationResumeContext struct {
	runID               string
	phase               OperationPhase
	activeStores        map[StoreID]struct{}
	failedByNamespace   map[int][]State
	completedOperations map[OperationID]struct{}
	resumableProgress   map[OperationID]Progress
	completedTargets    map[string]struct{}
	resumableTargets    map[string]Progress
	selection           MigrationSelection
	storeAssessments    []StoreMigrationAssessment
}

type migrationNamespaceRuntime struct {
	storeIDs          []StoreID
	metadataStore     *MetadataStore
	transactioner     providers.Transactioner
	locker            MigrationExecutionLocker
	controller        *MigrationStateController
	atomicExecutor    *AtomicOperationExecutor
	onlineExecutor    *OnlineOperationExecutor
	resumableExecutor *ResumableOperationExecutor
}

type operationFailure struct {
	storeID            StoreID
	forwardOperationID OperationID
	currentOperationID OperationID
	executionTargetID  string
	executionDirection ExecutionDirection
}

// MigrationCoordinator owns the complete basic migration run across all database namespaces.
type MigrationCoordinator struct {
	release    ReleaseDefinition
	plan       MigrationPlan
	preflight  *PreflightChecker
	namespaces []migrationNamespaceRuntime
	newID      func() (string, error)
}

// NewMigrationCoordinator validates and assembles the resources for one release migration.
func NewMigrationCoordinator(
	release ReleaseDefinition,
	namespaces []MigrationNamespace,
) (*MigrationCoordinator, error) {
	registry, err := NewPlanRegistry(release.Manifest, release.Plan)
	if err != nil {
		return nil, fmt.Errorf("create migration coordinator: %w", err)
	}
	if len(namespaces) == 0 {
		return nil, errors.New("create migration coordinator: at least one database namespace is required")
	}

	plan := registry.Plan()

	requirements := make(map[StoreID]int, len(release.Manifest.Stores))
	for index, requirement := range release.Manifest.Stores {
		requirements[requirement.ID] = index
	}

	seenStores := make(map[StoreID]struct{}, len(requirements))
	stores := make(map[StoreID]ReadStore, len(requirements))
	runtimes := make([]migrationNamespaceRuntime, 0, len(namespaces))
	for index, namespace := range namespaces {
		if len(namespace.StoreIDs) == 0 {
			return nil, fmt.Errorf("create migration coordinator: namespace %d has no logical stores", index)
		}
		if namespace.Client == nil {
			return nil, fmt.Errorf("create migration coordinator: namespace %d database client is required", index)
		}
		if namespace.Transactioner == nil {
			return nil, fmt.Errorf("create migration coordinator: namespace %d transactioner is required", index)
		}
		if namespace.Locker == nil {
			return nil, fmt.Errorf("create migration coordinator: namespace %d execution locker is required", index)
		}
		metadataStore, err := NewMetadataStore(namespace.Client, namespace.DatabaseType)
		if err != nil {
			return nil, fmt.Errorf("create migration coordinator: namespace %d: %w", index, err)
		}
		storeIDs := append([]StoreID(nil), namespace.StoreIDs...)
		for _, storeID := range storeIDs {
			if _, declared := requirements[storeID]; !declared {
				return nil, fmt.Errorf("create migration coordinator: namespace %d contains undeclared store %q",
					index, storeID)
			}
			if _, duplicate := seenStores[storeID]; duplicate {
				return nil, fmt.Errorf("create migration coordinator: store %q belongs to multiple namespaces", storeID)
			}
			seenStores[storeID] = struct{}{}
			stores[storeID] = ReadStore{DatabaseType: namespace.DatabaseType, Client: namespace.Client}
		}
		sort.Slice(storeIDs, func(left, right int) bool {
			return requirements[storeIDs[left]] < requirements[storeIDs[right]]
		})

		runtimes = append(runtimes, migrationNamespaceRuntime{
			storeIDs: storeIDs, metadataStore: metadataStore, transactioner: namespace.Transactioner,
			locker: namespace.Locker,
		})
	}
	if len(seenStores) != len(requirements) {
		for _, requirement := range release.Manifest.Stores {
			if _, exists := seenStores[requirement.ID]; !exists {
				return nil, fmt.Errorf("create migration coordinator: store %q has no database namespace",
					requirement.ID)
			}
		}
	}
	sort.SliceStable(runtimes, func(left, right int) bool {
		return requirements[runtimes[left].storeIDs[0]] < requirements[runtimes[right].storeIDs[0]]
	})
	preflight, err := NewPreflightChecker(release, stores)
	if err != nil {
		return nil, fmt.Errorf("create migration coordinator: %w", err)
	}
	for index := range runtimes {
		controller, err := NewMigrationStateController(
			release, runtimes[index].metadataStore, runtimes[index].transactioner)
		if err != nil {
			return nil, err
		}
		executor, err := NewAtomicOperationExecutor(
			runtimes[index].metadataStore, runtimes[index].transactioner, stores)
		if err != nil {
			return nil, err
		}
		resumableExecutor, err := NewResumableOperationExecutor(
			runtimes[index].metadataStore, runtimes[index].transactioner, stores)
		if err != nil {
			return nil, err
		}
		onlineExecutor, err := NewOnlineOperationExecutor(
			runtimes[index].metadataStore, runtimes[index].transactioner, stores)
		if err != nil {
			return nil, err
		}
		runtimes[index].controller = controller
		runtimes[index].atomicExecutor = executor
		runtimes[index].onlineExecutor = onlineExecutor
		runtimes[index].resumableExecutor = resumableExecutor
	}

	return &MigrationCoordinator{
		release: release, plan: plan, preflight: preflight, namespaces: runtimes,
		newID: utils.GenerateUUIDv7,
	}, nil
}

// ApplyPhase executes one explicitly selected migration phase.
func (c *MigrationCoordinator) ApplyPhase(
	ctx context.Context,
	phase OperationPhase,
	selection MigrationSelection,
) (MigrationRunResult, error) {
	if phase == OperationPhaseContract {
		return MigrationRunResult{Phase: phase}, ErrContractRequiresDedicatedExecution
	}
	return c.applyPhaseWithLock(ctx, phase, selection)
}

// ApplyContract executes contract through its explicit migration path.
func (c *MigrationCoordinator) ApplyContract(
	ctx context.Context,
	selection MigrationSelection,
) (MigrationRunResult, error) {
	return c.applyPhaseWithLock(ctx, OperationPhaseContract, selection)
}

func (c *MigrationCoordinator) applyPhaseWithLock(
	ctx context.Context,
	phase OperationPhase,
	selection MigrationSelection,
) (MigrationRunResult, error) {
	result := MigrationRunResult{Phase: phase}
	err := c.withExecutionLocks(ctx, 0, func(lockCtx context.Context) error {
		var applyErr error
		result, applyErr = c.applyPhase(lockCtx, phase, selection)
		return applyErr
	})
	return result, err
}

// ApplySafe executes only expand so operators can start and verify the target, then drain the source
// before explicitly starting migrate.
func (c *MigrationCoordinator) ApplySafe(
	ctx context.Context,
	selection MigrationSelection,
) (MigrationWorkflowResult, error) {
	result := MigrationWorkflowResult{Phases: make([]MigrationRunResult, 0, 1)}
	if err := c.validateSafeWorkflow(); err != nil {
		return result, err
	}
	err := c.withExecutionLocks(ctx, 0, func(lockCtx context.Context) error {
		phaseResult, err := c.applyPhase(lockCtx, OperationPhaseExpand, selection)
		result.Phases = append(result.Phases, phaseResult)
		if err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func (c *MigrationCoordinator) validateSafeWorkflow() error {
	expandStores := make(map[StoreID]struct{})
	for _, operation := range c.plan.Expand {
		expandStores[operation.Definition().StoreID] = struct{}{}
	}
	for _, phase := range [][]Operation{c.plan.Migrate, c.plan.Contract} {
		for _, operation := range phase {
			storeID := operation.Definition().StoreID
			if _, exists := expandStores[storeID]; exists {
				continue
			}
			return fmt.Errorf(
				"%w: store %q has post-expand operations but no expand operation to establish the serving boundary",
				ErrSafeWorkflowUnsupported, storeID)
		}
	}
	return nil
}

func (c *MigrationCoordinator) applyPhase(
	ctx context.Context,
	phase OperationPhase,
	selection MigrationSelection,
) (MigrationRunResult, error) {
	result := MigrationRunResult{Phase: phase}
	if err := validateMigrationSelection(selection); err != nil {
		return result, fmt.Errorf("apply migration: %w", err)
	}
	if _, err := executionStageForPhase(phase); err != nil {
		return result, fmt.Errorf("apply migration: %w", err)
	}
	if len(selection.DeploymentIDs) > 0 && phase != OperationPhaseMigrate {
		return result, errors.New("deployment selection is supported only for the migrate phase")
	}
	metadataStores := c.metadataStoresByStore()
	preflight, err := c.preflight.CheckPhase(ctx, metadataStores, phase)
	result.Preflight = preflight
	if err != nil {
		return result, err
	}
	if !result.Preflight.Ready() {
		return result, ErrMigrationPreflightBlocked
	}

	migrationStores := migrationStoreSet(result.Preflight)
	if len(migrationStores) == 0 {
		result.AlreadyAtTarget = true
		return result, nil
	}

	runID, err := c.newID()
	if err != nil {
		return result, fmt.Errorf("start migration run: generate run ID: %w", err)
	}
	if !isUUIDv7(runID) {
		return result, errors.New("start migration run: generated run ID is not a canonical UUIDv7")
	}
	result.RunID = runID

	reservedStates := make(map[int][]State)
	reservedStores := make(map[StoreID]struct{}, len(migrationStores))
	for index := range c.namespaces {
		storeIDs := includedStoreIDs(c.namespaces[index].storeIDs, migrationStores)
		if len(storeIDs) == 0 {
			continue
		}
		reservations, reserveErr := c.namespaces[index].controller.ReservePhaseSelection(
			ctx, storeIDs, phase, runID, selection)
		if reserveErr != nil {
			return result, errors.Join(reserveErr, c.cancelReservations(ctx, reservedStates, runID))
		}
		result.Reservations = append(result.Reservations, reservations...)
		for _, reservation := range reservations {
			if reservation.Reserved {
				reservedStates[index] = append(reservedStates[index], reservation.State)
				reservedStores[reservation.State.StoreID] = struct{}{}
			}
		}
	}
	if len(reservedStores) == 0 {
		result.AlreadyAtTarget = true
		return result, nil
	}
	operationReport := c.preflight.readinessEvaluator.checkPhaseStores(
		ctx, phase, reservedStores, nil)
	result.Preflight.Operations = operationReport.Operations
	if !operationReport.Ready() {
		return result, errors.Join(ErrMigrationPreflightBlocked,
			c.cancelReservations(ctx, reservedStates, runID))
	}

	namespaceByStore := c.namespaceIndexes()
	plannedOperations := c.plan.plannedOperations()
	lastOperationByStore := make(map[StoreID]int, len(reservedStores))
	for _, planned := range plannedOperations {
		if planned.phase == phase {
			lastOperationByStore[planned.operation.Definition().StoreID] = planned.order
		}
	}
	for _, planned := range plannedOperations {
		if planned.phase != phase {
			continue
		}
		operation := planned.operation
		operationOrder := planned.order
		definition := operation.Definition()
		if _, reserved := reservedStores[definition.StoreID]; !reserved {
			continue
		}
		namespaceIndex := namespaceByStore[definition.StoreID]
		for _, operationSelection := range operationSelections(definition, selection) {
			execution, executeErr := c.executeOperation(
				ctx, namespaceIndex, operation, operationSelection, runID, operationOrder, nil)
			if executeErr != nil {
				failure := operationFailure{
					storeID: definition.StoreID, forwardOperationID: definition.ID,
					currentOperationID: definition.ID, executionTargetID: selectionTargetID(operationSelection),
					executionDirection: ExecutionDirectionForward,
				}
				return result, errors.Join(executeErr,
					c.failRun(ctx, reservedStores, runID, failure, executeErr))
			}
			result.Operations = append(result.Operations, execution)
		}
		if lastOperationByStore[definition.StoreID] != operationOrder {
			continue
		}
		finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompletePhaseSelection(
			ctx, definition.StoreID, phase, runID, selection)
		if finalizeErr != nil {
			return result, errors.Join(finalizeErr, c.markFailed(ctx, reservedStores, runID, finalizeErr))
		}
		result.Finalizations = append(result.Finalizations, finalization)
		delete(reservedStores, definition.StoreID)
	}
	for _, requirement := range c.release.Manifest.Stores {
		if _, reserved := reservedStores[requirement.ID]; !reserved {
			continue
		}
		namespaceIndex := namespaceByStore[requirement.ID]
		finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompletePhaseSelection(
			ctx, requirement.ID, phase, runID, selection)
		if finalizeErr != nil {
			return result, errors.Join(finalizeErr, c.markFailed(ctx, reservedStores, runID, finalizeErr))
		}
		result.Finalizations = append(result.Finalizations, finalization)
		delete(reservedStores, requirement.ID)
	}
	return result, nil
}

func operationSelections(
	definition OperationDefinition,
	selection MigrationSelection,
) []MigrationSelection {
	if definition.Kind != OperationKindDataChange || len(selection.DeploymentIDs) == 0 {
		return []MigrationSelection{selection}
	}
	selections := make([]MigrationSelection, 0, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		selections = append(selections, MigrationSelection{DeploymentIDs: []string{deploymentID}})
	}
	return selections
}

// Resume continues one failed or abandoned atomic migration run.
func (c *MigrationCoordinator) Resume(ctx context.Context) (MigrationRunResult, error) {
	var result MigrationRunResult
	err := c.withExecutionLocks(ctx, 0, func(lockCtx context.Context) error {
		var resumeErr error
		result, resumeErr = c.resume(lockCtx)
		return resumeErr
	})
	return result, err
}

func (c *MigrationCoordinator) resume(ctx context.Context) (MigrationRunResult, error) {
	resume, err := c.inspectResumableRun(ctx)
	result := MigrationRunResult{RunID: resume.runID, Phase: resume.phase, Preflight: PreflightReport{
		TargetVersion: c.release.Manifest.ReleaseVersion,
		Phase:         resume.phase,
		Stores:        resume.storeAssessments,
	}, Resumed: true}
	if err != nil {
		return result, err
	}

	operationReport := c.preflight.readinessEvaluator.checkPendingPhaseStores(
		ctx, resume.phase, resume.activeStores, nil, resume.completedOperations)
	result.Preflight.Operations = operationReport.Operations
	if !operationReport.Ready() {
		return result, ErrMigrationPreflightBlocked
	}

	resumedStores := make(map[StoreID]struct{}, len(resume.activeStores))
	for index := range c.namespaces {
		states := resume.failedByNamespace[index]
		if len(states) == 0 {
			continue
		}
		resumptions, resumeErr := c.namespaces[index].controller.Resume(ctx, states, resume.runID)
		if resumeErr != nil {
			return result, errors.Join(resumeErr, c.markFailed(ctx, resumedStores, resume.runID, resumeErr))
		}
		result.Resumptions = append(result.Resumptions, resumptions...)
		for _, resumed := range resumptions {
			resumedStores[resumed.State.StoreID] = struct{}{}
		}
	}
	namespaceByStore := c.namespaceIndexes()
	plannedOperations := c.plan.plannedOperations()
	lastOperationByStore := make(map[StoreID]int, len(resume.activeStores))
	for _, planned := range plannedOperations {
		if planned.phase != resume.phase {
			continue
		}
		definition := planned.operation.Definition()
		if _, active := resume.activeStores[definition.StoreID]; active {
			lastOperationByStore[definition.StoreID] = planned.order
		}
	}
	for _, planned := range plannedOperations {
		if planned.phase != resume.phase {
			continue
		}
		operation := planned.operation
		operationOrder := planned.order
		definition := operation.Definition()
		if _, active := resume.activeStores[definition.StoreID]; !active {
			continue
		}
		namespaceIndex := namespaceByStore[definition.StoreID]
		for _, operationSelection := range operationSelections(definition, resume.selection) {
			key := operationTargetKey(definition.ID, selectionTargetID(operationSelection))
			if _, completed := resume.completedTargets[key]; completed {
				continue
			}
			progress, hasProgress := resume.resumableTargets[key]
			var existingProgress *Progress
			if hasProgress {
				existingProgress = &progress
			}
			execution, executeErr := c.executeOperation(
				ctx, namespaceIndex, operation, operationSelection, resume.runID, operationOrder,
				existingProgress)
			if executeErr != nil {
				failure := operationFailure{
					storeID: definition.StoreID, forwardOperationID: definition.ID,
					currentOperationID: definition.ID, executionTargetID: selectionTargetID(operationSelection),
					executionDirection: ExecutionDirectionForward,
				}
				return result, errors.Join(executeErr,
					c.failRun(ctx, resume.activeStores, resume.runID, failure, executeErr))
			}
			result.Operations = append(result.Operations, execution)
		}
		if lastOperationByStore[definition.StoreID] != operationOrder {
			continue
		}
		finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompletePhaseSelection(
			ctx, definition.StoreID, resume.phase, resume.runID, resume.selection)
		if finalizeErr != nil {
			return result, errors.Join(finalizeErr,
				c.markFailed(ctx, resume.activeStores, resume.runID, finalizeErr))
		}
		result.Finalizations = append(result.Finalizations, finalization)
		delete(resume.activeStores, definition.StoreID)
	}
	for _, requirement := range c.release.Manifest.Stores {
		if _, active := resume.activeStores[requirement.ID]; !active {
			continue
		}
		namespaceIndex := namespaceByStore[requirement.ID]
		finalization, finalizeErr := c.namespaces[namespaceIndex].controller.CompletePhaseSelection(
			ctx, requirement.ID, resume.phase, resume.runID, resume.selection)
		if finalizeErr != nil {
			return result, errors.Join(finalizeErr,
				c.markFailed(ctx, resume.activeStores, resume.runID, finalizeErr))
		}
		result.Finalizations = append(result.Finalizations, finalization)
		delete(resume.activeStores, requirement.ID)
	}
	return result, nil
}

func selectionTargetID(selection MigrationSelection) string {
	if len(selection.DeploymentIDs) == 0 {
		return ""
	}
	return selection.DeploymentIDs[0]
}

func operationTargetKey(operationID OperationID, targetID string) string {
	return string(operationID) + "\x00" + targetID
}

func (c *MigrationCoordinator) executeOperation(
	ctx context.Context,
	namespaceIndex int,
	operation Operation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
	progress *Progress,
) (OperationExecutionResult, error) {
	definition := operation.Definition()
	switch definition.InterruptionMode {
	case InterruptionModeAtomic:
		if progress != nil {
			return OperationExecutionResult{}, fmt.Errorf(
				"execute operation %q: atomic operation cannot have resumable progress", definition.ID)
		}
		atomicOperation, ok := operation.(AtomicOperation)
		if !ok {
			return OperationExecutionResult{}, fmt.Errorf(
				"execute operation %q: operation does not implement AtomicOperation", definition.ID)
		}
		return c.namespaces[namespaceIndex].atomicExecutor.Execute(
			ctx, atomicOperation, selection, runID, operationOrder)
	case InterruptionModeOnline:
		onlineOperation, ok := operation.(OnlineOperation)
		if !ok {
			return OperationExecutionResult{}, fmt.Errorf(
				"execute operation %q: operation does not implement OnlineOperation", definition.ID)
		}
		return c.namespaces[namespaceIndex].onlineExecutor.Execute(
			ctx, onlineOperation, selection, runID, operationOrder, progress)
	case InterruptionModeResumable:
		resumableOperation, ok := operation.(ResumableOperation)
		if !ok {
			return OperationExecutionResult{}, fmt.Errorf(
				"execute operation %q: operation does not implement ResumableOperation", definition.ID)
		}
		if progress != nil {
			return c.namespaces[namespaceIndex].resumableExecutor.Resume(
				ctx, resumableOperation, selection, *progress)
		}
		return c.namespaces[namespaceIndex].resumableExecutor.Execute(
			ctx, resumableOperation, selection, runID, operationOrder)
	default:
		return OperationExecutionResult{}, fmt.Errorf(
			"execute operation %q: unsupported interruption mode %q",
			definition.ID, definition.InterruptionMode)
	}
}

func (c *MigrationCoordinator) withExecutionLocks(
	ctx context.Context,
	namespaceIndex int,
	fn func(context.Context) error,
) error {
	if namespaceIndex == len(c.namespaces) {
		return fn(ctx)
	}
	return c.namespaces[namespaceIndex].locker.WithLock(ctx, func(lockCtx context.Context) error {
		return c.withExecutionLocks(lockCtx, namespaceIndex+1, fn)
	})
}

func (c *MigrationCoordinator) inspectResumableRun(ctx context.Context) (migrationResumeContext, error) {
	resume := migrationResumeContext{
		activeStores:        make(map[StoreID]struct{}),
		failedByNamespace:   make(map[int][]State),
		completedOperations: make(map[OperationID]struct{}),
		resumableProgress:   make(map[OperationID]Progress),
		completedTargets:    make(map[string]struct{}),
		resumableTargets:    make(map[string]Progress),
		storeAssessments:    make([]StoreMigrationAssessment, 0, len(c.release.Manifest.Stores)),
	}
	namespaceByStore := c.namespaceIndexes()
	statesByStore := make(map[StoreID]State, len(c.release.Manifest.Stores))
	for _, requirement := range c.release.Manifest.Stores {
		namespaceIndex := namespaceByStore[requirement.ID]
		state, err := c.namespaces[namespaceIndex].metadataStore.GetState(ctx, requirement.ID)
		if err != nil {
			return resume, fmt.Errorf("inspect migration resume state for store %q: %w", requirement.ID, err)
		}
		if state == nil {
			return resume, fmt.Errorf("inspect migration resume state for store %q: migration state is missing",
				requirement.ID)
		}
		if state.MetadataVersion != c.release.Manifest.MigrationMetadataVersion {
			return resume, fmt.Errorf("inspect migration resume state for store %q: metadata version %d is not supported",
				requirement.ID, state.MetadataVersion)
		}
		statesByStore[requirement.ID] = *state
		if state.Status != StatusFailed && state.Status != StatusInProgress {
			continue
		}
		if !requirement.Changed {
			return resume, fmt.Errorf("inspect migration resume state for store %q: unchanged store has status %q",
				requirement.ID, state.Status)
		}
		if state.CompletedLevel != requirement.AcceptedSourceLevel || state.TargetLevel != requirement.RequiredLevel {
			return resume, fmt.Errorf(
				"inspect migration resume state for store %q: levels %q to %q do not match route %q to %q",
				requirement.ID, state.CompletedLevel, state.TargetLevel,
				requirement.AcceptedSourceLevel, requirement.RequiredLevel)
		}
		if state.Stage == MigrationStageRollingBack {
			resume.runID = state.ActiveRunID
			return resume, ErrMigrationRollbackInterrupted
		}
		phase, err := phaseForExecutionStage(state.Stage)
		if err != nil {
			return resume, fmt.Errorf("inspect migration resume state for store %q: %w", requirement.ID, err)
		}
		if resume.phase == "" {
			resume.phase = phase
		} else if resume.phase != phase {
			return resume, fmt.Errorf("inspect migration resume state: active stores belong to different phases")
		}
		if resume.runID == "" {
			resume.runID = state.ActiveRunID
		} else if state.ActiveRunID != resume.runID {
			return resume, fmt.Errorf("inspect migration resume state: active stores belong to different runs")
		}
	}
	if resume.runID == "" {
		return resume, ErrNoMigrationToResume
	}
	if resume.phase == OperationPhaseMigrate {
		selection, err := c.loadResumeSelection(ctx, resume.runID, statesByStore)
		if err != nil {
			return resume, err
		}
		resume.selection = selection
	}

	activeStates := make(map[StoreID]State)
	for _, requirement := range c.release.Manifest.Stores {
		state := statesByStore[requirement.ID]
		namespaceIndex := namespaceByStore[requirement.ID]
		assessment := StoreMigrationAssessment{
			StoreID: requirement.ID, AcceptedSourceLevel: requirement.AcceptedSourceLevel,
			RequiredLevel: requirement.RequiredLevel, CompletedLevel: state.CompletedLevel,
			Status: state.Status, Stage: state.Stage,
		}
		if state.Status == StatusClean && state.CompletedLevel == requirement.RequiredLevel {
			assessment.State = StoreAssessmentAtTarget
			resume.storeAssessments = append(resume.storeAssessments, assessment)
			continue
		}
		if !requirement.Changed {
			return resume, fmt.Errorf("inspect migration resume state for store %q: unchanged store is not at level %q",
				requirement.ID, requirement.RequiredLevel)
		}
		if state.Status != StatusFailed && state.Status != StatusInProgress {
			completed, err := c.storeCompletedPhase(requirement, state, resume.phase)
			if err != nil {
				return resume, err
			}
			if !completed {
				return resume, fmt.Errorf(
					"inspect migration resume state for store %q: phase %q is not complete and is not owned by run %q",
					requirement.ID, resume.phase, resume.runID)
			}
			assessment.State = StoreAssessmentAtTarget
			resume.storeAssessments = append(resume.storeAssessments, assessment)
			continue
		}
		assessment.State = StoreAssessmentMigrationRequired
		resume.storeAssessments = append(resume.storeAssessments, assessment)
		resume.activeStores[requirement.ID] = struct{}{}
		if state.Status == StatusFailed {
			resume.failedByNamespace[namespaceIndex] = append(resume.failedByNamespace[namespaceIndex], state)
		}
		activeStates[requirement.ID] = state
	}
	if err := c.rejectForwardResumeDuringRollback(ctx, resume); err != nil {
		return resume, err
	}
	if err := c.inspectCompletedProgress(ctx, resume, activeStates); err != nil {
		return resume, err
	}
	return resume, nil
}

func (c *MigrationCoordinator) rejectForwardResumeDuringRollback(
	ctx context.Context,
	resume migrationResumeContext,
) error {
	for namespaceIndex, states := range resume.failedByNamespace {
		if len(states) == 0 {
			continue
		}
		run, err := c.namespaces[namespaceIndex].metadataStore.GetRun(ctx, resume.runID)
		if err != nil {
			return fmt.Errorf("inspect migration resume run: %w", err)
		}
		if run != nil && (run.Mode == MigrationRunModeRollback || run.Mode == MigrationRunModeRevertAll ||
			run.Failure != nil && run.Failure.ExecutionDirection == ExecutionDirectionRollback) {
			return ErrMigrationRollbackInterrupted
		}
	}
	return nil
}

func (c *MigrationCoordinator) loadResumeSelection(
	ctx context.Context,
	runID string,
	statesByStore map[StoreID]State,
) (MigrationSelection, error) {
	visited := make(map[int]struct{})
	var expected []string
	indexes := c.namespaceIndexes()
	for storeID, state := range statesByStore {
		if state.ActiveRunID != runID {
			continue
		}
		namespaceIndex := indexes[storeID]
		if _, done := visited[namespaceIndex]; done {
			continue
		}
		run, err := c.namespaces[namespaceIndex].metadataStore.GetRun(ctx, runID)
		if err != nil {
			return MigrationSelection{}, fmt.Errorf("inspect migration resume run: %w", err)
		}
		if run == nil {
			return MigrationSelection{}, fmt.Errorf("inspect migration resume: run %q is missing", runID)
		}
		targets := run.Selection.DeploymentIDs
		if expected == nil {
			expected = targets
		} else if strings.Join(expected, "\x00") != strings.Join(targets, "\x00") {
			return MigrationSelection{}, errors.New(
				"inspect migration resume targets: database namespaces have different deployment selections")
		}
		visited[namespaceIndex] = struct{}{}
	}
	return MigrationSelection{DeploymentIDs: append([]string(nil), expected...)}, nil
}

func (c *MigrationCoordinator) storeCompletedPhase(
	requirement StoreRequirement,
	state State,
	phase OperationPhase,
) (bool, error) {
	phasePlan, err := phasePlanForStore(c.plan, requirement.ID, phase)
	if err != nil {
		if !errors.Is(err, errStorePhaseNotDeclared) {
			return false, err
		}
		return state.Status == StatusClean || state.Status == StatusCompleted, nil
	}
	if phasePlan.next == "" {
		return state.Status == StatusClean && state.Stage == MigrationStageComplete &&
			state.CompletedLevel == requirement.RequiredLevel, nil
	}
	return state.CompletedLevel == requirement.AcceptedSourceLevel &&
		state.ActiveRunID == "" &&
		requestedPhaseCompleted(phase, requirement.RequiredLevel, &state), nil
}

func (c *MigrationCoordinator) inspectCompletedProgress(
	ctx context.Context,
	resume migrationResumeContext,
	states map[StoreID]State,
) error {
	operations := make(map[string]struct {
		storeID   StoreID
		order     int
		operation Operation
	}, len(c.plan.Operations()))
	for _, planned := range c.plan.plannedOperations() {
		if planned.phase != resume.phase {
			continue
		}
		definition := planned.operation.Definition()
		operations[string(definition.ID)] = struct {
			storeID   StoreID
			order     int
			operation Operation
		}{storeID: definition.StoreID, order: planned.order, operation: planned.operation}
	}
	seenProgress := make(map[string]struct{})
	selectedDeployments := make(map[string]struct{}, len(resume.selection.DeploymentIDs))
	for _, deploymentID := range resume.selection.DeploymentIDs {
		selectedDeployments[deploymentID] = struct{}{}
	}
	for storeID, state := range states {
		namespaceIndex := c.namespaceIndexes()[storeID]
		progress, err := c.namespaces[namespaceIndex].metadataStore.ListProgress(ctx, state.ID, resume.runID)
		if err != nil {
			return fmt.Errorf("inspect migration resume progress for store %q: %w", storeID, err)
		}
		for _, item := range progress {
			expected, exists := operations[item.OperationID]
			if !exists || expected.storeID != storeID || expected.order != item.OperationOrder {
				return fmt.Errorf("inspect migration resume progress for store %q: operation %q does not match the plan",
					storeID, item.OperationID)
			}
			if item.RunID != resume.runID || item.MigrationStateID != state.ID ||
				item.TargetLevel != state.TargetLevel {
				return fmt.Errorf("inspect migration resume progress for store %q: operation %q does not match the active run",
					storeID, item.OperationID)
			}
			operationID := OperationID(item.OperationID)
			key := operationTargetKey(operationID, item.ExecutionTargetID)
			if _, duplicate := seenProgress[key]; duplicate {
				return fmt.Errorf("inspect migration resume progress: operation %q target %q is duplicated",
					item.OperationID, item.ExecutionTargetID)
			}
			seenProgress[key] = struct{}{}
			if item.ExecutionTargetID != "" {
				if _, selected := selectedDeployments[item.ExecutionTargetID]; !selected {
					return fmt.Errorf("inspect migration resume progress: operation %q references unselected target %q",
						item.OperationID, item.ExecutionTargetID)
				}
			}
			switch item.Status {
			case StatusCompleted:
				resume.completedTargets[key] = struct{}{}
				if item.ExecutionTargetID == "" {
					resume.completedOperations[operationID] = struct{}{}
				}
			case StatusInProgress:
				mode := expected.operation.Definition().InterruptionMode
				if mode != InterruptionModeResumable && mode != InterruptionModeOnline {
					return fmt.Errorf(
						"inspect migration resume progress for store %q: atomic operation %q is in progress",
						storeID, item.OperationID)
				}
				resume.resumableTargets[key] = item
				if item.ExecutionTargetID == "" {
					resume.resumableProgress[operationID] = item
				}
			default:
				return fmt.Errorf(
					"inspect migration resume progress for store %q: operation %q has unsupported status %q",
					storeID, item.OperationID, item.Status)
			}
		}
	}
	for storeID := range states {
		targetIDs := append([]string(nil), resume.selection.DeploymentIDs...)
		if len(targetIDs) == 0 {
			targetIDs = []string{""}
		}
		for _, targetID := range targetIDs {
			if err := c.validateTargetProgressPrefix(storeID, resume, targetID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *MigrationCoordinator) validateTargetProgressPrefix(
	storeID StoreID,
	resume migrationResumeContext,
	targetID string,
) error {
	pendingFound := false
	activeFound := false
	for _, planned := range c.plan.plannedOperations() {
		if planned.phase != resume.phase || planned.operation.Definition().StoreID != storeID {
			continue
		}
		definition := planned.operation.Definition()
		operationTargetID := targetID
		if definition.Kind == OperationKindSchemaChange {
			operationTargetID = ""
		}
		key := operationTargetKey(definition.ID, operationTargetID)
		if _, completed := resume.completedTargets[key]; completed {
			if pendingFound {
				return fmt.Errorf(
					"inspect migration resume progress for store %q target %q: completed operations are not a plan prefix",
					storeID, targetID)
			}
			continue
		}
		if _, active := resume.resumableTargets[key]; active {
			if activeFound || pendingFound {
				return fmt.Errorf(
					"inspect migration resume progress for store %q target %q: in-progress operation is not the first pending operation",
					storeID, targetID)
			}
			activeFound = true
		}
		pendingFound = true
	}
	return nil
}

func (c *MigrationCoordinator) metadataStoresByStore() map[StoreID]*MetadataStore {
	stores := make(map[StoreID]*MetadataStore, len(c.release.Manifest.Stores))
	for _, namespace := range c.namespaces {
		for _, storeID := range namespace.storeIDs {
			stores[storeID] = namespace.metadataStore
		}
	}
	return stores
}

func (c *MigrationCoordinator) namespaceIndexes() map[StoreID]int {
	indexes := make(map[StoreID]int, len(c.release.Manifest.Stores))
	for index, namespace := range c.namespaces {
		for _, storeID := range namespace.storeIDs {
			indexes[storeID] = index
		}
	}
	return indexes
}

func (c *MigrationCoordinator) cancelReservations(
	ctx context.Context,
	reservedStates map[int][]State,
	runID string,
) error {
	var errs []error
	for index := range c.namespaces {
		states := reservedStates[index]
		if len(states) == 0 {
			continue
		}
		if _, err := c.namespaces[index].controller.CancelReservations(ctx, states, runID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *MigrationCoordinator) markFailed(
	ctx context.Context,
	reservedStores map[StoreID]struct{},
	runID string,
	cause error,
) error {
	return c.markFailedWithFailure(ctx, reservedStores, runID, nil, cause)
}

func (c *MigrationCoordinator) markFailedWithFailure(
	ctx context.Context,
	reservedStores map[StoreID]struct{},
	runID string,
	failure *MigrationFailure,
	cause error,
) error {
	var errs []error
	summary := boundedFailureSummary(cause.Error())
	for index := range c.namespaces {
		storeIDs := includedStoreIDs(c.namespaces[index].storeIDs, reservedStores)
		if len(storeIDs) == 0 {
			continue
		}
		if _, err := c.namespaces[index].controller.MarkFailedWithFailure(
			ctx, storeIDs, runID, summary, failure); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *MigrationCoordinator) failRun(
	ctx context.Context,
	reservedStores map[StoreID]struct{},
	runID string,
	failure operationFailure,
	cause error,
) error {
	record := MigrationFailure{
		RunID: runID, StoreID: failure.storeID,
		ForwardOperationID: failure.forwardOperationID,
		CurrentOperationID: failure.currentOperationID,
		ExecutionTargetID:  failure.executionTargetID,
		ExecutionDirection: failure.executionDirection,
		FailureSummary:     boundedFailureSummary(cause.Error()), UpdatedAt: time.Now().UTC(),
	}
	return c.markFailedWithFailure(ctx, reservedStores, runID, &record, cause)
}

func migrationStoreSet(report PreflightReport) map[StoreID]struct{} {
	stores := make(map[StoreID]struct{})
	for _, store := range report.Stores {
		if store.State == StoreAssessmentMigrationRequired {
			stores[store.StoreID] = struct{}{}
		}
	}
	return stores
}

func includedStoreIDs(ordered []StoreID, included map[StoreID]struct{}) []StoreID {
	storeIDs := make([]StoreID, 0, len(ordered))
	for _, storeID := range ordered {
		if _, exists := included[storeID]; exists {
			storeIDs = append(storeIDs, storeID)
		}
	}
	return storeIDs
}

func boundedFailureSummary(summary string) string {
	if utf8.RuneCountInString(summary) <= maxFailureSummaryLength {
		return summary
	}
	runes := []rune(summary)
	return string(runes[:maxFailureSummaryLength])
}
