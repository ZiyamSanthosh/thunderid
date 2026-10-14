// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayExcludedResourceOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateGatewayExcludedResourceOperation,
		engine.OperationID("2.0.0/configdb/18_create_gateway_excluded_resource"), gatewayExcludedResourceTable, 1)
}
