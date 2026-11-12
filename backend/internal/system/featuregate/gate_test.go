// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGate(t *testing.T) {
	gate := NewGate(UserRegistration, AccountRecovery)

	assert.False(t, gate.IsEnabled(UserRegistration))
	assert.False(t, gate.IsEnabled(AccountRecovery))
	assert.True(t, gate.IsEnabled(EntityProvisioning))
	assert.True(t, gate.IsEnabled(OrganizationUnitCreation))
}

func TestNilGateEnablesFeatures(t *testing.T) {
	var gate *Gate

	assert.True(t, gate.IsEnabled(UserRegistration))
}
