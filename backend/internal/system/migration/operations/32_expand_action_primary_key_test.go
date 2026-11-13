// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandActionPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandActionPrimaryKeyOperation,
		"configdb/32_expand_action_primary_key", engine.StoreIDConfigDB,
		"ACTION", `"DEPLOYMENT_ID", "ID"`)
}
