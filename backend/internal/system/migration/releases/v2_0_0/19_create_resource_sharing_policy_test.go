// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceSharingPolicyOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceSharingPolicyOperation,
		engine.OperationID("2.0.0/configdb/19_create_resource_sharing_policy"), resourceSharingPolicyTable, 2)
}
