// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandEntityTypesPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/23_expand_entity_types_primary_key",
			Description:      "Scope the ENTITY_TYPES primary key by deployment",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table:         "ENTITY_TYPES",
		legacyColumns: []string{"ID"},
		targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName:     "idx_entity_types_deployment_primary_key",
		queryPrefix:   "MIG-CONFIG-PK-23",
	})
}
