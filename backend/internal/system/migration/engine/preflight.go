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
	operationIndex        int
	precedingOperationIDs []OperationID
}

func newPlanContext(plan MigrationPlan, operationIndex int) PlanContext {
	preceding := make([]OperationID, 0, operationIndex)
	for _, operation := range plan.Operations[:operationIndex] {
		preceding = append(preceding, operation.Definition().ID)
	}
	return PlanContext{
		targetVersion:         plan.TargetVersion,
		operationIndex:        operationIndex,
		precedingOperationIDs: preceding,
	}
}

// TargetVersion returns the release targeted by the complete migration plan.
func (c PlanContext) TargetVersion() string {
	return c.targetVersion
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

type operationReadinessChecker struct {
	plan   MigrationPlan
	stores map[StoreID]ReadStore
}

func newOperationReadinessChecker(
	release ReleaseDefinition,
	stores map[StoreID]ReadStore,
) (*operationReadinessChecker, error) {
	registry, err := NewPlanRegistry(release.Manifest, release.Plan)
	if err != nil {
		return nil, fmt.Errorf("create migration preflight checker: %w", err)
	}
	storeCopy := make(map[StoreID]ReadStore, len(stores))
	for storeID, store := range stores {
		storeCopy[storeID] = store
	}
	return &operationReadinessChecker{plan: registry.Plan(), stores: storeCopy}, nil
}

func (c *operationReadinessChecker) check(ctx context.Context) operationReadinessReport {
	return c.checkStores(ctx, nil, nil)
}

func (c *operationReadinessChecker) checkStores(
	ctx context.Context,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
) operationReadinessReport {
	return c.checkPendingStores(ctx, includedStores, blockedStores, nil)
}

func (c *operationReadinessChecker) checkPendingStores(
	ctx context.Context,
	includedStores map[StoreID]struct{},
	blockedStores map[StoreID]struct{},
	completedOperations map[OperationID]struct{},
) operationReadinessReport {
	report := operationReadinessReport{
		TargetVersion: c.plan.TargetVersion,
		Operations:    make([]OperationReadinessReport, 0, len(c.plan.Operations)),
	}
	resultsByID := make(map[OperationID]OperationReadinessReport, len(c.plan.Operations))
	for index, operation := range c.plan.Operations {
		definition := operation.Definition()
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
		planContext := newPlanContext(c.plan, index)
		access, err := newOperationReadAccess(definition, c.stores)
		if err != nil {
			result := failedReadinessReport(definition, "Operation read access is unavailable",
				"read_access_unavailable", "The operation's declared stores could not be accessed", err)
			report.Operations = append(report.Operations, result)
			resultsByID[definition.ID] = result
			continue
		}

		readiness, err := operation.CheckReadiness(ctx, access, planContext)
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
	State               StoreMigrationAssessmentState
	Reason              CompatibilityReason
	Err                 error
}

// Ready reports whether the store is either already complete or can be migrated by this release.
func (r StoreMigrationAssessment) Ready() bool {
	return r.State == StoreAssessmentAtTarget || r.State == StoreAssessmentMigrationRequired
}

// PreflightReport combines store applicability and operation readiness.
type PreflightReport struct {
	TargetVersion string
	Stores        []StoreMigrationAssessment
	Operations    []OperationReadinessReport
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
	manifest          ReleaseManifest
	compatibilityGate *CompatibilityGate
	operationChecker  *operationReadinessChecker
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
	operationChecker, err := newOperationReadinessChecker(release, stores)
	if err != nil {
		return nil, err
	}
	return &PreflightChecker{
		manifest: release.Manifest, compatibilityGate: compatibilityGate, operationChecker: operationChecker,
	}, nil
}

// Check evaluates store applicability and then checks operations for stores requiring migration.
func (c *PreflightChecker) Check(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
) PreflightReport {
	compatibility := c.compatibilityGate.Check(ctx, metadataStores)
	report := PreflightReport{
		TargetVersion: c.manifest.ReleaseVersion,
		Stores:        make([]StoreMigrationAssessment, 0, len(c.manifest.Stores)),
	}
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
			Status: storeCompatibility.Status, Reason: storeCompatibility.Reason, Err: storeCompatibility.Err,
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
		report.Stores = append(report.Stores, storeAssessment)
	}

	operationReport := c.operationChecker.checkStores(ctx, migrationStores, blockedStores)
	report.Operations = operationReport.Operations
	return report
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
