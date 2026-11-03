// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateVariableOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateVariableOperation,
		engine.OperationID("2.0.0/configdb/13_create_variable"), variableTable, 2)
}
