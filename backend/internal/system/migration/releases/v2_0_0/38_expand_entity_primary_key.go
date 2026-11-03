// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandEntityPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "2.0.0/entitydb/38_expand_entity_primary_key", Description: "Scope the ENTITY primary key by deployment",
			StoreID: engine.StoreIDEntityDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "ENTITY", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_entity_deployment_primary_key", queryPrefix: "MIG-ENTITY-PK-38",
		references: []primaryKeyReference{{
			table: "ENTITY_IDENTIFIER", legacyColumns: []string{"ENTITY_ID"},
			targetColumns:  []string{"DEPLOYMENT_ID", "ENTITY_ID"},
			constraintName: "mig_entity_identifier_entity_fk", onDelete: "CASCADE", onUpdate: "NO ACTION",
		}},
	})
}
