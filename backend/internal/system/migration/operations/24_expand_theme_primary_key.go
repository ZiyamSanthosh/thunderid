// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandThemePrimaryKeyOperation returns the expand theme primary key operation.
func NewExpandThemePrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "configdb/24_expand_theme_primary_key", Description: "Scope the THEME primary key by deployment",
			StoreID: engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "THEME", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_theme_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-25",
		// The new schema defines no foreign key from INBOUND_CLIENT.THEME_ID, so the legacy one is dropped.
		droppedReferences: []droppedPrimaryKeyReference{{table: "INBOUND_CLIENT", columns: []string{"THEME_ID"}}},
	})
}
