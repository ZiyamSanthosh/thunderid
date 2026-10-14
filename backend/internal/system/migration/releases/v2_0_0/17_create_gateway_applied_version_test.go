// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayAppliedVersionOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateGatewayAppliedVersionOperation,
		engine.OperationID("2.0.0/configdb/17_create_gateway_applied_version"), gatewayAppliedVersionTable, 1)
}
