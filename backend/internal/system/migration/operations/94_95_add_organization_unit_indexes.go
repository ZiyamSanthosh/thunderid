// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const (
	addCredentialConfigurationOUIndexOperationID engine.OperationID = "configdb/" +
		"94_add_credential_configuration_ou_index"
	addPresentationDefinitionOUIndexOperationID engine.OperationID = "configdb/" +
		"95_add_presentation_definition_ou_index"
)

// The indexes list the credential configurations and presentation definitions that an organization unit owns.
// They are built concurrently, so the server keeps writing to both tables meanwhile.
func NewAddCredentialConfigurationOUIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addCredentialConfigurationOUIndexOperationID,
		description: "Add the index that lists credential configurations by organization unit",
		storeID:     engine.StoreIDConfigDB,
		table:       "CREDENTIAL_CONFIGURATION",
		name:        "idx_openid4vci_cc_ou",
		definition:  "(deployment_id, ou_id)",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_openid4vci_cc_ou"
			ON "CREDENTIAL_CONFIGURATION" (DEPLOYMENT_ID, OU_ID)`,
	}, "MIG-OIX-CC-OU")
}

// NewAddPresentationDefinitionOUIndexOperation returns the add presentation definition ou index operation.
func NewAddPresentationDefinitionOUIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addPresentationDefinitionOUIndexOperationID,
		description: "Add the index that lists presentation definitions by organization unit",
		storeID:     engine.StoreIDConfigDB,
		table:       "PRESENTATION_DEFINITION",
		name:        "idx_openid4vp_pd_ou",
		definition:  "(deployment_id, ou_id)",
		createSQL: `CREATE INDEX CONCURRENTLY "idx_openid4vp_pd_ou"
			ON "PRESENTATION_DEFINITION" (DEPLOYMENT_ID, OU_ID)`,
	}, "MIG-OIX-PD-OU")
}
