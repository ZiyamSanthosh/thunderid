// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandOrganizationUnitPrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "2.0.0/entitydb/37_expand_organization_unit_primary_key",
			Description: "Scope the ORGANIZATION_UNIT primary key by deployment",
			StoreID:     engine.StoreIDEntityDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "ORGANIZATION_UNIT", legacyColumns: []string{"OU_ID"},
		targetColumns: []string{"DEPLOYMENT_ID", "OU_ID"},
		indexName:     "idx_organization_unit_deployment_primary_key", queryPrefix: "MIG-ENTITY-PK-37",
	})
}
