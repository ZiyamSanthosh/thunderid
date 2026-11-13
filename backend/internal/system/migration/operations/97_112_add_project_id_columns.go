// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

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

// ProjectScopedTables lists the tables that gain a project column.
var ProjectScopedTables = []projectScopedTable{
	{engine.StoreIDConfigDB, "ROLE", "role", "idx_role_project",
		"configdb/97_add_role_project_id", "configdb/105_add_role_project_index"},
	{engine.StoreIDConfigDB, "THEME", "theme", "idx_theme_project",
		"configdb/98_add_theme_project_id", "configdb/106_add_theme_project_index"},
	{engine.StoreIDConfigDB, "LAYOUT", "layout", "idx_layout_project",
		"configdb/99_add_layout_project_id", "configdb/107_add_layout_project_index"},
	{engine.StoreIDConfigDB, "RESOURCE_SERVER", "resource_server", "idx_resource_server_project",
		"configdb/100_add_resource_server_project_id", "configdb/108_add_resource_server_project_index"},
	{engine.StoreIDConfigDB, "FLOW", "flow", "idx_flow_project",
		"configdb/101_add_flow_project_id", "configdb/109_add_flow_project_index"},
	{engine.StoreIDConfigDB, "PRESENTATION_DEFINITION", "presentation_definition", "idx_openid4vp_pd_project",
		"configdb/102_add_presentation_definition_project_id",
		"configdb/110_add_presentation_definition_project_index"},
	{engine.StoreIDConfigDB, "CREDENTIAL_CONFIGURATION", "credential_configuration", "idx_openid4vci_cc_project",
		"configdb/103_add_credential_configuration_project_id",
		"configdb/111_add_credential_configuration_project_index"},
	{engine.StoreIDEntityDB, "GROUP", "group", "idx_group_project",
		"entitydb/104_add_group_project_id", "entitydb/112_add_group_project_index"},
}

// NewAddProjectIDColumnOperation returns the add project id column operation.
func NewAddProjectIDColumnOperation(scoped projectScopedTable) (engine.Operation, error) {
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

// NewAddProjectIndexOperation builds the index that lists a project's rows. It is built concurrently, so the
// server keeps writing to the table meanwhile.
func NewAddProjectIndexOperation(scoped projectScopedTable) engine.Operation {
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
