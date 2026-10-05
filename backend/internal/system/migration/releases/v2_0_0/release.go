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

// Definition returns the manifest and plan owned by ThunderID 2.0.0.
func Definition() (*engine.ReleaseDefinition, error) {
	plan, err := migrationPlan()
	if err != nil {
		return nil, err
	}
	return engine.NewReleaseDefinition(manifestData, plan)
}

func migrationPlan() (engine.MigrationPlan, error) {
	removeReasonCheck, err := newRemoveRevocationReasonCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createAuthorizationPDPConnection, err := newCreateAuthorizationPDPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	return engine.MigrationPlan{
		TargetVersion: "2.0.0",
		Operations: []engine.Operation{
			removeReasonCheck,
			createAuthorizationPDPConnection,
		},
	}, nil
}
