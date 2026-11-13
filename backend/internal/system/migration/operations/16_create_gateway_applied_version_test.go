// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayAppliedVersionOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateGatewayAppliedVersionOperation,
		engine.OperationID("configdb/16_create_gateway_applied_version"), gatewayAppliedVersionTable, 1)
}
