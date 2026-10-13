// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestDefinitionMatchesProductVersion(t *testing.T) {
	definition, err := Definition()
	require.NoError(t, err)

	versionBytes, err := os.ReadFile("../../../../../../version.txt")
	require.NoError(t, err)
	expectedVersion := strings.TrimPrefix(strings.TrimSpace(string(versionBytes)), "v")

	assert.Equal(t, expectedVersion, definition.Manifest.ReleaseVersion)
	assert.Equal(t, expectedVersion, definition.Plan.TargetVersion)
	assert.Len(t, definition.Manifest.Stores, 4)
	require.Len(t, definition.Plan.Expand, 4)
	require.Len(t, definition.Plan.Migrate, 2)
	require.Len(t, definition.Plan.Contract, 1)
	require.Len(t, definition.Plan.Rollback, 1)
	assert.Equal(t, "2.0.0/runtime_persistent/01_remove_revocation_reason_check",
		string(definition.Plan.Expand[0].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/02_create_authorization_pdp_connection",
		string(definition.Plan.Expand[1].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/03_add_entity_type_handle",
		string(definition.Plan.Expand[2].Definition().ID))
	assert.Equal(t, "2.0.0/entitydb/04_expand_entity_type_length",
		string(definition.Plan.Expand[3].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/05_backfill_entity_type_handle",
		string(definition.Plan.Migrate[0].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/06_add_entity_type_handle_uniqueness",
		string(definition.Plan.Migrate[1].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/07_finalize_entity_type_handle",
		string(definition.Plan.Contract[0].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/08_rollback_entity_type_handle_backfill",
		string(definition.Plan.Rollback[0].Definition().ID))
	assert.Equal(t, definition.Plan.Migrate[0].Definition().ID,
		definition.Plan.Rollback[0].ForwardOperationID())
	for _, operation := range definition.Plan.Operations() {
		assert.Implements(t, (*engine.ReadinessChecker)(nil), operation)
	}
	for _, store := range definition.Manifest.Stores {
		if store.ID == "runtime_persistent" || store.ID == "configdb" || store.ID == "entitydb" {
			assert.True(t, store.Changed)
			assert.Equal(t, "1.0.0", store.AcceptedSourceLevel)
			assert.Equal(t, "2.0.0", store.RequiredLevel)
		}
	}
}

func TestDefinitionWithDependenciesMatchesDefaultDefinition(t *testing.T) {
	defaultDefinition, err := Definition()
	require.NoError(t, err)
	dependencyDefinition, err := DefinitionWithDependencies(Dependencies{})
	require.NoError(t, err)

	assert.Equal(t, defaultDefinition.Manifest, dependencyDefinition.Manifest)
	assert.Equal(t, defaultDefinition.Plan.TargetVersion, dependencyDefinition.Plan.TargetVersion)
	require.Len(t, dependencyDefinition.Plan.Expand, len(defaultDefinition.Plan.Expand))
	for index := range defaultDefinition.Plan.Expand {
		assert.Equal(t,
			defaultDefinition.Plan.Expand[index].Definition(),
			dependencyDefinition.Plan.Expand[index].Definition())
	}
}

func TestFreshRuntimePersistentSchemasDoNotRestrictRevocationReasons(t *testing.T) {
	for _, path := range []string{
		"../../../../../dbscripts/runtime_persistent/postgres.sql",
		"../../../../../dbscripts/runtime_persistent/sqlite.sql",
	} {
		schema, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(schema),
			"CHECK (REVOCATION_REASON IN ('explicit', 'refresh_rotation'))")
	}
}
