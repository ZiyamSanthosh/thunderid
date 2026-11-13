// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandNotificationSenderPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandNotificationSenderPrimaryKeyOperation,
		"configdb/28_expand_notification_sender_primary_key", engine.StoreIDConfigDB,
		"NOTIFICATION_SENDER", `"DEPLOYMENT_ID", "ID"`)
}
