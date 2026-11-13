// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandResourceServerPrimaryKeyOperation returns the expand resource server primary key operation.
func NewExpandResourceServerPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "configdb/30_expand_resource_server_primary_key",
			Description: "Scope the RESOURCE_SERVER primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "RESOURCE_SERVER", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_resource_server_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-31",
		references: []primaryKeyReference{
			{table: "RESOURCE", legacyColumns: []string{"RESOURCE_SERVER_ID"},
				targetColumns:  []string{"DEPLOYMENT_ID", "RESOURCE_SERVER_ID"},
				constraintName: "mig_resource_resource_server_fk", onDelete: "RESTRICT", onUpdate: "CASCADE"},
			{table: "ACTION", legacyColumns: []string{"RESOURCE_SERVER_ID"},
				targetColumns:  []string{"DEPLOYMENT_ID", "RESOURCE_SERVER_ID"},
				constraintName: "mig_action_resource_server_fk", onDelete: "RESTRICT", onUpdate: "CASCADE"},
		},
	})
}
