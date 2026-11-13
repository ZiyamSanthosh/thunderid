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
	"github.com/thunder-id/thunderid/internal/system/migration/operations"
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
	require.Len(t, definition.Plan.Expand, 88)
	require.Len(t, definition.Plan.Migrate, 15)
	require.Len(t, definition.Plan.Contract, 31)
	require.Len(t, definition.Plan.Rollback, 14)
	expectedExpandIDs := []string{
		"configdb/01_create_authorization_pdp_connection",
		"configdb/02_add_entity_type_handle",
		"entitydb/03_expand_entity_type_length",
		"entitydb/07_expand_entity_identifier_primary_key",
		"runtime_persistent/08_create_entity_runtime_data",
		"entitydb/09_add_entity_state_default",
		"configdb/11_create_notification_template",
		"configdb/12_create_variable",
		"configdb/13_create_secret",
		"configdb/14_create_gateway",
		"configdb/15_create_configuration_version",
		"configdb/16_create_gateway_applied_version",
		"configdb/17_create_gateway_excluded_resource",
		"configdb/18_create_resource_sharing_policy",
		"configdb/19_create_resource_sharing_policy_unit",
		"configdb/20_create_resource_sharing_policy_overlay_rule",
		"configdb/21_create_resource_overlay_value",
		"configdb/22_expand_entity_types_primary_key",
		"configdb/23_expand_role_primary_key",
		"configdb/24_expand_theme_primary_key",
		"configdb/25_expand_layout_primary_key",
		"configdb/26_expand_inbound_client_primary_key",
		"configdb/27_expand_idp_primary_key",
		"configdb/28_expand_notification_sender_primary_key",
		"configdb/29_expand_certificate_primary_key",
		"configdb/30_expand_resource_server_primary_key",
		"configdb/31_expand_resource_primary_key",
		"configdb/32_expand_action_primary_key",
		"configdb/33_expand_flow_primary_key",
		"configdb/34_expand_presentation_definition_primary_key",
		"configdb/35_expand_credential_configuration_primary_key",
		"entitydb/36_expand_organization_unit_primary_key",
		"entitydb/37_expand_entity_primary_key",
		"entitydb/38_expand_group_primary_key",
		"configdb/39_expand_certificate_reference_id",
		"runtime_persistent/40_expand_revocation_criterion_value",
		"runtime_persistent/41_add_sso_session_participant_application_index",
		"runtime_persistent/51_rename_session_tables",
		"runtime_persistent/52_rename_session_indexes",
		"runtime_persistent/53_add_session_type_check",
		"runtime_persistent/54_validate_session_type_check",
		"runtime_persistent/55_add_session_subject_index",
		"runtime_persistent/57_add_session_properties",
		"entitydb/73_create_project_table",
		"entitydb/74_add_entity_project_id",
		"entitydb/75_add_organization_unit_project_id",
		"entitydb/79_add_entity_project_index",
		"entitydb/80_add_entity_owner_index",
		"entitydb/81_add_organization_unit_project_index",
		"configdb/94_add_credential_configuration_ou_index",
		"configdb/95_add_presentation_definition_ou_index",
		"entitydb/114_add_entity_kind_index",
		"configdb/113_create_managed_resource",
		"configdb/72_create_gateway_master",
		"configdb/76_create_http_connection",
		"configdb/77_create_authorization_detail_type",
		"configdb/78_create_gateway_apply_lease",
		"configdb/58_add_inbound_client_device_grant_flow_id",
		"runtime_transient/59_create_runtime_store_device_req_partition",
		"configdb/60_add_role_assignment_sharing_policy_id",
		"configdb/61_widen_role_assignment_primary_key",
		"configdb/91_add_role_assignment_assignee_index",
		"entitydb/62_create_user_share_migration_table",
		"entitydb/63_snapshot_duplicate_users",
		"configdb/64_create_original_user_share_policies",
		"entitydb/65_add_originals_to_duplicate_groups",
		"configdb/04_backfill_entity_type_handle",
		"configdb/05_add_entity_type_handle_uniqueness",
		"entitydb/48_link_legacy_federated_subjects",
		"configdb/82_add_flow_ou_id",
		"configdb/83_add_idp_ou_id",
		"configdb/84_add_notification_sender_ou_id",
		"configdb/97_add_role_project_id",
		"configdb/98_add_theme_project_id",
		"configdb/99_add_layout_project_id",
		"configdb/100_add_resource_server_project_id",
		"configdb/101_add_flow_project_id",
		"configdb/102_add_presentation_definition_project_id",
		"configdb/103_add_credential_configuration_project_id",
		"entitydb/104_add_group_project_id",
		"configdb/105_add_role_project_index",
		"configdb/106_add_theme_project_index",
		"configdb/107_add_layout_project_index",
		"configdb/108_add_resource_server_project_index",
		"configdb/109_add_flow_project_index",
		"configdb/110_add_presentation_definition_project_index",
		"configdb/111_add_credential_configuration_project_index",
		"entitydb/112_add_group_project_index",
	}
	for index, expectedID := range expectedExpandIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Expand[index].Definition().ID))
	}
	expectedReferenceMigrationIDs := []string{
		"entitydb/42_migrate_entity_type_references",
		"configdb/43_migrate_inbound_client_entity_type_references",
		"configdb/44_migrate_idp_entity_type_references",
		"configdb/45_migrate_flow_entity_type_references",
	}
	assert.Equal(t, "configdb/04_backfill_entity_type_handle_catch_up",
		string(definition.Plan.Migrate[0].Definition().ID))
	assert.Equal(t, "entitydb/49_link_legacy_federated_subjects_catch_up",
		string(definition.Plan.Migrate[1].Definition().ID))
	for index, expectedID := range expectedReferenceMigrationIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Migrate[index+2].Definition().ID))
	}
	assert.Equal(t, "configdb/46_migrate_default_agent_name_schema",
		string(definition.Plan.Migrate[6].Definition().ID))
	assert.Equal(t, "entitydb/47_migrate_agent_names_to_attributes",
		string(definition.Plan.Migrate[7].Definition().ID))
	expectedAddedMigrateIDs := []string{
		"entitydb/66_rehome_original_users",
		"entitydb/67_merge_duplicate_linked_accounts",
		"configdb/85_backfill_flow_ou_id",
		"configdb/86_backfill_idp_ou_id",
		"configdb/87_backfill_notification_sender_ou_id",
		"configdb/92_backfill_role_assignment_ou_id",
		"entitydb/116_convert_agent_owner",
	}
	for index, expectedID := range expectedAddedMigrateIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Migrate[index+8].Definition().ID))
	}
	assert.Equal(t, "configdb/06_finalize_entity_type_handle",
		string(definition.Plan.Contract[0].Definition().ID))
	assert.Equal(t, "entitydb/50_remove_legacy_subject_attribute",
		string(definition.Plan.Contract[1].Definition().ID))
	assert.Equal(t, "entitydb/10_remove_entity_state",
		string(definition.Plan.Contract[2].Definition().ID))
	expectedPrimaryKeyContractIDs := []string{
		"configdb/22_finalize_entity_types_primary_key",
		"configdb/23_finalize_role_primary_key",
		"configdb/24_finalize_theme_primary_key",
		"configdb/25_finalize_layout_primary_key",
		"configdb/26_finalize_inbound_client_primary_key",
		"configdb/27_finalize_idp_primary_key",
		"configdb/28_finalize_notification_sender_primary_key",
		"configdb/29_finalize_certificate_primary_key",
		"configdb/30_finalize_resource_server_primary_key",
		"configdb/31_finalize_resource_primary_key",
		"configdb/32_finalize_action_primary_key",
		"configdb/33_finalize_flow_primary_key",
		"configdb/34_finalize_presentation_definition_primary_key",
		"configdb/35_finalize_credential_configuration_primary_key",
		"entitydb/36_finalize_organization_unit_primary_key",
		"entitydb/37_finalize_entity_primary_key",
		"entitydb/38_finalize_group_primary_key",
	}
	for index, expectedID := range expectedPrimaryKeyContractIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Contract[index+3].Definition().ID))
	}
	expectedAddedContractIDs := []string{
		"runtime_persistent/56_finalize_session_tables",
		"configdb/88_finalize_flow_ou_id",
		"configdb/89_finalize_idp_ou_id",
		"configdb/90_finalize_notification_sender_ou_id",
		"configdb/93_finalize_role_assignment",
		"runtime_persistent/68_delete_duplicate_user_sessions",
		"configdb/69_delete_duplicate_user_assignments",
		"entitydb/70_delete_duplicate_users",
		"entitydb/71_drop_user_share_migration_table",
		"entitydb/96_drop_group_member_lookup_index",
		"configdb/115_add_agent_type_organization_unit_index",
	}
	for index, expectedID := range expectedAddedContractIDs {
		assert.Equal(t, expectedID, string(definition.Plan.Contract[index+20].Definition().ID))
	}
	expectedRollbackIDs := []string{
		"entitydb/116_convert_agent_owner_rollback",
		"entitydb/67_merge_duplicate_linked_accounts_rollback",
		"entitydb/66_rehome_original_users_rollback",
		"entitydb/47_migrate_agent_names_to_attributes_rollback",
		"configdb/46_migrate_default_agent_name_schema_rollback",
		"configdb/45_migrate_flow_entity_type_references_rollback",
		"configdb/44_migrate_idp_entity_type_references_rollback",
		"configdb/43_migrate_inbound_client_entity_type_references_rollback",
		"entitydb/42_migrate_entity_type_references_rollback",
		"entitydb/48_link_legacy_federated_subjects_rollback",
		"configdb/04_backfill_entity_type_handle_rollback",
		"entitydb/65_add_originals_to_duplicate_groups_rollback",
		"configdb/64_create_original_user_share_policies_rollback",
		"entitydb/63_snapshot_duplicate_users_rollback",
	}
	expectedRollbackForwardIDs := []string{
		"entitydb/116_convert_agent_owner",
		"entitydb/67_merge_duplicate_linked_accounts",
		"entitydb/66_rehome_original_users",
		"entitydb/47_migrate_agent_names_to_attributes",
		"configdb/46_migrate_default_agent_name_schema",
		"configdb/45_migrate_flow_entity_type_references",
		"configdb/44_migrate_idp_entity_type_references",
		"configdb/43_migrate_inbound_client_entity_type_references",
		"entitydb/42_migrate_entity_type_references",
		"entitydb/48_link_legacy_federated_subjects",
		"configdb/04_backfill_entity_type_handle",
		"entitydb/65_add_originals_to_duplicate_groups",
		"configdb/64_create_original_user_share_policies",
		"entitydb/63_snapshot_duplicate_users",
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
			assert.Equal(t, expectedVersion, store.RequiredLevel)
		}
	}
}

func TestDefinitionWithDependenciesMatchesDefaultDefinition(t *testing.T) {
	defaultDefinition, err := Definition()
	require.NoError(t, err)
	dependencyDefinition, err := DefinitionWithDependencies(operations.Dependencies{})
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
