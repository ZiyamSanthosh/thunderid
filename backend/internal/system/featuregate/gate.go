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
	// EntityProvisioning controls creating entities, such as users and agents, during authentication.
	EntityProvisioning Feature = "entity.provisioning"
	// OrganizationUnitCreation controls organization-unit creation.
	OrganizationUnitCreation Feature = "organization_unit.creation"
	// AccountLinking controls linking federated accounts inside flows.
	AccountLinking Feature = "account.linking"
	// UserAttributeUpdate controls profile-attribute updates performed inside flows.
	UserAttributeUpdate Feature = "user.attribute_update"
	// CredentialUpdate controls credential-setting and credential-enrollment operations inside flows.
	CredentialUpdate Feature = "user.credential_update"
	// PasskeySignCountUpdate controls recording the sign count a passkey authentication reports.
	PasskeySignCountUpdate Feature = "authentication.passkey_sign_count"
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
