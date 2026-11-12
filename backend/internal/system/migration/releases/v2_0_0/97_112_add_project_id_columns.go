// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// projectScopedTable is a table whose rows now belong to a project. The column is nullable with no default, so
// existing rows belong to no project and need no data migration, and the previous release, which never names the
// column, keeps working.
type projectScopedTable struct {
	storeID   engine.StoreID
	table     string
	slug      string
	indexName string
	columnID  engine.OperationID
	indexID   engine.OperationID
}

var projectScopedTables = []projectScopedTable{
	{engine.StoreIDConfigDB, "ROLE", "role", "idx_role_project",
		"2.0.0/configdb/97_add_role_project_id", "2.0.0/configdb/105_add_role_project_index"},
	{engine.StoreIDConfigDB, "THEME", "theme", "idx_theme_project",
		"2.0.0/configdb/98_add_theme_project_id", "2.0.0/configdb/106_add_theme_project_index"},
	{engine.StoreIDConfigDB, "LAYOUT", "layout", "idx_layout_project",
		"2.0.0/configdb/99_add_layout_project_id", "2.0.0/configdb/107_add_layout_project_index"},
	{engine.StoreIDConfigDB, "RESOURCE_SERVER", "resource_server", "idx_resource_server_project",
		"2.0.0/configdb/100_add_resource_server_project_id", "2.0.0/configdb/108_add_resource_server_project_index"},
	{engine.StoreIDConfigDB, "FLOW", "flow", "idx_flow_project",
		"2.0.0/configdb/101_add_flow_project_id", "2.0.0/configdb/109_add_flow_project_index"},
	{engine.StoreIDConfigDB, "PRESENTATION_DEFINITION", "presentation_definition", "idx_openid4vp_pd_project",
		"2.0.0/configdb/102_add_presentation_definition_project_id",
		"2.0.0/configdb/110_add_presentation_definition_project_index"},
	{engine.StoreIDConfigDB, "CREDENTIAL_CONFIGURATION", "credential_configuration", "idx_openid4vci_cc_project",
		"2.0.0/configdb/103_add_credential_configuration_project_id",
		"2.0.0/configdb/111_add_credential_configuration_project_index"},
	{engine.StoreIDEntityDB, "GROUP", "group", "idx_group_project",
		"2.0.0/entitydb/104_add_group_project_id", "2.0.0/entitydb/112_add_group_project_index"},
}

func newAddProjectIDColumnOperation(scoped projectScopedTable) (engine.Operation, error) {
	return newAddColumnOperation(addColumnSpec{
		id:          scoped.columnID,
		description: fmt.Sprintf("Add the project ID to %s rows", scoped.slug),
		storeID:     scoped.storeID,
		table:       scoped.table,
		columns:     []columnSpec{{"project_id", "YES", "character varying", "36", "none"}},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-project-id", Query: fmt.Sprintf(
				`ALTER TABLE %q ADD COLUMN PROJECT_ID VARCHAR(36)`, scoped.table)},
		},
	})
}

// newAddProjectIndexOperation builds the index that lists a project's rows. It is built concurrently, so the
// server keeps writing to the table meanwhile.
func newAddProjectIndexOperation(scoped projectScopedTable) engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          scoped.indexID,
		description: fmt.Sprintf("Add the index that lists the %s rows of a project", scoped.slug),
		storeID:     scoped.storeID,
		table:       scoped.table,
		name:        scoped.indexName,
		definition:  "(deployment_id, project_id)",
		createSQL: fmt.Sprintf(`CREATE INDEX CONCURRENTLY %q ON %q (DEPLOYMENT_ID, PROJECT_ID)`,
			scoped.indexName, scoped.table),
	}, "MIG-OIX-PRJ-"+scoped.slug)
}
