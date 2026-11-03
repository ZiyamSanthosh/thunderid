// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandThemePrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandThemePrimaryKeyOperation,
		"2.0.0/configdb/25_expand_theme_primary_key", engine.StoreIDConfigDB,
		"THEME", `"DEPLOYMENT_ID", "ID"`)
}
