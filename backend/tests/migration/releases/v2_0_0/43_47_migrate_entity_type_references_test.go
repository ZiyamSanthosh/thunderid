// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

func verifyMigrateEntityTypeReferences(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("43_47_migrate_entity_type_references", func(t *testing.T) {
		entityDB := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT TYPE FROM "ENTITY" WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4703-7abc-8def-0123456789ab'`,
			"legacy-person")
		migrationharness.AssertOperationCompleted(t, entityDB,
			"2.0.0/entitydb/43_migrate_entity_type_references")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT TYPE FROM "ENTITY" WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4708-7abc-8def-0123456789ab'`,
			"declarative-customer")

		configDB := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT PROPERTIES -> 'allowedUserTypes' ->> 0 FROM "INBOUND_CLIENT"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`,
			"legacy-person")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT PROPERTIES -> 'subjectAttribute' ->> 'legacy-person' FROM "INBOUND_CLIENT"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`,
			"email")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT PROPERTIES -> 'allowedUserTypes' ->> 1 FROM "INBOUND_CLIENT"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`,
			"declarative-customer")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT PROPERTIES -> 'subjectAttribute' ->> 'declarative-customer' FROM "INBOUND_CLIENT"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ENTITY_ID = '0199a123-4794-7abc-8def-0123456789ab'`,
			"username")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT ATTRIBUTE_CONFIGURATION -> 'userTypeResolution' ->> 'default' FROM "IDP"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4792-7abc-8def-0123456789ab'`,
			"legacy-person")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT ATTRIBUTE_CONFIGURATION -> 'userTypeResolution' -> 'valueMapping' ->> 'declared'
				FROM "IDP" WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4792-7abc-8def-0123456789ab'`,
			"declarative-customer")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT NODES -> 0 -> 'properties' -> 'allowedUserTypes' ->> 0 FROM "FLOW_VERSION"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND FLOW_ID = '0199a123-4791-7abc-8def-0123456789ab' AND VERSION = 1`,
			"legacy-person")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT NODES -> 0 -> 'properties' -> 'allowedUserTypes' ->> 1 FROM "FLOW_VERSION"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND FLOW_ID = '0199a123-4791-7abc-8def-0123456789ab' AND VERSION = 1`,
			"declarative-customer")
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT PROPERTIES -> 'subjectAttributeMappings' -> 1 ->> 'entityType'
				FROM "AUTHORIZATION_PDP_CONNECTION"
				WHERE DEPLOYMENT_ID = 'migration-deployment'
				AND ID = '0199a123-4782-7abc-8def-0123456789ab'`,
			"declarative-customer")

		for _, operationID := range []string{
			"2.0.0/configdb/44_migrate_inbound_client_entity_type_references",
			"2.0.0/configdb/45_migrate_idp_entity_type_references",
			"2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references",
			"2.0.0/configdb/47_migrate_flow_entity_type_references",
		} {
			migrationharness.AssertOperationCompleted(t, configDB, operationID)
		}
		t.Log("verified: database and declarative entity-type names were replaced by handles in every supported reference")
	})
}
