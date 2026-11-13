// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateConfigurationVersionOperation(t *testing.T) {
	assertConfigTableOperation(t, NewCreateConfigurationVersionOperation,
		engine.OperationID("configdb/15_create_configuration_version"), configurationVersionTable, 2)
}
