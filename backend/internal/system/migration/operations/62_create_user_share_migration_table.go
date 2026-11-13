// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const createUserShareMigrationTableOperationID engine.OperationID = "entitydb/" +
	"62_create_user_share_migration_table"

// userShareMigrationTable is a working table that lives only for the duration of this release. It records,
// for every user, grouped by email, the original that survives and the snapshot
// values that later operations and their rollbacks need. Operation 17 drops it.
//
// For an original, ORIGINAL_USER_ID equals USER_ID, SHARING_POLICY_ID holds the ID of the policy that shares
// it, and OWNER_OU_ID is the organization unit that will own it: the common parent of the organization units
// of the original and its duplicates, because a unit can share only to its direct children. ADDED_GROUP_IDS
// lists only the group memberships the migration added to the original.
var userShareMigrationTable = tableSpec{
	storeID: engine.StoreIDEntityDB,
	name:    "USER_SHARE_MIGRATION",
	columns: []columnSpec{
		{"deployment_id", "NO", "character varying", "255", "none"},
		{"user_id", "NO", "character varying", "36", "none"},
		{"is_original", "NO", "boolean", "none", "none"},
		{"original_user_id", "NO", "character varying", "36", "none"},
		{"email", "NO", "character varying", "320", "none"},
		{"ou_id", "NO", "character varying", "36", "none"},
		{"linked_accounts", "NO", "jsonb", "none", "'{}'::jsonb"},
		{"sharing_policy_id", "YES", "character varying", "36", "none"},
		{"owner_ou_id", "YES", "character varying", "36", "none"},
		{"added_group_ids", "NO", "jsonb", "none", "'[]'::jsonb"},
		{"created_at", "NO", "timestamp with time zone", "none", "none"},
	},
	keys: []keySpec{{typeCode: "p", columns: "deployment_id,user_id"}},
	indexes: []indexSpec{
		{name: "idx_user_share_migration_original", unique: "false", columns: "deployment_id,original_user_id"},
	},
}

// NewCreateUserShareMigrationTableOperation returns the create user share migration table operation.
func NewCreateUserShareMigrationTableOperation() (engine.Operation, error) {
	return newCreateTableOperation(
		createUserShareMigrationTableOperationID,
		"Create the working table that pairs duplicate users with their original",
		userShareMigrationTable,
		[]engine.SQLStatement{
			{ID: "create-user-share-migration-table", Query: postgresCreateUserShareMigrationTableSQL},
			{ID: "create-user-share-migration-original-index", Query: `CREATE INDEX
    idx_user_share_migration_original ON "USER_SHARE_MIGRATION" (DEPLOYMENT_ID, ORIGINAL_USER_ID)`},
		},
	)
}

const postgresCreateUserShareMigrationTableSQL = `CREATE TABLE "USER_SHARE_MIGRATION" (
    DEPLOYMENT_ID     VARCHAR(255) NOT NULL,
    USER_ID           VARCHAR(36)  NOT NULL,
    IS_ORIGINAL       BOOLEAN      NOT NULL,
    ORIGINAL_USER_ID  VARCHAR(36)  NOT NULL,
    EMAIL             VARCHAR(320) NOT NULL,
    OU_ID             VARCHAR(36)  NOT NULL,
    LINKED_ACCOUNTS   JSONB        NOT NULL DEFAULT '{}',
    SHARING_POLICY_ID VARCHAR(36),
    OWNER_OU_ID       VARCHAR(36),
    ADDED_GROUP_IDS   JSONB        NOT NULL DEFAULT '[]',
    CREATED_AT        TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (DEPLOYMENT_ID, USER_ID)
)`
