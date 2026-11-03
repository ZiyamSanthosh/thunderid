// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type recordingExecutionAccess struct {
	primaryKeyReadAccess
	executed []string
}

func (a *recordingExecutionAccess) Execute(
	_ context.Context,
	_ engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) (int64, error) {
	a.executed = append(a.executed, query.PostgresQuery)
	return 0, nil
}

func TestExpandThemeAndLayoutPrimaryKeysDropInboundClientForeignKeys(t *testing.T) {
	tests := []struct {
		name        string
		constructor func() (engine.Operation, error)
		column      string
		legacy      string
	}{
		{"theme", newExpandThemePrimaryKeyOperation, "THEME_ID", "inbound_client_theme_id_fkey"},
		{"layout", newExpandLayoutPrimaryKeyOperation, "LAYOUT_ID", "inbound_client_layout_id_fkey"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registered, err := test.constructor()
			require.NoError(t, err)
			operation := registered.(*onlinePrimaryKeyOperation)
			require.Len(t, operation.config.droppedReferences, 1)

			access := &recordingExecutionAccess{primaryKeyReadAccess: primaryKeyReadAccess{
				storeID: engine.StoreIDConfigDB,
				rowsByQueryID: map[string][]engine.QueryRow{
					operation.droppedReferenceQuery.GetID(): {
						{"constraint_name": test.legacy, "child_columns": test.column, "parent_columns": "ID"},
						// Unrelated foreign key from the same table must be left alone.
						{"constraint_name": "other_fkey", "child_columns": "OTHER_ID", "parent_columns": "ID"},
					},
				},
			}}
			require.NoError(t, operation.dropLegacyReferences(context.Background(), access))
			require.Len(t, access.executed, 1)
			assert.Equal(t, `ALTER TABLE "INBOUND_CLIENT" DROP CONSTRAINT "`+test.legacy+`"`, access.executed[0])
		})
	}
}

func TestDropLegacyReferencesIsNoOpWhenForeignKeyAlreadyGone(t *testing.T) {
	registered, err := newExpandThemePrimaryKeyOperation()
	require.NoError(t, err)
	operation := registered.(*onlinePrimaryKeyOperation)
	access := &recordingExecutionAccess{primaryKeyReadAccess: primaryKeyReadAccess{
		storeID:       engine.StoreIDConfigDB,
		rowsByQueryID: map[string][]engine.QueryRow{operation.droppedReferenceQuery.GetID(): {}},
	}}
	require.NoError(t, operation.dropLegacyReferences(context.Background(), access))
	assert.Empty(t, access.executed)
}
