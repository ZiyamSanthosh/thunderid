// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandNotificationSenderPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandNotificationSenderPrimaryKeyOperation,
		"2.0.0/configdb/29_expand_notification_sender_primary_key", engine.StoreIDConfigDB,
		"NOTIFICATION_SENDER", `"DEPLOYMENT_ID", "ID"`)
}
