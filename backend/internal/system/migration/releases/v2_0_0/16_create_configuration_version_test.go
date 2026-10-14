// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateConfigurationVersionOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateConfigurationVersionOperation,
		engine.OperationID("2.0.0/configdb/16_create_configuration_version"), configurationVersionTable, 1)
}
