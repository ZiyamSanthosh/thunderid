// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandEntityPrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, NewExpandEntityPrimaryKeyOperation,
		engine.OperationID("entitydb/37_expand_entity_primary_key"), engine.StoreIDEntityDB,
		"ENTITY", `"DEPLOYMENT_ID", "ID"`)
	registered, err := NewExpandEntityPrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 1)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "ENTITY_IDENTIFIER"`)
}
