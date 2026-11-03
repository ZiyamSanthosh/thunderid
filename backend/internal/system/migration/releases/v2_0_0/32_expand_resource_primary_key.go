// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandResourcePrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "2.0.0/configdb/32_expand_resource_primary_key",
			Description: "Scope the RESOURCE primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "RESOURCE", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_resource_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-32",
		references: []primaryKeyReference{
			{table: "RESOURCE", legacyColumns: []string{"PARENT_RESOURCE_ID"},
				targetColumns:  []string{"DEPLOYMENT_ID", "PARENT_RESOURCE_ID"},
				constraintName: "mig_resource_parent_fk", onDelete: "RESTRICT", onUpdate: "CASCADE"},
			{table: "ACTION", legacyColumns: []string{"RESOURCE_ID"},
				targetColumns:  []string{"DEPLOYMENT_ID", "RESOURCE_ID"},
				constraintName: "mig_action_resource_fk", onDelete: "RESTRICT", onUpdate: "CASCADE"},
		},
	})
}
