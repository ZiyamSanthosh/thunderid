// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func newExpandCertificatePrimaryKeyOperation() (engine.Operation, error) {
	return newOnlinePrimaryKeyOperation(onlinePrimaryKeyConfig{
		definition: engine.OperationDefinition{
			ID:          "2.0.0/configdb/30_expand_certificate_primary_key",
			Description: "Scope the CERTIFICATE primary key by deployment",
			StoreID:     engine.StoreIDConfigDB, Kind: engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		table: "CERTIFICATE", legacyColumns: []string{"ID"}, targetColumns: []string{"DEPLOYMENT_ID", "ID"},
		indexName: "idx_certificate_deployment_primary_key", queryPrefix: "MIG-CONFIG-PK-30",
	})
}
