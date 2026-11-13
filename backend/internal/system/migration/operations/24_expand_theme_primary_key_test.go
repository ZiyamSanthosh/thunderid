// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandThemePrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandThemePrimaryKeyOperation,
		"configdb/24_expand_theme_primary_key", engine.StoreIDConfigDB,
		"THEME", `"DEPLOYMENT_ID", "ID"`)
}
