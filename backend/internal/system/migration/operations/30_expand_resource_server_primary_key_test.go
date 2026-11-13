// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandResourceServerPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandResourceServerPrimaryKeyOperation,
		engine.OperationID("configdb/30_expand_resource_server_primary_key"), engine.StoreIDConfigDB,
		"RESOURCE_SERVER", `"DEPLOYMENT_ID", "ID"`)
	registered, err := NewExpandResourceServerPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 2)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "RESOURCE"`)
	assert.Contains(t, operation.referenceQueries[1].add.PostgresQuery, `ALTER TABLE "ACTION"`)
}
