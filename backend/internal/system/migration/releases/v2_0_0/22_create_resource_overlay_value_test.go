// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceOverlayValueOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceOverlayValueOperation,
		engine.OperationID("2.0.0/configdb/22_create_resource_overlay_value"), resourceOverlayValueTable, 1)
}
