// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandActionPrimaryKeyOperation returns the expand action primary key operation.
func NewExpandActionPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "configdb/32_expand_action_primary_key",
			Description: "Scope the ACTION primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "ACTION", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_action_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-33",
	})
}
