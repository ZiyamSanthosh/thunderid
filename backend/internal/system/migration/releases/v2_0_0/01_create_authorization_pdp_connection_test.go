// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateAuthorizationPDPConnectionOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateAuthorizationPDPConnectionOperation,
		engine.OperationID("2.0.0/configdb/01_create_authorization_pdp_connection"),
		authorizationPDPConnectionTable, 2)

	operation, err := newCreateAuthorizationPDPConnectionOperation()
	require.NoError(t, err)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Contains(t, implementation.Statements[0].Query, `CREATE TABLE "AUTHORIZATION_PDP_CONNECTION"`)
	assert.Contains(t, implementation.Statements[1].Query, authorizationPDPConnectionIndex)
}
