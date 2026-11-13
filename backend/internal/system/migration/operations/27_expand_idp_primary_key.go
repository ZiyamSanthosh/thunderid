// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandIDPPrimaryKeyOperation returns the expand idp primary key operation.
func NewExpandIDPPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID: "configdb/27_expand_idp_primary_key", Description: "Scope the IDP primary key by deployment",
			StoreID: engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "IDP", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_idp_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-28",
	})
}
