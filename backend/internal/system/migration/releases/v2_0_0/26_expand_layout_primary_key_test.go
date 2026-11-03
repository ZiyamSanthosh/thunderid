// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandLayoutPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandLayoutPrimaryKeyOperation,
		"2.0.0/configdb/26_expand_layout_primary_key", engine.StoreIDConfigDB,
		"LAYOUT", `"DEPLOYMENT_ID", "ID"`)
}
