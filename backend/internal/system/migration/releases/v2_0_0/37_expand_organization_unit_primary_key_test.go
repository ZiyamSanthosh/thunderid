// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandOrganizationUnitPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandOrganizationUnitPrimaryKeyOperation,
		"2.0.0/entitydb/37_expand_organization_unit_primary_key", engine.StoreIDEntityDB,
		"ORGANIZATION_UNIT", `"DEPLOYMENT_ID", "OU_ID"`)
}
