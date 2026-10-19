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

const declarativeEntityTypeName = "Declarative Customer"

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
				'0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
			('migration-deployment', '0199a123-4705-7abc-8def-0123456789ab', 'agent', 'Default',
				'0199a123-4702-7abc-8def-0123456789ab', FALSE,
				'{"modelProvider":{"type":"string","required":false}}'::jsonb),
			('migration-deployment', '0199a123-4707-7abc-8def-0123456789ab', 'user',
				'declarative-customer', '0199a123-4702-7abc-8def-0123456789ab', FALSE,
				'{}'::jsonb)`, legacyEntityTypeName)
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
	_, err = configDB.Exec(`INSERT INTO "THEME"
		(DEPLOYMENT_ID, ID, DISPLAY_NAME, HANDLE, THEME)
		VALUES ('migration-deployment', '0199a123-4780-7abc-8def-0123456789ab',
			'Legacy Theme', 'legacy-theme', '{}'::jsonb)`)
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "LAYOUT"
		(DEPLOYMENT_ID, ID, DISPLAY_NAME, HANDLE, LAYOUT)
		VALUES ('migration-deployment', '0199a123-4781-7abc-8def-0123456789ab',
			'Legacy Layout', 'legacy-layout', '{}'::jsonb)`)
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "INBOUND_CLIENT"
		(DEPLOYMENT_ID, ENTITY_ID, AUTH_FLOW_ID, THEME_ID, LAYOUT_ID, PROPERTIES)
		VALUES ('migration-deployment', '0199a123-4794-7abc-8def-0123456789ab',
			'0199a123-4793-7abc-8def-0123456789ab',
			'0199a123-4780-7abc-8def-0123456789ab',
			'0199a123-4781-7abc-8def-0123456789ab', $1::jsonb)`, fmt.Sprintf(`{
			"allowedUserTypes":[%q,%q],
			"subjectAttribute":{%q:"email",%q:"username"}
		}`, legacyEntityTypeName, declarativeEntityTypeName,
		legacyEntityTypeName, declarativeEntityTypeName))
	require.NoError(t, err)
	_, err = configDB.Exec(`ALTER TABLE "INBOUND_CLIENT"
		ADD CONSTRAINT "INBOUND_CLIENT_theme_id_fkey" FOREIGN KEY (THEME_ID)
		REFERENCES "THEME" (ID) ON DELETE SET NULL`)
	require.NoError(t, err)
	_, err = configDB.Exec(`ALTER TABLE "INBOUND_CLIENT"
		ADD CONSTRAINT "INBOUND_CLIENT_layout_id_fkey" FOREIGN KEY (LAYOUT_ID)
		REFERENCES "LAYOUT" (ID) ON DELETE SET NULL`)
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "IDP"
		(DEPLOYMENT_ID, ID, NAME, TYPE, ATTRIBUTE_CONFIGURATION)
		VALUES ('migration-deployment', '0199a123-4792-7abc-8def-0123456789ab',
			'Legacy IDP', 'OIDC', $1::jsonb)`, fmt.Sprintf(`{
			"userTypeResolution":{"default":%q,"valueMapping":{"legacy":%q,"declared":%q}},
			"userTypeAttributeMappings":[
				{"userType":%q,"attributes":[]},
				{"userType":%q,"attributes":[]}
			]
		}`, legacyEntityTypeName, legacyEntityTypeName, declarativeEntityTypeName,
		legacyEntityTypeName, declarativeEntityTypeName))
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "FLOW"
		(DEPLOYMENT_ID, ID, HANDLE, NAME, FLOW_TYPE, ACTIVE_VERSION)
		VALUES ('migration-deployment', '0199a123-4791-7abc-8def-0123456789ab',
			'legacy-registration', 'Legacy Registration', 'REGISTRATION', 1)`)
	require.NoError(t, err)
	_, err = configDB.Exec(`INSERT INTO "FLOW_VERSION"
		(DEPLOYMENT_ID, FLOW_ID, VERSION, NODES)
		VALUES ('migration-deployment', '0199a123-4791-7abc-8def-0123456789ab', 1, $1::jsonb)`,
		fmt.Sprintf(`[{"id":"resolver","type":"TASK_EXECUTION",
			"executor":{"name":"UserTypeResolver"},
			"properties":{"allowedUserTypes":[%q,%q]}}]`,
			legacyEntityTypeName, declarativeEntityTypeName))
	require.NoError(t, err)

	entityDB := environment.OpenDatabase(t, "entitydb")
	_, err = entityDB.Exec(`INSERT INTO "ENTITY"
        (DEPLOYMENT_ID, ID, CATEGORY, TYPE, STATE, OU_ID, CREATED_AT, UPDATED_AT)
        VALUES ('migration-deployment', '0199a123-4703-7abc-8def-0123456789ab', 'user', $1, 'ACTIVE',
            '0199a123-4702-7abc-8def-0123456789ab', NOW(), NOW())`, legacyEntityTypeName)
	require.NoError(t, err)
	_, err = entityDB.Exec(`INSERT INTO "ENTITY"
		(DEPLOYMENT_ID, ID, CATEGORY, TYPE, STATE, OU_ID, ATTRIBUTES, SYSTEM_ATTRIBUTES, CREATED_AT, UPDATED_AT)
		VALUES
			('migration-deployment', '0199a123-4708-7abc-8def-0123456789ab', 'user', $1, 'ACTIVE',
				'0199a123-4702-7abc-8def-0123456789ab', '{}'::jsonb, '{}'::jsonb, NOW(), NOW()),
			('migration-deployment', '0199a123-4712-7abc-8def-0123456789ab', 'user', $2, 'ACTIVE',
				'0199a123-4702-7abc-8def-0123456789ab', '{"name":"Shared User Name"}'::jsonb,
				'{}'::jsonb, NOW(), NOW()),
			('migration-deployment', '0199a123-4713-7abc-8def-0123456789ab', 'user', $2, 'ACTIVE',
				'0199a123-4702-7abc-8def-0123456789ab', '{"name":"Shared User Name"}'::jsonb,
				'{}'::jsonb, NOW(), NOW()),
			('migration-deployment', '0199a123-4714-7abc-8def-0123456789ab', 'user', $2, 'ACTIVE',
				'0199a123-4702-7abc-8def-0123456789ab', '{"name":"Calendar Agent"}'::jsonb,
				'{}'::jsonb, NOW(), NOW())`, declarativeEntityTypeName, legacyEntityTypeName)
	require.NoError(t, err)
	_, err = entityDB.Exec(`INSERT INTO "ENTITY"
		(DEPLOYMENT_ID, ID, CATEGORY, TYPE, STATE, OU_ID, ATTRIBUTES, SYSTEM_ATTRIBUTES, CREATED_AT, UPDATED_AT)
		VALUES ('migration-deployment', '0199a123-4706-7abc-8def-0123456789ab', 'agent', 'Default', 'ACTIVE',
			'0199a123-4702-7abc-8def-0123456789ab', '{"modelProvider":"openai"}'::jsonb,
			'{"name":"Calendar Agent","description":"Schedules meetings"}'::jsonb, NOW(), NOW())`)
	require.NoError(t, err)
	_, err = entityDB.Exec(`INSERT INTO "ENTITY_IDENTIFIER"
		(DEPLOYMENT_ID, ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT)
		VALUES
			('migration-deployment', '0199a123-4703-7abc-8def-0123456789ab',
				'username', 'legacy-person', 'attribute', NOW()),
			('migration-deployment', '0199a123-4706-7abc-8def-0123456789ab',
				'name', 'Calendar Agent', 'system', NOW())`)
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

func preparePostExpandMigrationSource(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	configDB := environment.OpenDatabase(t, "configdb")
	_, err := configDB.Exec(`INSERT INTO "AUTHORIZATION_PDP_CONNECTION"
		(DEPLOYMENT_ID, ID, NAME, TYPE, PROPERTIES)
		VALUES ('migration-deployment', '0199a123-4782-7abc-8def-0123456789ab',
			'Migration PDP', 'authzen-pdp', $1::jsonb)`, fmt.Sprintf(`{
			"endpoint":"https://pdp.example.test",
			"subjectAttributeMappings":[
				{"entityType":%q,"attributes":[]},
				{"entityType":%q,"attributes":[]}
			]
		}`, legacyEntityTypeName, declarativeEntityTypeName))
	require.NoError(t, err)
}
