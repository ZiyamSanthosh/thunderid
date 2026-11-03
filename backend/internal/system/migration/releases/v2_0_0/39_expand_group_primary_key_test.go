// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandGroupPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandGroupPrimaryKeyOperation,
		"2.0.0/entitydb/39_expand_group_primary_key", engine.StoreIDEntityDB,
		"GROUP", `"DEPLOYMENT_ID", "ID"`)
}
