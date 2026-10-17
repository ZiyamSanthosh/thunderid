// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandPresentationDefinitionPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "2.0.0/configdb/35_expand_presentation_definition_primary_key",
			Description: "Scope the PRESENTATION_DEFINITION primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "PRESENTATION_DEFINITION", legacyColumns: []string{"ID"},
		targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName:     "idx_presentation_definition_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-35",
	})
}
