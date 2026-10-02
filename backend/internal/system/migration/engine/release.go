// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

// ReleaseDefinition binds one validated manifest to its compiled migration plan.
type ReleaseDefinition struct {
	Manifest ReleaseManifest
	Plan     MigrationPlan
}

// NewReleaseDefinition parses and validates one immutable release definition.
func NewReleaseDefinition(manifestData []byte, plan MigrationPlan) (*ReleaseDefinition, error) {
	manifest, err := LoadReleaseManifest(manifestData)
	if err != nil {
		return nil, err
	}
	registry, err := NewPlanRegistry(*manifest, plan)
	if err != nil {
		return nil, err
	}
	return &ReleaseDefinition{Manifest: *manifest, Plan: registry.Plan()}, nil
}
