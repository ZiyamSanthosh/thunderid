// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandRolePrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "2.0.0/configdb/24_expand_role_primary_key", Description: "Scope the ROLE primary key by deployment",
			StoreID: engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "ROLE", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_role_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-24",
		references: []primaryKeyReference{{
			table: "ROLE_PERMISSION", legacyColumns: []string{"ROLE_ID"},
			targetColumns:  []string{"DEPLOYMENT_ID", "ROLE_ID"},
			constraintName: "mig_role_permission_role_fk", onDelete: "CASCADE", onUpdate: "NO ACTION",
		}},
	})
}
