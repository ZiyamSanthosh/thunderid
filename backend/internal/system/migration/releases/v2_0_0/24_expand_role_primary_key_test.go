// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestExpandRolePrimaryKeyDefinition(t *testing.T) {
	assertOnlinePrimaryKeyDefinition(t, newExpandRolePrimaryKeyOperation,
		engine.OperationID("2.0.0/configdb/24_expand_role_primary_key"), engine.StoreIDConfigDB,
		"ROLE", `"DEPLOYMENT_ID", "ID"`)
	registered, err := newExpandRolePrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	require.Len(t, operation.referenceQueries, 1)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery, `ALTER TABLE "ROLE_PERMISSION"`)
	assert.Contains(t, operation.referenceQueries[0].add.PostgresQuery,
		`FOREIGN KEY ("deployment_id", "role_id")`)
}

func TestExpandRolePrimaryKeyVerificationStates(t *testing.T) {
	registered, err := newExpandRolePrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)

	legacyReference := engine.QueryRow{
		"constraint_name": "role_permission_role_id_fkey", "is_valid": true,
		"child_columns": "ROLE_ID", "parent_columns": "ID",
		"delete_action": "c", "update_action": "a",
	}
	targetReference := engine.QueryRow{
		"constraint_name": "mig_role_permission_role_fk", "is_valid": true,
		"child_columns": "DEPLOYMENT_ID,ROLE_ID", "parent_columns": "DEPLOYMENT_ID,ID",
		"delete_action": "c", "update_action": "a",
	}
	validIndex := []engine.QueryRow{{
		"is_unique": true, "is_valid": true, "is_ready": true,
		"column_names": "DEPLOYMENT_ID,ID",
	}}
	tests := []struct {
		name       string
		primaryKey string
		indexRows  []engine.QueryRow
		foreignKey []engine.QueryRow
		expected   engine.VerificationState
	}{
		{name: "source", primaryKey: "ID", foreignKey: []engine.QueryRow{legacyReference},
			expected: engine.VerificationSource},
		{name: "prepared source", primaryKey: "ID", indexRows: validIndex,
			foreignKey: []engine.QueryRow{legacyReference, targetReference}, expected: engine.VerificationSource},
		{name: "target", primaryKey: "DEPLOYMENT_ID,ID", foreignKey: []engine.QueryRow{targetReference},
			expected: engine.VerificationTarget},
		{name: "source missing legacy foreign key", primaryKey: "ID",
			foreignKey: []engine.QueryRow{targetReference}, expected: engine.VerificationInvalid},
		{name: "target retains legacy foreign key", primaryKey: "DEPLOYMENT_ID,ID",
			foreignKey: []engine.QueryRow{legacyReference, targetReference}, expected: engine.VerificationInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := rolePrimaryKeyAccess(test.primaryKey, test.indexRows, test.foreignKey)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
		})
	}
}

func rolePrimaryKeyAccess(
	primaryKeyColumns string,
	indexRows []engine.QueryRow,
	foreignKeyRows []engine.QueryRow,
) primaryKeyReadAccess {
	var legacyIndexRows []engine.QueryRow
	if primaryKeyColumns == "DEPLOYMENT_ID,ID" {
		legacyIndexRows = []engine.QueryRow{{
			"is_unique": true, "is_valid": true, "is_ready": true, "column_names": "ID",
		}}
	}
	return primaryKeyReadAccess{
		storeID: engine.StoreIDConfigDB,
		rowsByQueryID: map[string][]engine.QueryRow{
			"MIG-CONFIG-PK-24-01": {{"relation_kind": "r"}},
			"MIG-CONFIG-PK-24-02": {
				{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO"},
				{"column_name": "ID", "is_nullable": "NO"},
			},
			"MIG-CONFIG-PK-24-03": {{
				"constraint_name": "ROLE_pkey", "column_names": primaryKeyColumns,
			}},
			"MIG-CONFIG-PK-24-04":     indexRows,
			"MIG-CONFIG-PK-24-09":     legacyIndexRows,
			"MIG-CONFIG-PK-24-R01-01": foreignKeyRows,
		},
	}
}
