// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
)

// CompatibilityReason identifies why a logical store cannot serve with the current release.
type CompatibilityReason string

const (
	// CompatibilityReasonNone means that the store is compatible.
	CompatibilityReasonNone CompatibilityReason = ""
	// CompatibilityReasonMetadataMissing means that migration metadata is absent.
	CompatibilityReasonMetadataMissing CompatibilityReason = "metadata_missing"
	// CompatibilityReasonMetadataPartial means that only part of the metadata schema exists.
	CompatibilityReasonMetadataPartial CompatibilityReason = "metadata_partial"
	// CompatibilityReasonUnverifiable means that the store's migration state could not be read.
	CompatibilityReasonUnverifiable CompatibilityReason = "unverifiable"
	// CompatibilityReasonUnsupportedMetadata means that the metadata format is not supported.
	CompatibilityReasonUnsupportedMetadata CompatibilityReason = "unsupported_metadata"
	// CompatibilityReasonMigrationIncomplete means that the migration state cannot safely serve this release.
	CompatibilityReasonMigrationIncomplete CompatibilityReason = "migration_incomplete"
	// CompatibilityReasonTargetNotReady means that the target representation is not ready to serve.
	CompatibilityReasonTargetNotReady CompatibilityReason = "target_not_ready"
	// CompatibilityReasonSourceRetired means that contract has started and the source binary must not serve.
	CompatibilityReasonSourceRetired CompatibilityReason = "source_retired"
	// CompatibilityReasonRollbackInProgress means that the target binary must not serve during rollback.
	CompatibilityReasonRollbackInProgress CompatibilityReason = "rollback_in_progress"
	// CompatibilityReasonLevelMismatch means that the completed and required store levels differ.
	CompatibilityReasonLevelMismatch CompatibilityReason = "level_mismatch"
	// CompatibilityReasonUnsupportedProvider means that no migration adapter can inspect the store.
	CompatibilityReasonUnsupportedProvider CompatibilityReason = "unsupported_provider"
)

// StoreCompatibility is the read-only compatibility result for one logical store.
type StoreCompatibility struct {
	StoreID         StoreID
	RequiredLevel   string
	CompletedLevel  string
	MetadataVersion int
	Status          Status
	Stage           MigrationStage
	TargetLevel     string
	ActiveRunID     string
	Reason          CompatibilityReason
	// Skipped reports that no migration adapter was available to inspect this store.
	Skipped bool
	Err     error
}

// Compatible reports whether this logical store can serve with the current release.
func (c StoreCompatibility) Compatible() bool {
	return c.Reason == CompatibilityReasonNone
}

// CompatibilityReport contains the compatibility result for every store in manifest order.
type CompatibilityReport struct {
	Stores               []StoreCompatibility
	NextMigrationRelease string
	MigrationRoute       []string
	RouteErr             error
}

// Compatible reports whether at least one store was checked and every checked store is compatible.
func (r CompatibilityReport) Compatible() bool {
	checked := false
	for _, store := range r.Stores {
		if store.Skipped {
			continue
		}
		checked = true
		if !store.Compatible() {
			return false
		}
	}
	return checked
}

// CompatibilityGate performs the read-only serving compatibility check.
type CompatibilityGate struct {
	manifest      ReleaseManifest
	routeResolver *ReleaseRouteResolver
}

// NewCompatibilityGate creates a gate for the supplied release manifest.
func NewCompatibilityGate(
	manifest ReleaseManifest,
	releaseCatalog ...ReleaseManifest,
) (*CompatibilityGate, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("create migration compatibility gate: %w", err)
	}
	if len(releaseCatalog) == 0 {
		releaseCatalog = []ReleaseManifest{manifest}
	}
	resolver, err := NewReleaseRouteResolver(manifest, releaseCatalog)
	if err != nil {
		return nil, fmt.Errorf("create migration compatibility gate: %w", err)
	}
	return &CompatibilityGate{manifest: manifest, routeResolver: resolver}, nil
}

// Check evaluates every manifest store without modifying migration metadata.
func (g *CompatibilityGate) Check(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
) CompatibilityReport {
	report := CompatibilityReport{Stores: make([]StoreCompatibility, 0, len(g.manifest.Stores))}
	for _, requirement := range g.manifest.Stores {
		metadataStore := metadataStores[requirement.ID]
		report.Stores = append(report.Stores, g.checkStore(ctx, requirement, metadataStore))
	}
	g.resolveMigrationRoute(&report)
	return report
}

// CheckDeployment evaluates serving compatibility for one deployment in a shared physical store.
func (g *CompatibilityGate) CheckDeployment(
	ctx context.Context,
	metadataStores map[StoreID]*MetadataStore,
	deploymentID string,
) CompatibilityReport {
	if deploymentID == "" {
		return g.Check(ctx, metadataStores)
	}
	report := CompatibilityReport{Stores: make([]StoreCompatibility, 0, len(g.manifest.Stores))}
	for _, requirement := range g.manifest.Stores {
		metadataStore := metadataStores[requirement.ID]
		result := g.checkStore(ctx, requirement, metadataStore)
		if result.Reason == CompatibilityReasonTargetNotReady && metadataStore != nil {
			state, err := metadataStore.GetState(ctx, requirement.ID)
			if err != nil {
				result.Reason = CompatibilityReasonUnverifiable
				result.Err = err
			} else if state != nil {
				deploymentState, err := metadataStore.GetDeploymentState(ctx, state.ID, deploymentID)
				switch {
				case err != nil:
					result.Reason = CompatibilityReasonUnverifiable
					result.Err = err
				case deploymentState != nil && deploymentState.CompletedLevel == requirement.RequiredLevel:
					result.Reason = CompatibilityReasonNone
				}
			}
		}
		report.Stores = append(report.Stores, result)
	}
	return report
}

func (g *CompatibilityGate) resolveMigrationRoute(report *CompatibilityReport) {
	if report.Compatible() {
		return
	}
	levels := make(map[StoreID]string, len(report.Stores))
	for _, store := range report.Stores {
		if store.Reason != CompatibilityReasonNone && store.Reason != CompatibilityReasonLevelMismatch {
			return
		}
		levels[store.StoreID] = store.CompletedLevel
	}
	route, err := g.routeResolver.Resolve(levels)
	if err != nil {
		report.RouteErr = err
		return
	}
	report.MigrationRoute = append([]string(nil), route.Releases...)
	report.NextMigrationRelease = route.NextMigrationRelease()
}

func (g *CompatibilityGate) checkStore(
	ctx context.Context,
	requirement StoreRequirement,
	metadataStore *MetadataStore,
) StoreCompatibility {
	result := StoreCompatibility{StoreID: requirement.ID, RequiredLevel: requirement.RequiredLevel}
	if metadataStore == nil {
		result.Reason = CompatibilityReasonUnverifiable
		result.Err = errors.New("metadata store is not configured")
		return result
	}

	schemaStatus, err := metadataStore.InspectSchema(ctx)
	if err != nil {
		result.Reason = CompatibilityReasonUnverifiable
		result.Err = err
		return result
	}
	switch schemaStatus {
	case SchemaAbsent:
		result.Reason = CompatibilityReasonMetadataMissing
		return result
	case SchemaPartial:
		result.Reason = CompatibilityReasonMetadataPartial
		return result
	case SchemaReady:
	default:
		result.Reason = CompatibilityReasonUnverifiable
		result.Err = fmt.Errorf("unsupported metadata schema status %q", schemaStatus)
		return result
	}

	state, err := metadataStore.GetState(ctx, requirement.ID)
	if err != nil {
		result.Reason = CompatibilityReasonUnverifiable
		result.Err = err
		return result
	}
	if state == nil {
		result.Reason = CompatibilityReasonMetadataMissing
		return result
	}

	result.CompletedLevel = state.CompletedLevel
	result.MetadataVersion = state.MetadataVersion
	result.Status = state.Status
	result.Stage = state.Stage
	result.TargetLevel = state.TargetLevel
	result.ActiveRunID = state.ActiveRunID

	if state.MetadataVersion != g.manifest.MigrationMetadataVersion {
		result.Reason = CompatibilityReasonUnsupportedMetadata
		return result
	}
	result.Reason = servingCompatibilityReason(requirement, *state)
	return result
}

func servingCompatibilityReason(requirement StoreRequirement, state State) CompatibilityReason {
	if state.Status == StatusClean {
		if state.CompletedLevel == requirement.RequiredLevel {
			return CompatibilityReasonNone
		}
		return CompatibilityReasonLevelMismatch
	}

	servingAsSource := state.CompletedLevel == requirement.RequiredLevel &&
		state.TargetLevel != requirement.RequiredLevel
	if servingAsSource {
		switch state.Stage {
		case MigrationStageExpanding, MigrationStageExpanded,
			MigrationStageMigrating, MigrationStageContractReady, MigrationStageRollingBack:
			return CompatibilityReasonNone
		case MigrationStageContracting:
			return CompatibilityReasonSourceRetired
		default:
			return CompatibilityReasonMigrationIncomplete
		}
	}

	servingAsTarget := state.CompletedLevel == requirement.AcceptedSourceLevel &&
		state.TargetLevel == requirement.RequiredLevel
	if servingAsTarget {
		switch state.Stage {
		case MigrationStageContractReady, MigrationStageContracting:
			return CompatibilityReasonNone
		case MigrationStageRollingBack:
			return CompatibilityReasonRollbackInProgress
		case MigrationStageExpanding, MigrationStageExpanded, MigrationStageMigrating:
			return CompatibilityReasonTargetNotReady
		default:
			return CompatibilityReasonMigrationIncomplete
		}
	}

	return CompatibilityReasonMigrationIncomplete
}
