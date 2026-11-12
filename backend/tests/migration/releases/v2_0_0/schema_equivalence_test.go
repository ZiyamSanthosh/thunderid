// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

var schemaStores = []string{"configdb", "entitydb", "runtime_transient", "runtime_persistent"}

// The signature leaves out constraint names and the names of indexes that back a constraint. The expand
// operations name what they add with a temporary prefix, and a migrated database has to behave as a fresh one,
// not to carry the same identifier text.
const schemaSignatureQuery = `
SELECT 'COLUMN ' || table_name || '.' || column_name || ' ' || data_type ||
	COALESCE('(' || character_maximum_length || ')', '') || ' nullable=' || is_nullable ||
	' default=' || COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = CURRENT_SCHEMA() AND table_name NOT LIKE 'MIGRATION\_%'
UNION ALL
SELECT 'INDEX ' || t.relname || ' ' || pg_get_indexdef(i.indexrelid)
FROM pg_index i INNER JOIN pg_class t ON t.oid = i.indrelid
WHERE t.relnamespace = CURRENT_SCHEMA()::regnamespace AND t.relname NOT LIKE 'MIGRATION\_%'
	AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)
UNION ALL
SELECT 'CONSTRAINT ' || t.relname || ' ' || pg_get_constraintdef(c.oid)
FROM pg_constraint c INNER JOIN pg_class t ON t.oid = c.conrelid
WHERE c.connamespace = CURRENT_SCHEMA()::regnamespace AND t.relname NOT LIKE 'MIGRATION\_%'
UNION ALL
SELECT 'RELATION ' || c.relname || ' ' || c.relkind::TEXT
FROM pg_class c WHERE c.relnamespace = CURRENT_SCHEMA()::regnamespace
	AND c.relkind IN ('r', 'v', 'p') AND c.relname NOT LIKE 'MIGRATION\_%'
ORDER BY 1`

func schemaSignature(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(schemaSignatureQuery)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var signature []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		signature = append(signature, line)
	}
	require.NoError(t, rows.Err())
	return signature
}

func schemaScript(t *testing.T, store string) string {
	t.Helper()
	data, err := os.ReadFile( //nolint:gosec // The path is built from the fixed store names of the test.
		filepath.Join(releaseTestDirectory(t), "..", "..", "..", "..", "dbscripts", store, "postgres.sql"))
	require.NoError(t, err)
	return string(data)
}

// verifyMigratedSchemaMatchesFreshInstall compares every migrated database with a database created from the
// current schema scripts. After the whole plan a migrated installation must be indistinguishable from a new one.
func verifyMigratedSchemaMatchesFreshInstall(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("migrated_schema_matches_fresh_install", func(t *testing.T) {
		admin := environment.OpenNamedDatabase(t, "migration_admin")
		type pair struct {
			migrated string
			fresh    string
			scripts  []string
		}
		var pairs []pair
		if environment.Shared() {
			pairs = append(pairs, pair{migrated: "thunderid", fresh: "fresh_thunderid", scripts: schemaStores})
		} else {
			for _, store := range schemaStores {
				pairs = append(pairs, pair{migrated: store, fresh: "fresh_" + store, scripts: []string{store}})
			}
		}
		var differences []string
		for _, p := range pairs {
			_, err := admin.Exec(`CREATE DATABASE ` + p.fresh)
			require.NoError(t, err)
			fresh := environment.OpenNamedDatabase(t, p.fresh)
			for _, store := range p.scripts {
				_, err := fresh.Exec(schemaScript(t, store))
				require.NoError(t, err, "load the %s schema script", store)
			}
			expected := schemaSignature(t, fresh)
			actual := schemaSignature(t, environment.OpenNamedDatabase(t, p.migrated))
			missing, extra := signatureDifference(expected, actual)
			for _, line := range missing {
				differences = append(differences, p.migrated+": missing after migration: "+line)
			}
			for _, line := range extra {
				differences = append(differences, p.migrated+": not in a fresh install: "+line)
			}
		}
		require.Empty(t, differences, "a migrated database must match a fresh install")
		t.Log("verified: the migrated schema equals a fresh install for every store")
	})
}

// reloadRuntimePersistentCleanupRoutine installs the cleanup routine that ships with the target release. The
// routine of the source release reads the legacy session names, and the contract phase refuses to remove the
// compatibility views while it does.
func reloadRuntimePersistentCleanupRoutine(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseTestDirectory(t), "..", "..", "..", "..",
		"dbscripts", "runtime_persistent", "postgres-cleanup.sql"))
	require.NoError(t, err)
	_, err = environment.OpenDatabase(t, "runtime_persistent").Exec(string(data))
	require.NoError(t, err)
}

// signatureDifference returns the lines only the expected signature has and the lines only the actual one has.
func signatureDifference(expected, actual []string) ([]string, []string) {
	count := map[string]int{}
	for _, line := range expected {
		count[line]++
	}
	var extra []string
	for _, line := range actual {
		if count[line] > 0 {
			count[line]--
			continue
		}
		extra = append(extra, line)
	}
	var missing []string
	for _, line := range expected {
		if count[line] > 0 {
			count[line]--
			missing = append(missing, line)
		}
	}
	return missing, extra
}
