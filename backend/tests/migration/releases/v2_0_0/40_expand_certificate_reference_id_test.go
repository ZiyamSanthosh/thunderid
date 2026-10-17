// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyExpandCertificateReferenceID(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("40_expand_certificate_reference_id", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT CHARACTER_MAXIMUM_LENGTH FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA()
					AND TABLE_NAME = 'CERTIFICATE' AND COLUMN_NAME = 'ref_id'`,
			"2048")
		migrationharness.AssertQueryValue(t, db,
			`SELECT REF_ID FROM "CERTIFICATE" WHERE ID = '0199a123-4799-7abc-8def-0123456789ab'`,
			legacyCertificateReferenceID)
		longReferenceID := strings.Repeat("r", 2048)
		_, err := db.Exec(`INSERT INTO "CERTIFICATE"
			(DEPLOYMENT_ID, ID, REF_TYPE, REF_ID, TYPE, VALUE)
			VALUES ('migration-deployment', '0199a123-4798-7abc-8def-0123456789ab',
				'application', $1, 'JWKS', 'target-certificate')`, longReferenceID)
		require.NoError(t, err)
		migrationharness.AssertOperationCompleted(t, db,
			"2.0.0/configdb/40_expand_certificate_reference_id")
		t.Log("verified: CERTIFICATE.REF_ID accepts 2048 characters without changing existing values")
	})
}
