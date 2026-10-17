// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandInboundClientPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandInboundClientPrimaryKeyOperation,
		engine.OperationID("2.0.0/configdb/27_expand_inbound_client_primary_key"), engine.StoreIDConfigDB,
		"INBOUND_CLIENT", `"DEPLOYMENT_ID", "ENTITY_ID"`)
	registered, err := newExpandInboundClientPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 1)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "OAUTH_INBOUND_PROFILE"`)
}
