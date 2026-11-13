// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandLayoutPrimaryKeyOperation returns the expand layout primary key operation.
func NewExpandLayoutPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "configdb/25_expand_layout_primary_key",
			Description: "Scope the LAYOUT primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "LAYOUT", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_layout_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-26",
		// The new schema defines no foreign key from INBOUND_CLIENT.LAYOUT_ID, so the legacy one is dropped.
		droppedReferences: []droppedPrimaryKeyReference{{table: "INBOUND_CLIENT", columns: []string{"LAYOUT_ID"}}},
	})
}
