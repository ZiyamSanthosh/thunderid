// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandPresentationDefinitionPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandPresentationDefinitionPrimaryKeyOperation,
		"configdb/34_expand_presentation_definition_primary_key", engine.StoreIDConfigDB,
		"PRESENTATION_DEFINITION", `"DEPLOYMENT_ID", "ID"`)
}
