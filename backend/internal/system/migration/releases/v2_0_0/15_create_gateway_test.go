// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateGatewayOperation,
		engine.OperationID("2.0.0/configdb/15_create_gateway"), gatewayTable, 2)
}
