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
	require.Len(t, definition.Plan.Expand, 35)
	require.Len(t, definition.Plan.Migrate, 7)
	require.Len(t, definition.Plan.Contract, 18)
	require.Len(t, definition.Plan.Rollback, 6)
	expectedExpandIDs := []string{
		"2.0.0/configdb/01_create_authorization_pdp_connection",
		"2.0.0/configdb/02_add_entity_type_handle",
		"2.0.0/entitydb/03_expand_entity_type_length",
		"2.0.0/entitydb/07_expand_entity_identifier_primary_key",
		"2.0.0/configdb/12_create_notification_template",
		"2.0.0/configdb/13_create_variable",
		"2.0.0/configdb/14_create_secret",
		"2.0.0/configdb/15_create_gateway",
		"2.0.0/configdb/16_create_configuration_version",
		"2.0.0/configdb/17_create_gateway_applied_version",
		"2.0.0/configdb/18_create_gateway_excluded_resource",
		"2.0.0/configdb/19_create_resource_sharing_policy",
		"2.0.0/configdb/20_create_resource_sharing_policy_unit",
		"2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule",
		"2.0.0/configdb/22_create_resource_overlay_value",
		"2.0.0/configdb/23_expand_entity_types_primary_key",
		"2.0.0/configdb/24_expand_role_primary_key",
		"2.0.0/configdb/25_expand_theme_primary_key",
		"2.0.0/configdb/26_expand_layout_primary_key",
		"2.0.0/configdb/27_expand_inbound_client_primary_key",
		"2.0.0/configdb/28_expand_idp_primary_key",
		"2.0.0/configdb/29_expand_notification_sender_primary_key",
		"2.0.0/configdb/30_expand_certificate_primary_key",
		"2.0.0/configdb/31_expand_resource_server_primary_key",
		"2.0.0/configdb/32_expand_resource_primary_key",
		"2.0.0/configdb/33_expand_action_primary_key",
		"2.0.0/configdb/34_expand_flow_primary_key",
		"2.0.0/configdb/35_expand_presentation_definition_primary_key",
		"2.0.0/configdb/36_expand_credential_configuration_primary_key",
		"2.0.0/entitydb/37_expand_organization_unit_primary_key",
		"2.0.0/entitydb/38_expand_entity_primary_key",
		"2.0.0/entitydb/39_expand_group_primary_key",
		"2.0.0/configdb/40_expand_certificate_reference_id",
		"2.0.0/runtime_persistent/41_expand_revocation_criterion_value",
		"2.0.0/runtime_persistent/42_add_sso_session_participant_application_index",
	}
	for index, expectedID := range expectedExpandIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Expand[index].Definition().ID))
	}
	assert.Equal(t, "2.0.0/configdb/04_backfill_entity_type_handle",
		string(definition.Plan.Migrate[0].Definition().ID))
	assert.Equal(t, "2.0.0/configdb/05_add_entity_type_handle_uniqueness",
		string(definition.Plan.Migrate[1].Definition().ID))
	expectedReferenceMigrationIDs := []string{
		"2.0.0/entitydb/43_migrate_entity_type_references",
		"2.0.0/configdb/44_migrate_inbound_client_entity_type_references",
		"2.0.0/configdb/45_migrate_idp_entity_type_references",
		"2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references",
		"2.0.0/configdb/47_migrate_flow_entity_type_references",
	}
	for index, expectedID := range expectedReferenceMigrationIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Migrate[index+2].Definition().ID))
	}
	assert.Equal(t, "2.0.0/configdb/06_finalize_entity_type_handle",
		string(definition.Plan.Contract[0].Definition().ID))
	expectedPrimaryKeyContractIDs := []string{
		"2.0.0/configdb/23_finalize_entity_types_primary_key",
		"2.0.0/configdb/24_finalize_role_primary_key",
		"2.0.0/configdb/25_finalize_theme_primary_key",
		"2.0.0/configdb/26_finalize_layout_primary_key",
		"2.0.0/configdb/27_finalize_inbound_client_primary_key",
		"2.0.0/configdb/28_finalize_idp_primary_key",
		"2.0.0/configdb/29_finalize_notification_sender_primary_key",
		"2.0.0/configdb/30_finalize_certificate_primary_key",
		"2.0.0/configdb/31_finalize_resource_server_primary_key",
		"2.0.0/configdb/32_finalize_resource_primary_key",
		"2.0.0/configdb/33_finalize_action_primary_key",
		"2.0.0/configdb/34_finalize_flow_primary_key",
		"2.0.0/configdb/35_finalize_presentation_definition_primary_key",
		"2.0.0/configdb/36_finalize_credential_configuration_primary_key",
		"2.0.0/entitydb/37_finalize_organization_unit_primary_key",
		"2.0.0/entitydb/38_finalize_entity_primary_key",
		"2.0.0/entitydb/39_finalize_group_primary_key",
	}
	for index, expectedID := range expectedPrimaryKeyContractIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Contract[index+1].Definition().ID))
	}
	expectedRollbackIDs := []string{
		"2.0.0/configdb/47_migrate_flow_entity_type_references_rollback",
		"2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references_rollback",
		"2.0.0/configdb/45_migrate_idp_entity_type_references_rollback",
		"2.0.0/configdb/44_migrate_inbound_client_entity_type_references_rollback",
		"2.0.0/entitydb/43_migrate_entity_type_references_rollback",
		"2.0.0/configdb/04_backfill_entity_type_handle_rollback",
	}
	expectedRollbackForwardIDs := []string{
		"2.0.0/configdb/47_migrate_flow_entity_type_references",
		"2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references",
		"2.0.0/configdb/45_migrate_idp_entity_type_references",
		"2.0.0/configdb/44_migrate_inbound_client_entity_type_references",
		"2.0.0/entitydb/43_migrate_entity_type_references",
		"2.0.0/configdb/04_backfill_entity_type_handle",
	}
	for index, expectedID := range expectedRollbackIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Rollback[index].Definition().ID))
		assert.Equal(t, expectedRollbackForwardIDs[index],
			string(definition.Plan.Rollback[index].ForwardOperationID()))
	}
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

func TestFreshEntitySchemasUseExpandedIdentifierPrimaryKey(t *testing.T) {
	for _, path := range []string{
		"../../../../../dbscripts/entitydb/postgres.sql",
		"../../../../../dbscripts/entitydb/sqlite.sql",
	} {
		schema, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(schema),
			"PRIMARY KEY (ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)")
	}
}
