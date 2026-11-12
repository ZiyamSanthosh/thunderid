// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package migration_test

import (
	"testing"

	migrationharness "github.com/thunder-id/thunderid/tests/migration/harness"
)

// verifyOwnerAndSharingMigration checks the data the plan converts: the owner of rows that had none, the
// organization unit of existing role assignments, and the replacement of one person's duplicate accounts by one
// shared account.
func verifyOwnerAndSharingMigration(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("owner_and_sharing_data", func(t *testing.T) {
		configDB := environment.OpenDatabase(t, "configdb")
		for _, table := range []string{"FLOW", "IDP", "NOTIFICATION_SENDER"} {
			migrationharness.AssertQueryValue(t, configDB,
				`SELECT COUNT(*)::TEXT FROM "`+table+`" WHERE OU_ID IS DISTINCT FROM $1`, "0", systemOrganizationUnit)
			migrationharness.AssertQueryValue(t, configDB,
				`SELECT IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = $1 AND COLUMN_NAME = 'ou_id'`, "NO", table)
		}
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT ASSIGNMENT_OU_ID FROM "ROLE_ASSIGNMENT" WHERE ROLE_ID = $1 AND ASSIGNEE_ID = $2`,
			fixtureRoleOU, fixtureRoleID, fixtureLegacyAssigneeID)
		migrationharness.AssertQueryValue(t, configDB,
			`SELECT COUNT(*)::TEXT FROM "RESOURCE_SHARING_POLICY" WHERE RESOURCE_ID = $1`, "1", fixtureOriginalUser)

		entityDB := environment.OpenDatabase(t, "entitydb")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT OU_ID FROM "ENTITY" WHERE ID = $1`, fixtureShareParentOU, fixtureOriginalUser)
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM "ENTITY" WHERE ID = $1`, "0", fixtureDuplicateUser)
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT COUNT(*)::TEXT FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'USER_SHARE_MIGRATION'`, "0")
		migrationharness.AssertQueryValue(t, entityDB,
			`SELECT SYSTEM_ATTRIBUTES #>> '{owner,id}' || ':' || (SYSTEM_ATTRIBUTES #>> '{owner,idType}') ||
				':' || (SYSTEM_ATTRIBUTES #>> '{owner,isLocal}')
				FROM "ENTITY" WHERE ID = '0199a123-4706-7abc-8def-0123456789ab'`,
			fixtureLegacyAssigneeID+":uuid:true")
		t.Log("verified: owners, assignment organization units, the duplicate user conversion and agent owners")
	})
}
