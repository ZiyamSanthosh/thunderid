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

func writeDeclarativeConnection(t *testing.T, directory, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600))
}

func TestDeclarativeConnectionListerReadsIdentifierAndType(t *testing.T) {
	serverHome := t.TempDir()
	connections := filepath.Join(serverHome, "config", "resources", "connections")
	writeDeclarativeConnection(t, connections, "google.yaml", `resource_type: connection
id: google-connection
name: Google
type: google
clientId: client
`)
	writeDeclarativeConnection(t, connections, "github.yaml", `resource_type: connection
id: github-connection
name: GitHub
type: github
`)
	config.ResetServerRuntime()
	require.NoError(t, config.InitializeServerRuntime(serverHome, &config.Config{}))
	t.Cleanup(config.ResetServerRuntime)

	lister, err := newDeclarativeConnectionsProvider()()
	require.NoError(t, err)
	listed, err := lister.ListDeclarativeConnections(context.Background())

	require.NoError(t, err)
	assert.ElementsMatch(t, []current.DeclarativeConnection{
		{ID: "google-connection", Type: "google"},
		{ID: "github-connection", Type: "github"},
	}, listed)
}

func TestDeclarativeConnectionListerWithoutResourceFilesListsNothing(t *testing.T) {
	config.ResetServerRuntime()
	require.NoError(t, config.InitializeServerRuntime(t.TempDir(), &config.Config{}))
	t.Cleanup(config.ResetServerRuntime)

	listed, err := declarativeConnectionLister{}.ListDeclarativeConnections(context.Background())

	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestParseDeclarativeConnectionRejectsInvalidYAML(t *testing.T) {
	_, err := parseDeclarativeConnection([]byte("id: [unterminated"))
	require.Error(t, err)
}

func TestDeclarativeConnectionCollectorRejectsUnexpectedType(t *testing.T) {
	require.Error(t, (&declarativeConnectionCollector{}).Create("id", "not a connection"))
}
