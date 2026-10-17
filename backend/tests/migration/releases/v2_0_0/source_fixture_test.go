// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

const legacyEntityTypeName = "Legacy Person"

const legacyCertificateReferenceID = "legacy-client"

const legacyRevocationCriterionValue = "legacy-token-family"

const entityTypeBackfillFixtureCount = 101

func prepareEntityTypeMigrationSource(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	configDB := environment.OpenDatabase(t, "configdb")
	_, err := configDB.Exec(`INSERT INTO "ENTITY_TYPES"
        (DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
		VALUES
			('migration-deployment', '0199a123-4701-7abc-8def-0123456789ab', 'user', $1,
				'0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
			('migration-deployment', '0199a123-4704-7abc-8def-0123456789ab', 'user', 'legacy-person',
				'0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb)`, legacyEntityTypeName)
	require.NoError(t, err)
	for index := 0; index < entityTypeBackfillFixtureCount-2; index++ {
		_, err = configDB.Exec(`INSERT INTO "ENTITY_TYPES"
			(DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
			VALUES ('migration-deployment', $1, 'user', $2,
				'0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb)`,
			fmt.Sprintf("0199a123-%04d-7abc-8def-0123456789ab", 4800+index),
			fmt.Sprintf("Batch Entity %03d", index+1))
		require.NoError(t, err)
	}
	_, err = configDB.Exec(`INSERT INTO "CERTIFICATE"
		(DEPLOYMENT_ID, ID, REF_TYPE, REF_ID, TYPE, VALUE)
		VALUES ('migration-deployment', '0199a123-4799-7abc-8def-0123456789ab',
			'application', $1, 'JWKS', 'legacy-certificate')`, legacyCertificateReferenceID)
	require.NoError(t, err)

	entityDB := environment.OpenDatabase(t, "entitydb")
	_, err = entityDB.Exec(`INSERT INTO "ENTITY"
        (DEPLOYMENT_ID, ID, CATEGORY, TYPE, STATE, OU_ID, CREATED_AT, UPDATED_AT)
        VALUES ('migration-deployment', '0199a123-4703-7abc-8def-0123456789ab', 'user', $1, 'ACTIVE',
            '0199a123-4702-7abc-8def-0123456789ab', NOW(), NOW())`, legacyEntityTypeName)
	require.NoError(t, err)
	_, err = entityDB.Exec(`INSERT INTO "ENTITY_IDENTIFIER"
		(DEPLOYMENT_ID, ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT)
		VALUES ('migration-deployment', '0199a123-4703-7abc-8def-0123456789ab',
			'username', 'legacy-person', 'attribute', NOW())`)
	require.NoError(t, err)

	runtimeDB := environment.OpenDatabase(t, "runtime_persistent")
	_, err = runtimeDB.Exec(`INSERT INTO "REVOCATION_CRITERIA"
		(DEPLOYMENT_ID, ID, CRITERION_TYPE, CRITERION_VALUE, REASON, REVOKED_AT, EXPIRY_TIME)
		VALUES ('migration-deployment', '0199a123-4797-7abc-8def-0123456789ab',
			'token_family', $1, 'test', NOW(), NOW() + INTERVAL '1 day')`, legacyRevocationCriterionValue)
	require.NoError(t, err)
	_, err = runtimeDB.Exec(`INSERT INTO "SSO_SESSION_PARTICIPANT"
		(SESSION_ID, DEPLOYMENT_ID, APP_ID, TFID, FIRST_JOINED_AT, LAST_ACTIVE_AT)
		VALUES ('0199a123-4796-7abc-8def-0123456789ab', 'migration-deployment',
			'0199a123-4795-7abc-8def-0123456789ab', NULL, NOW(), NOW())`)
	require.NoError(t, err)
	t.Log("prepared: legacy entity-type and entity rows for migration verification")
}
