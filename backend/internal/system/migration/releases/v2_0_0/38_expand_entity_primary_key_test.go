// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandEntityPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandEntityPrimaryKeyOperation,
		engine.OperationID("2.0.0/entitydb/38_expand_entity_primary_key"), engine.StoreIDEntityDB,
		"ENTITY", `"DEPLOYMENT_ID", "ID"`)
	registered, err := newExpandEntityPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 1)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "ENTITY_IDENTIFIER"`)
}
