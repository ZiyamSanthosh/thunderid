// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateSecretOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateSecretOperation,
		engine.OperationID("2.0.0/configdb/14_create_secret"), secretTable, 2)
}
