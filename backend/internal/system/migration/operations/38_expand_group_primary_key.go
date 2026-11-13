// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandGroupPrimaryKeyOperation returns the expand group primary key operation.
func NewExpandGroupPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "entitydb/38_expand_group_primary_key", Description: "Scope the GROUP primary key by deployment",
			StoreID: engine.StoreIDEntityDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "GROUP", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_group_deployment_primary_key", queryPrefix: "MIG-ENTITY-PK-39",
	})
}
