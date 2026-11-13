// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

// NewExpandCredentialConfigurationPrimaryKeyOperation returns the operation of that name.
func NewExpandCredentialConfigurationPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "configdb/35_expand_credential_configuration_primary_key",
			Description: "Scope the CREDENTIAL_CONFIGURATION primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "CREDENTIAL_CONFIGURATION", legacyColumns: []string{"ID"},
		targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName:     "idx_credential_configuration_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-36",
	})
}
