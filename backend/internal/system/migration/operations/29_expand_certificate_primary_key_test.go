// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandCertificatePrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandCertificatePrimaryKeyOperation,
		"configdb/29_expand_certificate_primary_key", engine.StoreIDConfigDB,
		"CERTIFICATE", `"DEPLOYMENT_ID", "ID"`)
}
