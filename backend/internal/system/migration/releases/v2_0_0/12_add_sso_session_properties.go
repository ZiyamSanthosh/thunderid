// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var (
	querySSOSessionTable = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-07",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = 'SSO_SESSION'`,
	}
	querySSOSessionPropertiesColumn = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-08",
		PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
			CASE WHEN COLUMN_DEFAULT IS NULL THEN 'no' ELSE 'yes' END AS has_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SSO_SESSION'
			AND COLUMN_NAME = 'properties'`,
	}
	querySSOSessionPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-09",
		PostgresQuery: `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'SSO_SESSION' AND c.contype = 'p'
			GROUP BY c.oid`,
	}
)

type ssoSessionPropertiesInspection struct {
	state   engine.VerificationState
	summary string
}

func newAddSSOSessionPropertiesOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/runtime_persistent/12_add_sso_session_properties",
			Description:      "Add nullable properties to SSO sessions",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkAddSSOSessionPropertiesReadiness,
		PreVerifier:      verifySSOSessionProperties,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "add-properties", Query: `ALTER TABLE "SSO_SESSION" ADD COLUMN PROPERTIES JSONB`},
				},
			},
		},
		PostVerifier: verifySSOSessionProperties,
	})
}

func checkAddSSOSessionPropertiesReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := inspectSSOSessionProperties(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	switch inspection.state {
	case engine.VerificationSource:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The nullable SSO session properties column is ready to be added"}, nil
	case engine.VerificationTarget:
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The nullable SSO session properties column is already present"}, nil
	default:
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: "unexpected_sso_session_properties_schema", Message: inspection.summary,
				Remediation: "Restore the expected SSO_SESSION source schema or reconcile the PROPERTIES column before retrying",
			}},
		}, nil
	}
}

func verifySSOSessionProperties(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := inspectSSOSessionProperties(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_sso_session_properties_schema", Message: inspection.summary,
		}}
	}
	return result, nil
}

func inspectSSOSessionProperties(
	ctx context.Context,
	access engine.ReadAccess,
) (ssoSessionPropertiesInspection, error) {
	tableRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySSOSessionTable)
	if err != nil {
		return ssoSessionPropertiesInspection{}, fmt.Errorf("inspect SSO_SESSION table: %w", err)
	}
	if len(tableRows) != 1 {
		return invalidSSOSessionProperties(fmt.Sprintf("Expected one SSO_SESSION table, found %d", len(tableRows))), nil
	}
	relationKind, err := tableRows[0].String("relation_kind")
	if err != nil {
		return ssoSessionPropertiesInspection{}, fmt.Errorf("inspect SSO_SESSION relation kind: %w", err)
	}
	if relationKind != "r" {
		return invalidSSOSessionProperties("Relation name SSO_SESSION is not an ordinary table"), nil
	}

	primaryKeyRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySSOSessionPrimaryKey)
	if err != nil {
		return ssoSessionPropertiesInspection{}, fmt.Errorf("inspect SSO_SESSION primary key: %w", err)
	}
	if len(primaryKeyRows) != 1 {
		return invalidSSOSessionProperties(fmt.Sprintf(
			"Expected one SSO_SESSION primary key, found %d", len(primaryKeyRows))), nil
	}
	primaryKeyColumns, err := primaryKeyRows[0].String("column_names")
	if err != nil {
		return ssoSessionPropertiesInspection{}, fmt.Errorf("inspect SSO_SESSION primary key columns: %w", err)
	}
	if primaryKeyColumns != "SESSION_ID,DEPLOYMENT_ID" {
		return invalidSSOSessionProperties(fmt.Sprintf(
			"Unexpected SSO_SESSION primary key columns %q", primaryKeyColumns)), nil
	}

	columnRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySSOSessionPropertiesColumn)
	if err != nil {
		return ssoSessionPropertiesInspection{}, fmt.Errorf("inspect SSO_SESSION.PROPERTIES column: %w", err)
	}
	if len(columnRows) == 0 {
		return ssoSessionPropertiesInspection{state: engine.VerificationSource,
			summary: "The SSO_SESSION.PROPERTIES column is absent"}, nil
	}
	if len(columnRows) != 1 {
		return invalidSSOSessionProperties(fmt.Sprintf(
			"Expected at most one SSO_SESSION.PROPERTIES column, found %d", len(columnRows))), nil
	}
	nullable, err := columnRows[0].String("is_nullable")
	if err != nil {
		return ssoSessionPropertiesInspection{}, err
	}
	dataType, err := columnRows[0].String("data_type")
	if err != nil {
		return ssoSessionPropertiesInspection{}, err
	}
	hasDefault, err := columnRows[0].String("has_default")
	if err != nil {
		return ssoSessionPropertiesInspection{}, err
	}
	if nullable != "YES" || dataType != "jsonb" || hasDefault != "no" {
		return invalidSSOSessionProperties(fmt.Sprintf(
			"Unexpected SSO_SESSION.PROPERTIES definition: nullable=%q, type=%q, hasDefault=%q",
			nullable, dataType, hasDefault)), nil
	}
	return ssoSessionPropertiesInspection{state: engine.VerificationTarget,
		summary: "The nullable SSO_SESSION.PROPERTIES JSONB column is present"}, nil
}

func invalidSSOSessionProperties(summary string) ssoSessionPropertiesInspection {
	return ssoSessionPropertiesInspection{state: engine.VerificationInvalid, summary: summary}
}
