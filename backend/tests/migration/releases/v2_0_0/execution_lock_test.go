// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const postgresMigrationExecutionLockKey int64 = 6073191990351123017

func verifyConcurrentMigrationRejected(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("concurrent_migration_rejected", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer func() {
			require.NoError(t, conn.Close())
		}()

		var acquired bool
		require.NoError(t, conn.QueryRowContext(context.Background(),
			"SELECT pg_try_advisory_lock($1)", postgresMigrationExecutionLockKey).Scan(&acquired))
		require.True(t, acquired)
		defer func() {
			var released bool
			require.NoError(t, conn.QueryRowContext(context.Background(),
				"SELECT pg_advisory_unlock($1)", postgresMigrationExecutionLockKey).Scan(&released))
			require.True(t, released)
		}()

		output := environment.RunMigrationCommandExpectFailure(t, "apply")
		assert.Contains(t, output, "database namespace is owned by another migration runner")
		t.Log("verified: a second migration runner is rejected without locking application tables")
	})
}
