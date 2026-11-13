// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateVariableOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateVariableOperation,
		engine.OperationID("configdb/12_create_variable"), variableTable, 2)
}
