// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateGatewayOperation,
		engine.OperationID("configdb/14_create_gateway"), gatewayTable, 2)
}
