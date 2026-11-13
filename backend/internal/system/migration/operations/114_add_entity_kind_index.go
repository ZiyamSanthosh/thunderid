// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const addEntityKindIndexOperationID engine.OperationID = "entitydb/114_add_entity_kind_index"

// The index is built concurrently, so the server keeps writing to ENTITY meanwhile.
func NewAddEntityKindIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addEntityKindIndexOperationID,
		description: "Add the index that looks up agents by their kind",
		storeID:     engine.StoreIDEntityDB,
		table:       "ENTITY",
		name:        "idx_entity_kind",
		definition:  "(deployment_id, ((system_attributes ->> 'kind'::text)))",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_entity_kind"
			ON "ENTITY" (DEPLOYMENT_ID, (SYSTEM_ATTRIBUTES->>'kind'))`,
	}, "MIG-OIX-ENT-KIND")
}
