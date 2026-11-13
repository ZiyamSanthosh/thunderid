// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandLayoutPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandLayoutPrimaryKeyOperation,
		"configdb/25_expand_layout_primary_key", engine.StoreIDConfigDB,
		"LAYOUT", `"DEPLOYMENT_ID", "ID"`)
}
