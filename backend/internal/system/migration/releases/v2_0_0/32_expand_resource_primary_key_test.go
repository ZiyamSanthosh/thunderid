// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandResourcePrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandResourcePrimaryKeyOperation,
		engine.OperationID("2.0.0/configdb/32_expand_resource_primary_key"), engine.StoreIDConfigDB,
		"RESOURCE", `"DEPLOYMENT_ID", "ID"`)
	registered, err := newExpandResourcePrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 2)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "RESOURCE"`)
	assert.Contains(t, operation.referenceQueries[1].add.PostgresQuery, `ALTER TABLE "ACTION"`)
}
