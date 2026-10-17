// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandResourceServerPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandResourceServerPrimaryKeyOperation,
		engine.OperationID("2.0.0/configdb/31_expand_resource_server_primary_key"), engine.StoreIDConfigDB,
		"RESOURCE_SERVER", `"DEPLOYMENT_ID", "ID"`)
	registered, err := newExpandResourceServerPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 2)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "RESOURCE"`)
	assert.Contains(t, operation.referenceQueries[1].add.PostgresQuery, `ALTER TABLE "ACTION"`)
}
