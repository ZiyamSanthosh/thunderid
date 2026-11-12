// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/internal/system/migration/current"
)

func TestDeclarativeOrganizationUnitListerReadsIdentifierAndHandle(t *testing.T) {
	serverHome := t.TempDir()
	units := filepath.Join(serverHome, "config", "resources", "organization_units")
	require.NoError(t, os.MkdirAll(units, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(units, "system.yaml"), []byte(`resource_type: organization_unit
id: 01900000-0000-7000-8000-000000000001
handle: system
name: System
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(units, "engineering.yaml"), []byte(`resource_type: organization_unit
id: engineering-id
handle: engineering
name: Engineering
`), 0o600))
	config.ResetServerRuntime()
	require.NoError(t, config.InitializeServerRuntime(serverHome, &config.Config{}))
	t.Cleanup(config.ResetServerRuntime)

	lister, err := newDeclarativeOrganizationUnitsProvider()()
	require.NoError(t, err)
	listed, err := lister.ListDeclarativeOrganizationUnits(context.Background())

	require.NoError(t, err)
	assert.ElementsMatch(t, []current.DeclarativeOrganizationUnit{
		{ID: "01900000-0000-7000-8000-000000000001", Handle: "system"},
		{ID: "engineering-id", Handle: "engineering"},
	}, listed)
}

func TestDeclarativeOrganizationUnitListerWithoutResourceFilesListsNothing(t *testing.T) {
	config.ResetServerRuntime()
	require.NoError(t, config.InitializeServerRuntime(t.TempDir(), &config.Config{}))
	t.Cleanup(config.ResetServerRuntime)

	listed, err := declarativeOrganizationUnitLister{}.ListDeclarativeOrganizationUnits(context.Background())

	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestParseDeclarativeOrganizationUnitRejectsInvalidYAML(t *testing.T) {
	_, err := parseDeclarativeOrganizationUnit([]byte("id: [unterminated"))
	require.Error(t, err)
}

func TestDeclarativeOrganizationUnitCollectorRejectsUnexpectedType(t *testing.T) {
	require.Error(t, (&declarativeOrganizationUnitCollector{}).Create("id", "not an organization unit"))
}
