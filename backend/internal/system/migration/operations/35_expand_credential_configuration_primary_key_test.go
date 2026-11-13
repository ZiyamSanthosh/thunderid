// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandCredentialConfigurationPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandCredentialConfigurationPrimaryKeyOperation,
		"configdb/35_expand_credential_configuration_primary_key", engine.StoreIDConfigDB,
		"CREDENTIAL_CONFIGURATION", `"DEPLOYMENT_ID", "ID"`)
}
