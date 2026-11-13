// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateSecretOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateSecretOperation,
		engine.OperationID("configdb/13_create_secret"), secretTable, 2)
}
