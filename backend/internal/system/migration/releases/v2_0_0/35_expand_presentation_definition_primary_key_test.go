// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandPresentationDefinitionPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandPresentationDefinitionPrimaryKeyOperation,
		"2.0.0/configdb/35_expand_presentation_definition_primary_key", engine.StoreIDConfigDB,
		"PRESENTATION_DEFINITION", `"DEPLOYMENT_ID", "ID"`)
}
