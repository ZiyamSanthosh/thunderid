// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandOrganizationUnitPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandOrganizationUnitPrimaryKeyOperation,
		"entitydb/36_expand_organization_unit_primary_key", engine.StoreIDEntityDB,
		"ORGANIZATION_UNIT", `"DEPLOYMENT_ID", "OU_ID"`)
}
