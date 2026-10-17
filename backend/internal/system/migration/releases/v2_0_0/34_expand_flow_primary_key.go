// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandFlowPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "2.0.0/configdb/34_expand_flow_primary_key", Description: "Scope the FLOW primary key by deployment",
			StoreID: engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "FLOW", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_flow_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-34",
		references: []primaryKeyReference{{
			table: "FLOW_VERSION", legacyColumns: []string{"FLOW_ID"},
			targetColumns:  []string{"DEPLOYMENT_ID", "FLOW_ID"},
			constraintName: "mig_flow_version_flow_fk", onDelete: "CASCADE", onUpdate: "NO ACTION",
		}},
	})
}
