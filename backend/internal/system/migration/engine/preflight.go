// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
)

// ReadAccess provides query-only access to the stores declared by a migration operation.
type ReadAccess interface {
	Query(
		ctx context.Context,
		storeID StoreID,
		query dbmodel.DBQuery,
		args ...interface{},
	) ([]QueryRow, error)
	DatabaseType(storeID StoreID) (DatabaseType, error)
}

// PlanContext describes an operation's immutable position in its validated migration plan.
type PlanContext struct {
	targetVersion         string
	phase                 OperationPhase
	operationIndex        int
	precedingOperationIDs []OperationID
}

func newPlanContext(plan MigrationPlan, operationIndex int) PlanContext {
	planned := plan.plannedOperations()
	preceding := make([]OperationID, 0, operationIndex)
	for _, entry := range planned[:operationIndex] {
		preceding = append(preceding, entry.operation.Definition().ID)
	}
	return PlanContext{
		targetVersion:         plan.TargetVersion,
		phase:                 planned[operationIndex].phase,
		operationIndex:        operationIndex,
		precedingOperationIDs: preceding,
	}
}

// TargetVersion returns the release targeted by the complete migration plan.
func (c PlanContext) TargetVersion() string {
	return c.targetVersion
}

// Phase returns the release-plan phase containing this operation.
func (c PlanContext) Phase() OperationPhase {
	return c.phase
}

// OperationIndex returns the zero-based position of the operation in the plan.
func (c PlanContext) OperationIndex() int {
	return c.operationIndex
}

// PrecedingOperationIDs returns an independent copy of the earlier operation identifiers.
func (c PlanContext) PrecedingOperationIDs() []OperationID {
	return append([]OperationID(nil), c.precedingOperationIDs...)
}

// Precedes reports whether an operation appears earlier in the validated plan.
func (c PlanContext) Precedes(operationID OperationID) bool {
	for _, precedingID := range c.precedingOperationIDs {
		if precedingID == operationID {
			return true
		}
	}
	return false
}

// ReadinessState classifies whether an operation can participate in the migration plan.
type ReadinessState string

const (
	// ReadinessReady means all conditions observable before execution are satisfied.
	ReadinessReady ReadinessState = "ready"
	// ReadinessDeferred means exact validation depends on declared preceding operations.
	ReadinessDeferred ReadinessState = "deferred"
	// ReadinessBlocked means a known condition prevents safe execution.
	ReadinessBlocked ReadinessState = "blocked"
)

// Finding is one safe, actionable observation produced by an operation readiness check.
type Finding struct {
	Code        string
	Message     string
	Remediation string
}

// ReadinessResult is the standardized output of one operation readiness check.
type ReadinessResult struct {
	State         ReadinessState
	Summary       string
	Findings      []Finding
	DeferredUntil []OperationID
}

// ReadStore binds one logical store to its provider type and query client.
type ReadStore struct {
	DatabaseType DatabaseType
	Client       dbprovider.DBClientInterface
}

type operationReadAccess struct {
	stores map[StoreID]ReadStore
}

func newOperationReadAccess(
	definition OperationDefinition,
	configuredStores map[StoreID]ReadStore,
) (*operationReadAccess, error) {
	storeIDs := make([]StoreID, 0, len(definition.ReadStoreIDs)+1)
	storeIDs = append(storeIDs, definition.StoreID)
	storeIDs = append(storeIDs, definition.ReadStoreIDs...)

	stores := make(map[StoreID]ReadStore, len(storeIDs))
	for _, storeID := range storeIDs {
		store, exists := configuredStores[storeID]
		if !exists || store.Client == nil {
			return nil, fmt.Errorf("read access for store %q is not configured", storeID)
		}
		if strings.TrimSpace(string(store.DatabaseType)) == "" {
			return nil, fmt.Errorf("database type for store %q is not configured", storeID)
		}
		stores[storeID] = store
	}
	return &operationReadAccess{stores: stores}, nil
}

func (a *operationReadAccess) Query(
	ctx context.Context,
	storeID StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]QueryRow, error) {
	store, exists := a.stores[storeID]
	if !exists {
		return nil, fmt.Errorf("operation did not declare read access to store %q", storeID)
	}
	if strings.TrimSpace(query.GetQuery(string(store.DatabaseType))) == "" {
		return nil, fmt.Errorf("query %q has no implementation for database type %q",
			query.GetID(), store.DatabaseType)
	}
	rows, err := store.Client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result := make([]QueryRow, len(rows))
	for index, row := range rows {
		result[index] = QueryRow(row)
	}
	return result, nil
}

func (a *operationReadAccess) DatabaseType(storeID StoreID) (DatabaseType, error) {
	store, exists := a.stores[storeID]
	if !exists {
		return "", fmt.Errorf("operation did not declare read access to store %q", storeID)
	}
	return store.DatabaseType, nil
}

// OperationReadinessReport contains the validated readiness result for one operation.
type OperationReadinessReport struct {
	OperationID   OperationID
	Description   string
	StoreID       StoreID
	State         ReadinessState
	Summary       string
	Findings      []Finding
	DeferredUntil []OperationID
	Err           error
}

// Ready reports whether this operation permits the plan to proceed.
func (r OperationReadinessReport) Ready() bool {
	return r.Err == nil && (r.State == ReadinessReady || r.State == ReadinessDeferred)
}

type operationReadinessReport struct {
	TargetVersion string
	Operations    []OperationReadinessReport
}

func (r operationReadinessReport) Ready() bool {
	for _, operation := range r.Operations {
		if !operation.Ready() {
			return false
		}
	}
	return true
}

type readinessEvaluator struct {
	plan   MigrationPlan
	stores map[StoreID]ReadStore
}

func newReadinessEvaluator(
	release ReleaseDefinition,
	stores map[StoreID]ReadStore,
) (*readinessEvaluator, error) {
	registry, err := NewPlanRegistry(release.Manifest, release.Plan)
	if err != nil {
		return nil, fmt.Errorf("create migration preflight checker: %w", err)
	}
	storeCopy := make(map[StoreID]ReadStore, len(stores))
	for storeID, store := range stores {
		storeCopy[storeID] = store
	}
	return &readinessEvaluator{plan: registry.Plan(), stores: storeCopy}, nil
}

func (c *readinessEvaluator) check(ctx context.Context) operationReadinessReport {
	return c.checkStores(ctx, nil, nil)
}

func (c *readinessEvaluator) checkStores(
	ctx context.Context,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
) operationReadinessReport {
	return c.checkPendingStores(ctx, includedStores, blockedStores, nil)
}

func (c *readinessEvaluator) checkPhaseStores(
	ctx context.Context,
	phase OperationPhase,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
) operationReadinessReport {
	return c.checkPendingPhaseStores(ctx, phase, includedStores, blockedStores, nil)
}

func (c *readinessEvaluator) checkPendingStores(
	ctx context.Context,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
	completedOperations map[OperationID]struct{},
) operationReadinessReport {
	return c.checkPendingStoresForPhase(ctx, nil, false, includedStores, blockedStores, completedOperations)
}

func (c *readinessEvaluator) checkPendingPhaseStores(
	ctx context.Context,
	phase OperationPhase,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
	completedOperations map[OperationID]struct{},
) operationReadinessReport {
	return c.checkPendingStoresForPhase(
		ctx, &phase, false, includedStores, blockedStores, completedOperations)
}

func (c *readinessEvaluator) checkSafeWorkflowStores(
	ctx context.Context,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
) operationReadinessReport {
	lastPhase := OperationPhaseMigrate
	return c.checkPendingStoresForPhase(ctx, &lastPhase, true, includedStores, blockedStores, nil)
}

func (c *readinessEvaluator) checkPendingStoresForPhase(
	ctx context.Context,
	phase *OperationPhase,
	includePrecedingPhases bool,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
	completedOperations map[OperationID]struct{},
) operationReadinessReport {
	report := operationReadinessReport{
		TargetVersion: c.plan.TargetVersion,
		Operations:    make([]OperationReadinessReport, 0, len(c.plan.Operations())),
	}
	plannedOperations := c.plan.plannedOperations()
	resultsByID := make(map[OperationID]OperationReadinessReport, len(plannedOperations))
	for _, planned := range plannedOperations {
		operation := planned.operation
		definition := operation.Definition()
		if phase != nil && planned.phase != *phase {
			if includePrecedingPhases {
				if phasePrecedes(*phase, planned.phase) {
					continue
				}
			} else {
				if phasePrecedes(planned.phase, *phase) {
					resultsByID[definition.ID] = OperationReadinessReport{
						OperationID: definition.ID, State: ReadinessReady,
						Summary: "The preceding migration phase is complete",
					}
				}
				continue
			}
		}
		if _, completed := completedOperations[definition.ID]; completed {
			result := OperationReadinessReport{
				OperationID: definition.ID, Description: definition.Description,
				StoreID: definition.StoreID, State: ReadinessReady,
				Summary: "The operation already completed in the resumed run",
			}
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}
		if includedStores != nil {
			if _, included := includedStores[definition.StoreID]; !included {
				if _, blocked := blockedStores[definition.StoreID]; blocked {
					resultsByID[definition.ID] = failedReadinessReport(definition,
						"The operation's writable store is blocked by its migration assessment",
						"store_assessment_blocked",
						fmt.Sprintf("Store %q must pass its migration assessment", definition.StoreID), nil)
				} else {
					resultsByID[definition.ID] = OperationReadinessReport{
						OperationID: definition.ID, Description: definition.Description,
						StoreID: definition.StoreID, State: ReadinessReady,
						Summary: "The operation's store is already at the target level",
					}
				}
				continue
			}
		}
		blockedStoreID := blockedDeclaredStore(definition, blockedStores)
		if blockedStoreID != "" {
			result := failedReadinessReport(definition, "A declared store is blocked by its migration assessment",
				"store_assessment_blocked",
				fmt.Sprintf("Store %q must pass its migration assessment before this operation can be checked",
					blockedStoreID), nil)
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}
		checker, supportsReadinessCheck := operation.(ReadinessChecker)
		if !supportsReadinessCheck {
			result := OperationReadinessReport{
				OperationID: definition.ID, Description: definition.Description,
				StoreID: definition.StoreID, State: ReadinessReady,
				Summary: "No additional operation readiness check is required",
			}
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}

		planContext := newPlanContext(c.plan, planned.order)
		access, err := newOperationReadAccess(definition, c.stores)
		if err != nil {
			result := failedReadinessReport(definition, "Operation read access is unavailable",
				"read_access_unavailable", "The operation's declared stores could not be accessed", err)
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}

		readiness, err := checker.CheckReadiness(ctx, access, planContext)
		if err != nil {
			result := failedReadinessReport(definition, "Operation readiness could not be determined",
				"readiness_check_failed", "The operation readiness check returned an error", err)
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}
		if err := readiness.validate(planContext); err != nil {
			result := failedReadinessReport(definition, "Operation returned an invalid readiness result",
				"invalid_readiness_result", "The operation readiness implementation must be corrected", err)
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}

		result := OperationReadinessReport{
			OperationID: definition.ID, Description: definition.Description, StoreID: definition.StoreID,
			State: readiness.State, Summary: readiness.Summary,
			Findings:      append([]Finding(nil), readiness.Findings...),
			DeferredUntil: append([]OperationID(nil), readiness.DeferredUntil...),
		}
		if readiness.State == ReadinessDeferred {
			for _, dependencyID := range readiness.DeferredUntil {
				if dependency := resultsByID[dependencyID]; !dependency.Ready() {
					result = failedReadinessReport(definition, "A required preceding operation is blocked",
						"preceding_operation_blocked",
						fmt.Sprintf("Preceding operation %q must be ready before this operation", dependencyID),
						dependency.Err)
					break
				}
			}
		}
		report.Operations = append(report.Operations, result)
		resultsByID[definition.ID] = result
	}
	return report
}

func phasePrecedes(candidate OperationPhase, target OperationPhase) bool {
	order := map[OperationPhase]int{
		OperationPhaseExpand: 0, OperationPhaseMigrate: 1, OperationPhaseContract: 2,
	}
	return order[candidate] < order[target]
}

func blockedDeclaredStore(
	definition OperationDefinition,
	blockedStores map[StoreID]struct{},
) StoreID {
	if _, blocked := blockedStores[definition.StoreID]; blocked {
		return definition.StoreID
	}
	for _, storeID := range definition.ReadStoreIDs {
		if _, blocked := blockedStores[storeID]; blocked {
			return storeID
		}
	}
	return ""
}

// StoreMigrationAssessmentState classifies a logical store before migration execution.
type StoreMigrationAssessmentState string

const (
	// StoreAssessmentAtTarget means that the store already satisfies the target release.
	StoreAssessmentAtTarget StoreMigrationAssessmentState = "at_target"
	// StoreAssessmentPhaseComplete means that the requested phase already completed for the store.
	StoreAssessmentPhaseComplete StoreMigrationAssessmentState = "phase_complete"
	// StoreAssessmentMigrationRequired means that the store is at the accepted source level.
	StoreAssessmentMigrationRequired StoreMigrationAssessmentState = "migration_required"
	// StoreAssessmentBlocked means that the store is not a recognized migration source or target.
	StoreAssessmentBlocked StoreMigrationAssessmentState = "blocked"
)

// StoreMigrationAssessment contains the migration applicability result for one logical store.
type StoreMigrationAssessment struct {
	StoreID             StoreID
	AcceptedSourceLevel string
	RequiredLevel       string
	CompletedLevel      string
	Status              Status
	Stage               MigrationStage
	State               StoreMigrationAssessmentState
	Reason              CompatibilityReason
	Err                 error
}

// Ready reports whether the store is either already complete or can be migrated by this release.
func (r StoreMigrationAssessment) Ready() bool {
	return r.State == StoreAssessmentAtTarget || r.State == StoreAssessmentPhaseComplete ||
		r.State == StoreAssessmentMigrationRequired
}

// PreflightReport combines store applicability and operation readiness.
type PreflightReport struct {
	TargetVersion string
	Phase         OperationPhase
	Stores        []StoreMigrationAssessment
	Operations    []OperationReadinessReport
}

// WorkflowPhasePreflight contains operation readiness for one phase in the safe workflow.
type WorkflowPhasePreflight struct {
	Phase      OperationPhase
	Operations []OperationReadinessReport
}

// Ready reports whether every operation in this workflow phase can proceed.
func (r WorkflowPhasePreflight) Ready() bool {
	for _, operation := range r.Operations {
		if !operation.Ready() {
			return false
		}
	}
	return true
}

// WorkflowPreflightReport contains the complete read-only assessment for expand followed by migrate.
type WorkflowPreflightReport struct {
	TargetVersion string
	Stores        []StoreMigrationAssessment
	Phases        []WorkflowPhasePreflight
}

// Ready reports whether all stores and safe workflow phases can proceed without a known blocker.
func (r WorkflowPreflightReport) Ready() bool {
	if len(r.Stores) == 0 || len(r.Phases) == 0 {
		return false
	}
	for _, store := range r.Stores {
		if !store.Ready() {
			return false
		}
	}
	for _, phase := range r.Phases {
		if !phase.Ready() {
			return false
		}
	}
	return true
}

// Ready reports whether the migration can proceed without a known preflight blocker.
func (r PreflightReport) Ready() bool {
	if len(r.Stores) == 0 {
		return false
	}
	for _, store := range r.Stores {
		if !store.Ready() {
			return false
		}
	}
	for _, operation := range r.Operations {
		if !operation.Ready() {
			return false
		}
	}
	return true
}

// PreflightChecker combines store migration applicability with operation readiness.
type PreflightChecker struct {
	manifest           ReleaseManifest
	plan               MigrationPlan
	compatibilityGate  *CompatibilityGate
	readinessEvaluator *readinessEvaluator
}

// NewPreflightChecker creates the complete read-only checker for one release.
func NewPreflightChecker(
	release ReleaseDefinition,
	stores map[StoreID]ReadStore,
) (*PreflightChecker, error) {
	compatibilityGate, err := NewCompatibilityGate(release.Manifest)
	if err != nil {
		return nil, fmt.Errorf("create migration preflight checker: %w", err)
	}
	readinessEvaluator, err := newReadinessEvaluator(release, stores)
	if err != nil {
		return nil, err
	}
	return &PreflightChecker{
		manifest: release.Manifest, plan: readinessEvaluator.plan,
		compatibilityGate: compatibilityGate, readinessEvaluator: readinessEvaluator,
	}, nil
}

// Check evaluates store applicability and then checks operations for stores requiring migration.
func (c *PreflightChecker) Check(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
) PreflightReport {
	stores, migrationStores, blockedStores := c.assessStores(ctx, metadataStores)
	report := PreflightReport{TargetVersion: c.manifest.ReleaseVersion, Stores: stores}
	operationReport := c.readinessEvaluator.checkStores(ctx, migrationStores, blockedStores)
	report.Operations = operationReport.Operations
	return report
}

// CheckSafeWorkflow evaluates expand and migrate together without including contract operations.
func (c *PreflightChecker) CheckSafeWorkflow(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
) WorkflowPreflightReport {
	stores, migrationStores, blockedStores := c.assessStores(ctx, metadataStores)
	operationReport := c.readinessEvaluator.checkSafeWorkflowStores(ctx, migrationStores, blockedStores)
	operationsByID := make(map[OperationID]OperationReadinessReport, len(operationReport.Operations))
	for _, operation := range operationReport.Operations {
		operationsByID[operation.OperationID] = operation
	}

	report := WorkflowPreflightReport{
		TargetVersion: c.manifest.ReleaseVersion,
		Stores:        stores,
		Phases:        make([]WorkflowPhasePreflight, 0, 2),
	}
	for _, phase := range []OperationPhase{OperationPhaseExpand, OperationPhaseMigrate} {
		phaseReport := WorkflowPhasePreflight{Phase: phase}
		operations, _ := c.plan.OperationsForPhase(phase)
		for _, operation := range operations {
			if readiness, exists := operationsByID[operation.Definition().ID]; exists {
				phaseReport.Operations = append(phaseReport.Operations, readiness)
			}
		}
		report.Phases = append(report.Phases, phaseReport)
	}
	return report
}

func (c *PreflightChecker) assessStores(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
) ([]StoreMigrationAssessment, map[StoreID]struct{}, map[StoreID]struct{}) {
	compatibility := c.compatibilityGate.Check(ctx, metadataStores)
	stores := make([]StoreMigrationAssessment, 0, len(c.manifest.Stores))
	requirements := make(map[StoreID]StoreRequirement, len(c.manifest.Stores))
	for _, requirement := range c.manifest.Stores {
		requirements[requirement.ID] = requirement
	}

	migrationStores := make(map[StoreID]struct{})
	blockedStores := make(map[StoreID]struct{})
	for _, storeCompatibility := range compatibility.Stores {
		requirement := requirements[storeCompatibility.StoreID]
		storeAssessment := StoreMigrationAssessment{
			StoreID: storeCompatibility.StoreID, AcceptedSourceLevel: requirement.AcceptedSourceLevel,
			RequiredLevel: requirement.RequiredLevel, CompletedLevel: storeCompatibility.CompletedLevel,
			Status: storeCompatibility.Status, Stage: storeCompatibility.Stage,
			Reason: storeCompatibility.Reason, Err: storeCompatibility.Err,
		}
		switch {
		case storeCompatibility.Compatible():
			storeAssessment.State = StoreAssessmentAtTarget
			storeAssessment.Reason = CompatibilityReasonNone
		case storeCompatibility.Reason == CompatibilityReasonLevelMismatch && requirement.Changed &&
			storeCompatibility.CompletedLevel == requirement.AcceptedSourceLevel:
			storeAssessment.State = StoreAssessmentMigrationRequired
			storeAssessment.Reason = CompatibilityReasonNone
			migrationStores[requirement.ID] = struct{}{}
		default:
			storeAssessment.State = StoreAssessmentBlocked
			blockedStores[requirement.ID] = struct{}{}
		}
		stores = append(stores, storeAssessment)
	}
	return stores, migrationStores, blockedStores
}

// CheckPhase evaluates whether each store and operation can execute the selected migration phase.
func (c *PreflightChecker) CheckPhase(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
	phase OperationPhase,
) (PreflightReport, error) {
	if _, err := executionStageForPhase(phase); err != nil {
		return PreflightReport{}, fmt.Errorf("check migration phase: %w", err)
	}
	report := PreflightReport{
		TargetVersion: c.manifest.ReleaseVersion,
		Phase:         phase,
		Stores:        make([]StoreMigrationAssessment, 0, len(c.manifest.Stores)),
	}
	migrationStores := make(map[StoreID]struct{})
	blockedStores := make(map[StoreID]struct{})
	for _, requirement := range c.manifest.Stores {
		assessment, requiresMigration := c.assessPhaseStore(
			ctx, metadataStores[requirement.ID], requirement, phase)
		report.Stores = append(report.Stores, assessment)
		if requiresMigration {
			migrationStores[requirement.ID] = struct{}{}
		} else if assessment.State == StoreAssessmentBlocked {
			blockedStores[requirement.ID] = struct{}{}
		}
	}
	operationReport := c.readinessEvaluator.checkPhaseStores(
		ctx, phase, migrationStores, blockedStores)
	report.Operations = operationReport.Operations
	return report, nil
}

func (c *PreflightChecker) assessPhaseStore(
	ctx context.Context,
	metadataStore *MetadataStore,
	requirement StoreRequirement,
	phase OperationPhase,
) (StoreMigrationAssessment, bool) {
	assessment := StoreMigrationAssessment{
		StoreID: requirement.ID, AcceptedSourceLevel: requirement.AcceptedSourceLevel,
		RequiredLevel: requirement.RequiredLevel,
	}
	if metadataStore == nil {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonUnverifiable
		assessment.Err = errors.New("metadata store is not configured")
		return assessment, false
	}
	schemaStatus, err := metadataStore.InspectSchema(ctx)
	if err != nil {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonUnverifiable
		assessment.Err = err
		return assessment, false
	}
	if schemaStatus != SchemaReady {
		assessment.State = StoreAssessmentBlocked
		if schemaStatus == SchemaAbsent {
			assessment.Reason = CompatibilityReasonMetadataMissing
		} else {
			assessment.Reason = CompatibilityReasonMetadataPartial
		}
		return assessment, false
	}
	state, err := metadataStore.GetState(ctx, requirement.ID)
	if err != nil || state == nil {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonUnverifiable
		assessment.Err = err
		if state == nil && err == nil {
			assessment.Reason = CompatibilityReasonMetadataMissing
		}
		return assessment, false
	}
	assessment.CompletedLevel = state.CompletedLevel
	assessment.Status = state.Status
	assessment.Stage = state.Stage
	if state.MetadataVersion != c.manifest.MigrationMetadataVersion {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonUnsupportedMetadata
		return assessment, false
	}
	if state.Status == StatusClean && state.CompletedLevel == requirement.RequiredLevel {
		assessment.State = StoreAssessmentAtTarget
		return assessment, false
	}
	phasePlan, phaseErr := phasePlanForStore(c.plan, requirement.ID, phase)
	if phaseErr != nil {
		if state.Status == StatusInProgress || state.Status == StatusFailed {
			assessment.State = StoreAssessmentBlocked
			assessment.Reason = CompatibilityReasonMigrationIncomplete
			return assessment, false
		}
		assessment.State = StoreAssessmentAtTarget
		return assessment, false
	}
	if state.CompletedLevel != requirement.AcceptedSourceLevel {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonLevelMismatch
		return assessment, false
	}
	if requestedPhaseCompleted(phase, requirement.RequiredLevel, state) {
		assessment.State = StoreAssessmentPhaseComplete
		return assessment, false
	}
	if err := validatePhaseBoundary(
		requirement.ID, phase, phasePlan.previous, requirement.RequiredLevel, state); err != nil {
		assessment.State = StoreAssessmentBlocked
		assessment.Reason = CompatibilityReasonMigrationIncomplete
		assessment.Err = err
		return assessment, false
	}
	assessment.State = StoreAssessmentMigrationRequired
	return assessment, true
}

func requestedPhaseCompleted(phase OperationPhase, targetLevel string, state *State) bool {
	if state.TargetLevel != targetLevel {
		return false
	}
	switch phase {
	case OperationPhaseExpand:
		return state.Stage == MigrationStageExpanded && state.Status == StatusCompleted ||
			state.Stage == MigrationStageMigrating ||
			state.Stage == MigrationStageContractReady && state.Status == StatusCompleted ||
			state.Stage == MigrationStageContracting
	case OperationPhaseMigrate:
		return state.Stage == MigrationStageContractReady && state.Status == StatusCompleted ||
			state.Stage == MigrationStageContracting
	default:
		return false
	}
}

func failedReadinessReport(
	definition OperationDefinition,
	summary string,
	code string,
	message string,
	err error,
) OperationReadinessReport {
	return OperationReadinessReport{
		OperationID: definition.ID,
		Description: definition.Description,
		StoreID:     definition.StoreID,
		State:       ReadinessBlocked,
		Summary:     summary,
		Findings:    []Finding{{Code: code, Message: message}},
		Err:         err,
	}
}

func (r ReadinessResult) validate(planContext PlanContext) error {
	if r.State != ReadinessReady && r.State != ReadinessDeferred && r.State != ReadinessBlocked {
		return fmt.Errorf("unsupported readiness state %q", r.State)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return errors.New("readiness summary is required")
	}
	for index, finding := range r.Findings {
		if strings.TrimSpace(finding.Code) == "" {
			return fmt.Errorf("readiness finding %d code is required", index)
		}
		if strings.TrimSpace(finding.Message) == "" {
			return fmt.Errorf("readiness finding %d message is required", index)
		}
	}
	if r.State == ReadinessBlocked && len(r.Findings) == 0 {
		return errors.New("blocked readiness requires at least one finding")
	}

	if r.State != ReadinessDeferred {
		if len(r.DeferredUntil) > 0 {
			return fmt.Errorf("readiness state %q cannot declare deferred operations", r.State)
		}
		return nil
	}
	if len(r.DeferredUntil) == 0 {
		return errors.New("deferred readiness requires at least one preceding operation")
	}

	seen := make(map[OperationID]struct{}, len(r.DeferredUntil))
	for _, operationID := range r.DeferredUntil {
		if _, exists := seen[operationID]; exists {
			return fmt.Errorf("deferred readiness repeats operation %q", operationID)
		}
		if !planContext.Precedes(operationID) {
			return fmt.Errorf("deferred readiness references operation %q that does not precede this operation",
				operationID)
		}
		seen[operationID] = struct{}{}
	}
	return nil
}
