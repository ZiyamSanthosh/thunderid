// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const (
	addEntityProjectIndexOperationID           engine.OperationID = "2.0.0/entitydb/79_add_entity_project_index"
	addEntityOwnerIndexOperationID             engine.OperationID = "2.0.0/entitydb/80_add_entity_owner_index"
	addOrganizationUnitProjectIndexOperationID engine.OperationID = "2.0.0/entitydb/" +
		"81_add_organization_unit_project_index"
)

// The indexes are built concurrently, so the server keeps writing to ENTITY and ORGANIZATION_UNIT meanwhile.
func newAddEntityProjectIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addEntityProjectIndexOperationID,
		description: "Add the index that lists entities by project and category",
		storeID:     engine.StoreIDEntityDB,
		table:       "ENTITY",
		name:        "idx_entity_project",
		definition:  "(deployment_id, project_id, category)",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_entity_project"
			ON "ENTITY" (DEPLOYMENT_ID, PROJECT_ID, CATEGORY)`,
	}, "MIG-OIX-ENT-PROJECT")
}

func newAddEntityOwnerIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addEntityOwnerIndexOperationID,
		description: "Add the index that looks up agents by their owner",
		storeID:     engine.StoreIDEntityDB,
		table:       "ENTITY",
		name:        "idx_entity_owner",
		definition:  "(deployment_id, ((system_attributes #>> '{owner,id}'::text[])))",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_entity_owner"
			ON "ENTITY" (DEPLOYMENT_ID, (SYSTEM_ATTRIBUTES#>>'{owner,id}'))`,
	}, "MIG-OIX-ENT-OWNER")
}

func newAddOrganizationUnitProjectIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addOrganizationUnitProjectIndexOperationID,
		description: "Add the index that lists the root organization units a project owns",
		storeID:     engine.StoreIDEntityDB,
		table:       "ORGANIZATION_UNIT",
		name:        "idx_ou_project",
		definition:  "(deployment_id, project_id)",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_ou_project"
			ON "ORGANIZATION_UNIT" (DEPLOYMENT_ID, PROJECT_ID)`,
	}, "MIG-OIX-OU-PROJECT")
}
