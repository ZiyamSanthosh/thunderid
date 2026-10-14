// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const notificationTemplatePrimaryKeyName = "NOTIFICATION_TEMPLATE_pkey"

var (
	queryNotificationTemplateTable = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-14",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = 'NOTIFICATION_TEMPLATE'`,
	}
	queryNotificationTemplateColumns = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-15",
		PostgresQuery: `SELECT COLUMN_NAME, IS_NULLABLE, DATA_TYPE,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length,
			COALESCE(COLUMN_DEFAULT, 'none') AS column_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'NOTIFICATION_TEMPLATE'
			ORDER BY ORDINAL_POSITION`,
	}
	queryNotificationTemplatePrimaryKey = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-16",
		PostgresQuery: `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'NOTIFICATION_TEMPLATE' AND c.contype = 'p'
			GROUP BY c.oid`,
	}
	queryNotificationTemplateUniqueKeys = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-17",
		PostgresQuery: `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'NOTIFICATION_TEMPLATE' AND c.contype = 'u'
			GROUP BY c.oid
			ORDER BY column_names`,
	}
	queryNotificationTemplateGeneratedNames = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-18",
		PostgresQuery: `SELECT c.relname AS relation_name
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA()
			AND c.relname IN ('NOTIFICATION_TEMPLATE_pkey',
				'NOTIFICATION_TEMPLATE_DEPLOYMENT_ID_CHANNEL_HANDLE_key')
			ORDER BY c.relname`,
	}
)

type notificationTemplateInspection struct {
	state   engine.VerificationState
	summary string
}

type notificationTemplateColumn struct {
	name          string
	nullable      string
	dataType      string
	maximumLength string
	defaultValue  string
}

var notificationTemplateColumns = []notificationTemplateColumn{
	{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
	{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", defaultValue: "none"},
	{name: "channel", nullable: "NO", dataType: "character varying", maximumLength: "16", defaultValue: "none"},
	{name: "handle", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
	{name: "display_name", nullable: "NO", dataType: "character varying", maximumLength: "255", defaultValue: "none"},
	{name: "description", nullable: "YES", dataType: "character varying", maximumLength: "512", defaultValue: "none"},
	{name: "content", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "none"},
	{name: "design", nullable: "NO", dataType: "jsonb", maximumLength: "none", defaultValue: "'{}'::jsonb"},
	{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
	{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", defaultValue: "now()"},
}

func newCreateNotificationTemplateOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/13_create_notification_template",
			Description:      "Create the notification template table",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkCreateNotificationTemplateReadiness,
		PreVerifier:      verifyNotificationTemplate,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{{ID: "create-table", Query: postgresCreateNotificationTemplateSQL}},
			},
		},
		PostVerifier: verifyNotificationTemplate,
	})
}

func checkCreateNotificationTemplateReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectNotificationTemplate(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The notification template table is ready to be created"}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The notification template table already matches the target schema"}, nil
	default:
		return blockedNotificationTemplateReadiness(inspection.summary), nil
	}
}

func blockedNotificationTemplateReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "unexpected_notification_template_schema", Message: summary,
			Remediation: "Remove or reconcile the conflicting NOTIFICATION_TEMPLATE schema before retrying",
		}},
	}
}

func verifyNotificationTemplate(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := inspectNotificationTemplate(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_notification_template_schema", Message: inspection.summary,
		}}
	}
	return result, nil
}

func inspectNotificationTemplate(
	ctx context.Context,
	access engine.ReadAccess,
) (notificationTemplateInspection, error) {
	tableRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryNotificationTemplateTable)
	if err != nil {
		return notificationTemplateInspection{}, fmt.Errorf("inspect notification template table: %w", err)
	}
	if len(tableRows) == 0 {
		nameRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryNotificationTemplateGeneratedNames)
		if err != nil {
			return notificationTemplateInspection{}, fmt.Errorf("inspect notification template generated names: %w", err)
		}
		if len(nameRows) != 0 {
			name, readErr := nameRows[0].String("relation_name")
			if readErr != nil {
				return notificationTemplateInspection{}, readErr
			}
			return invalidNotificationTemplate(fmt.Sprintf("Relation name %s is already in use", name)), nil
		}
		return notificationTemplateInspection{state: engine.VerificationSource,
			summary: "The NOTIFICATION_TEMPLATE table is absent"}, nil
	}
	if len(tableRows) != 1 {
		return invalidNotificationTemplate(fmt.Sprintf(
			"Expected one NOTIFICATION_TEMPLATE table, found %d", len(tableRows))), nil
	}
	relationKind, err := tableRows[0].String("relation_kind")
	if err != nil {
		return notificationTemplateInspection{}, err
	}
	if relationKind != "r" {
		return invalidNotificationTemplate("Relation name NOTIFICATION_TEMPLATE is not an ordinary table"), nil
	}

	columnRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryNotificationTemplateColumns)
	if err != nil {
		return notificationTemplateInspection{}, fmt.Errorf("inspect notification template columns: %w", err)
	}
	if err := validateNotificationTemplateColumns(columnRows); err != nil {
		return invalidNotificationTemplate(err.Error()), nil
	}
	primaryKeyRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryNotificationTemplatePrimaryKey)
	if err != nil {
		return notificationTemplateInspection{}, fmt.Errorf("inspect notification template primary key: %w", err)
	}
	if err := validateNotificationTemplateKey(primaryKeyRows, "DEPLOYMENT_ID,ID", "primary key"); err != nil {
		return invalidNotificationTemplate(err.Error()), nil
	}
	uniqueRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryNotificationTemplateUniqueKeys)
	if err != nil {
		return notificationTemplateInspection{}, fmt.Errorf("inspect notification template unique constraints: %w", err)
	}
	if err := validateNotificationTemplateKey(
		uniqueRows, "DEPLOYMENT_ID,CHANNEL,HANDLE", "unique constraint"); err != nil {
		return invalidNotificationTemplate(err.Error()), nil
	}
	return notificationTemplateInspection{state: engine.VerificationTarget,
		summary: "The NOTIFICATION_TEMPLATE table matches the target schema"}, nil
}

func validateNotificationTemplateColumns(rows []engine.QueryRow) error {
	if len(rows) != len(notificationTemplateColumns) {
		return fmt.Errorf("expected %d NOTIFICATION_TEMPLATE columns, found %d",
			len(notificationTemplateColumns), len(rows))
	}
	for index, expected := range notificationTemplateColumns {
		name, err := rows[index].String("column_name")
		if err != nil {
			return err
		}
		nullable, err := rows[index].String("is_nullable")
		if err != nil {
			return err
		}
		dataType, err := rows[index].String("data_type")
		if err != nil {
			return err
		}
		maximumLength, err := rows[index].String("maximum_length")
		if err != nil {
			return err
		}
		defaultValue, err := rows[index].String("column_default")
		if err != nil {
			return err
		}
		if name != expected.name || nullable != expected.nullable || dataType != expected.dataType ||
			maximumLength != expected.maximumLength || defaultValue != expected.defaultValue {
			return fmt.Errorf("NOTIFICATION_TEMPLATE column %d does not match the target schema", index+1)
		}
	}
	return nil
}

func validateNotificationTemplateKey(rows []engine.QueryRow, expected string, kind string) error {
	if len(rows) != 1 {
		return fmt.Errorf("expected one NOTIFICATION_TEMPLATE %s, found %d", kind, len(rows))
	}
	columns, err := rows[0].String("column_names")
	if err != nil {
		return err
	}
	if columns != expected {
		return fmt.Errorf("NOTIFICATION_TEMPLATE %s does not match the target schema", kind)
	}
	return nil
}

func invalidNotificationTemplate(summary string) notificationTemplateInspection {
	return notificationTemplateInspection{state: engine.VerificationInvalid, summary: summary}
}

const postgresCreateNotificationTemplateSQL = `CREATE TABLE "NOTIFICATION_TEMPLATE" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID            VARCHAR(36)  NOT NULL,
    CHANNEL       VARCHAR(16)  NOT NULL,
    HANDLE        VARCHAR(255) NOT NULL,
    DISPLAY_NAME  VARCHAR(255) NOT NULL,
    DESCRIPTION   VARCHAR(512),
    CONTENT       JSONB        NOT NULL,
    DESIGN        JSONB        NOT NULL DEFAULT '{}',
    CREATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    UPDATED_AT    TIMESTAMPTZ  DEFAULT NOW(),
    PRIMARY KEY (DEPLOYMENT_ID, ID),
    UNIQUE (DEPLOYMENT_ID, CHANNEL, HANDLE)
)`
