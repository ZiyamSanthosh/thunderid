// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandIDPPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandIDPPrimaryKeyOperation,
		"2.0.0/configdb/28_expand_idp_primary_key", engine.StoreIDConfigDB,
		"IDP", `"DEPLOYMENT_ID", "ID"`)
}
