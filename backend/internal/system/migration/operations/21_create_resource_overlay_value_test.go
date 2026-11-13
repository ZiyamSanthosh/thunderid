// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceOverlayValueOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateResourceOverlayValueOperation,
		engine.OperationID("configdb/21_create_resource_overlay_value"), resourceOverlayValueTable, 1)
}
