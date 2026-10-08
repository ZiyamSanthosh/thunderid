// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestPostgresMetadataSchemaIsConsistentAcrossStores(t *testing.T) {
	scripts := []string{
		"configdb/postgres.sql",
		"entitydb/postgres.sql",
		"runtime_transient/postgres.sql",
		"runtime_persistent/postgres.sql",
	}

	expectedSchema := strings.TrimSpace(postgresMetadataSchema)
	for _, script := range scripts {
		t.Run(script, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join("../../../../dbscripts", script))
			require.NoError(t, err)

			schema := extractMetadataSchema(t, string(contents))
			assert.Contains(t, schema, `CREATE TABLE IF NOT EXISTS "MIGRATION_STATE"`)
			assert.Contains(t, schema, `CREATE TABLE IF NOT EXISTS "MIGRATION_PROGRESS"`)
			assert.Contains(t, schema, `CREATE TABLE IF NOT EXISTS "MIGRATION_DEPLOYMENT_STATE"`)
			assert.Contains(t, schema, `CREATE TABLE IF NOT EXISTS "MIGRATION_RUN_TARGET"`)

			assert.Equal(t, expectedSchema, strings.TrimSpace(schema))
		})
	}
}

func TestMetadataStoreInspectSchema(t *testing.T) {
	tests := []struct {
		name       string
		rows       []map[string]interface{}
		expected   SchemaStatus
		errorMatch string
	}{
		{name: "absent", expected: SchemaAbsent},
		{
			name: "ready",
			rows: []map[string]interface{}{
				{"table_name": metadataStateTable},
				{"table_name": []byte(metadataProgressTable)},
				{"table_name": metadataDeploymentStateTable},
				{"table_name": metadataRunTargetTable},
			},
			expected: SchemaReady,
		},
		{
			name:     "partial",
			rows:     []map[string]interface{}{{"table_name": metadataStateTable}},
			expected: SchemaPartial,
		},
		{
			name:       "malformed result",
			rows:       []map[string]interface{}{{}},
			errorMatch: "table_name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbClient := providermock.NewDBClientInterfaceMock(t)
			dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
				Return(test.rows, nil).Once()

			status, err := newTestMetadataStore(t, dbClient).InspectSchema(context.Background())

			if test.errorMatch == "" {
				require.NoError(t, err)
				assert.Equal(t, test.expected, status)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func TestMetadataStoreSchemaErrors(t *testing.T) {
	t.Run("inspection failure", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("QueryContext", mock.Anything, queryInspectMetadataSchema).
			Return(nil, errors.New("inspection failed")).Once()

		_, err := newTestMetadataStore(t, dbClient).InspectSchema(context.Background())

		require.Error(t, err)
		assert.ErrorContains(t, err, "inspection failed")
	})

	t.Run("creation failure", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("ExecuteContext", mock.Anything, queryCreateMetadataSchema).
			Return(int64(0), errors.New("creation failed")).Once()

		err := newTestMetadataStore(t, dbClient).CreateSchema(context.Background())

		require.Error(t, err)
		assert.ErrorContains(t, err, "creation failed")
	})

	t.Run("creates schema", func(t *testing.T) {
		dbClient := providermock.NewDBClientInterfaceMock(t)
		dbClient.On("ExecuteContext", mock.Anything, queryCreateMetadataSchema).
			Return(int64(0), nil).Once()

		err := newTestMetadataStore(t, dbClient).CreateSchema(context.Background())

		require.NoError(t, err)
	})
}

func extractMetadataSchema(t *testing.T, script string) string {
	t.Helper()

	start := strings.Index(script, `CREATE TABLE IF NOT EXISTS "MIGRATION_STATE"`)
	require.NotEqual(t, -1, start)

	endMarker := "    PRIMARY KEY (DEPLOYMENT_ID, RUN_ID)\n);"
	end := strings.Index(script[start:], endMarker)
	require.NotEqual(t, -1, end)

	return script[start : start+end+len(endMarker)]
}
