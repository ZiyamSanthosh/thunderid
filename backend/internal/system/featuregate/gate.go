// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package featuregate controls product features that may be disabled at runtime.
package featuregate

// Feature identifies a product feature that can be enabled or disabled independently.
type Feature string

const (
	// UserRegistration controls self-registration and user-onboarding.
	UserRegistration Feature = "user.registration"
	// AccountRecovery controls account-recovery operations.
	AccountRecovery Feature = "account.recovery"
	// AdministrativeOperations controls administrative operations.
	AdministrativeOperations Feature = "administration"
	// UserProvisioning controls user provisioning during authentication.
	UserProvisioning Feature = "user.provisioning"
	// OrganizationUnitCreation controls organization-unit creation.
	OrganizationUnitCreation Feature = "organization_unit.creation"
)

// Gate records the product features currently disabled by the server.
type Gate struct {
	disabled map[Feature]struct{}
}

// NewGate creates a gate that disables the supplied features.
func NewGate(disabled ...Feature) *Gate {
	gate := &Gate{disabled: make(map[Feature]struct{}, len(disabled))}
	for _, feature := range disabled {
		gate.disabled[feature] = struct{}{}
	}
	return gate
}

// IsEnabled reports whether a feature may execute. A nil gate preserves full access for callers
// that do not participate in feature gating.
func (g *Gate) IsEnabled(feature Feature) bool {
	if g == nil {
		return true
	}
	_, disabled := g.disabled[feature]
	return !disabled
}
