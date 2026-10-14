// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceSharingPolicyUnitOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceSharingPolicyUnitOperation,
		engine.OperationID("2.0.0/configdb/20_create_resource_sharing_policy_unit"), resourceSharingPolicyUnitTable, 2)
}
