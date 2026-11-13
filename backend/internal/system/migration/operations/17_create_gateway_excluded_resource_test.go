// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateGatewayExcludedResourceOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateGatewayExcludedResourceOperation,
		engine.OperationID("configdb/17_create_gateway_excluded_resource"), gatewayExcludedResourceTable, 1)
}
