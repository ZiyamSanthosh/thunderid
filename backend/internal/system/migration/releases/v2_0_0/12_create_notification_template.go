// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const notificationTemplatePrimaryKeyName = "NOTIFICATION_TEMPLATE_pkey"

var notificationTemplateTable = configTableSpec{
	name: "NOTIFICATION_TEMPLATE",
	columns: []configColumnSpec{
		{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "channel", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
		{name: "handle", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "ou_id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
		{name: "display_name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
		{name: "description", nullable: "YES", dataType: "character varying", maximumLength: "512", defaultValue: "none"},
		{name: "content", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
		{name: "design", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "'{}'::jsonb"},
		{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
		{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	},
	keys: []configKeySpec{
		{typeCode: "p", columns: "deployment_id,id"},
		{typeCode: "u", columns: "deployment_id,channel,handle"},
	},
	reservedRelationNames: []string{
		notificationTemplatePrimaryKeyName,
		"NOTIFICATION_TEMPLATE_DEPLOYMENT_ID_CHANNEL_HANDLE_key",
	},
}

func newCreateNotificationTemplateOperation() (engine.Operation, error) {
	return newConfigTableOperation(
		"2.0.0/configdb/12_create_notification_template",
		"Create the notification template table",
		notificationTemplateTable,
		[]engine.SQLStatement{{ID: "create-table", Query: postgresCreateNotificationTemplateSQL}},
	)
}

const postgresCreateNotificationTemplateSQL = `CREATE TABLE "NOTIFICATION_TEMPLATE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID            VARCHAR(36)  NOT NULL,
    CHANNEL       VARCHAR(16)  NOT NULL,
    HANDLE        VARCHAR(255) NOT NULL,
    OU_ID         VARCHAR(36)  NOT NULL,
    DISPLAY_NAME  VARCHAR(255) NOT NULL,
    DESCRIPTION   VARCHAR(512),
    CONTENT       JSONB        NOT NULL,
    DESIGN        JSONB        NOT NULL DEFAULT '{}',
    CREATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    UPDATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, ID),
    UNIQUE (DEPLOYMENT_ID, CHANNEL, HANDLE)
)`
