// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandActionPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandActionPrimaryKeyOperation,
		"2.0.0/configdb/33_expand_action_primary_key", engine.StoreIDConfigDB,
		"ACTION", `"DEPLOYMENT_ID", "ID"`)
}
