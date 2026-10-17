// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandInboundClientPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "2.0.0/configdb/27_expand_inbound_client_primary_key",
			Description: "Scope the INBOUND_CLIENT primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "INBOUND_CLIENT", legacyColumns: []string{"ENTITY_ID"},
		targetColumns: []string{"DEPLOYMENT_ID", "ENTITY_ID"},
		indexName:     "idx_inbound_client_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-27",
		references: []primaryKeyReference{{
			table: "OAUTH_INBOUND_PROFILE", legacyColumns: []string{"ENTITY_ID"},
			targetColumns:  []string{"DEPLOYMENT_ID", "ENTITY_ID"},
			constraintName: "mig_oauth_inbound_profile_client_fk", onDelete: "CASCADE", onUpdate: "NO ACTION",
		}},
	})
}
