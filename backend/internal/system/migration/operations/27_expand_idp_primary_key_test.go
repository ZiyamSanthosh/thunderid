// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandIDPPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandIDPPrimaryKeyOperation,
		"configdb/27_expand_idp_primary_key", engine.StoreIDConfigDB,
		"IDP", `"DEPLOYMENT_ID", "ID"`)
}
