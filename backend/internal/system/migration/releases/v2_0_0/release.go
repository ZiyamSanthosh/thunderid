// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package v2_0_0 contains the immutable migration definition shipped with ThunderID 2.0.0.
package v2_0_0

import (
	_ "embed"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

//go:embed release-manifest.json
var manifestData []byte

// Dependencies contains the dependencies available while constructing this release's operations.
// Add narrowly typed providers only when a concrete operation requires them.
type Dependencies struct{}

// Definition returns the manifest and plan owned by ThunderID 2.0.0.
func Definition() (*engine.ReleaseDefinition, error) {
	return DefinitionWithDependencies(Dependencies{})
}

// DefinitionWithDependencies returns the release definition constructed with the supplied dependencies.
func DefinitionWithDependencies(dependencies Dependencies) (*engine.ReleaseDefinition, error) {
	plan, err := migrationPlan(dependencies)
	if err != nil {
		return nil, err
	}
	return engine.NewReleaseDefinition(manifestData, plan)
}

func migrationPlan(Dependencies) (engine.MigrationPlan, error) {
	removeReasonCheck, err := newRemoveRevocationReasonCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createAuthorizationPDPConnection, err := newCreateAuthorizationPDPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityTypeHandle, err := newAddEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityTypeLength, err := newExpandEntityTypeLengthOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	backfillEntityTypeHandle := newBackfillEntityTypeHandleOperation()
	addEntityTypeHandleUniqueness, err := newAddEntityTypeHandleUniquenessOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeEntityTypeHandle, err := newFinalizeEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	return engine.MigrationPlan{
		TargetVersion: "2.0.0",
		Expand: []engine.Operation{
			removeReasonCheck,
			createAuthorizationPDPConnection,
			addEntityTypeHandle,
			expandEntityTypeLength,
		},
		Migrate: []engine.Operation{
			backfillEntityTypeHandle,
			addEntityTypeHandleUniqueness,
		},
		Contract: []engine.Operation{finalizeEntityTypeHandle},
	}, nil
}
