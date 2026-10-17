// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const ssoSessionParticipantApplicationIndexColumns = "DEPLOYMENT_ID,APP_ID"

var (
	querySSOSessionParticipantApplicationColumns = dbmodel.DBQuery{
		ID: "MIG-RTP-SSO-42-01",
		PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SSO_SESSION_PARTICIPANT'
				AND UPPER(COLUMN_NAME) IN ('DEPLOYMENT_ID', 'APP_ID')
			ORDER BY ORDINAL_POSITION`,
	}
	querySSOSessionParticipantApplicationIndex = dbmodel.DBQuery{
		ID: "MIG-RTP-SSO-42-02",
		PostgresQuery: `SELECT i.indisunique AS is_unique, i.indisvalid AS is_valid,
				i.indisready AS is_ready, (i.indpred IS NOT NULL) AS has_predicate,
				am.amname AS access_method,
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_class idx
			INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
			INNER JOIN pg_index i ON i.indexrelid = idx.oid
			INNER JOIN pg_am am ON am.oid = idx.relam
			INNER JOIN pg_class t ON t.oid = i.indrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'SSO_SESSION_PARTICIPANT'
				AND idx.relname = 'idx_sso_session_participant_app'
			GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready, i.indpred, am.amname`,
	}
)

type ssoSessionParticipantApplicationIndexOperation struct {
	definition       engine.OperationDefinition
	createIndexQuery dbmodel.DBQuery
	dropIndexQuery   dbmodel.DBQuery
}

type ssoSessionParticipantApplicationIndexInspection struct {
	present bool
	valid   bool
}

func newAddSSOSessionParticipantApplicationIndexOperation() (engine.Operation, error) {
	return &ssoSessionParticipantApplicationIndexOperation{
		definition: engine.OperationDefinition{
			ID:               "2.0.0/runtime_persistent/42_add_sso_session_participant_application_index",
			Description:      "Add the deployment-scoped SSO session participant application lookup index",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		createIndexQuery: dbmodel.DBQuery{
			ID: "MIG-RTP-SSO-42-03",
			PostgresQuery: `CREATE INDEX CONCURRENTLY "idx_sso_session_participant_app"
				ON "SSO_SESSION_PARTICIPANT" (DEPLOYMENT_ID, APP_ID)`,
		},
		dropIndexQuery: dbmodel.DBQuery{
			ID:            "MIG-RTP-SSO-42-04",
			PostgresQuery: `DROP INDEX CONCURRENTLY "idx_sso_session_participant_app"`,
		},
	}, nil
}

func (o *ssoSessionParticipantApplicationIndexOperation) Definition() engine.OperationDefinition {
	return o.definition
}

func (o *ssoSessionParticipantApplicationIndexOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := verifySSOSessionParticipantApplicationIndex(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: verification.Summary,
			Findings: []engine.Finding{{
				Code:        "unexpected_sso_session_participant_application_index",
				Message:     verification.Summary,
				Remediation: "Restore the expected SSO_SESSION_PARTICIPANT columns or remove the conflicting index",
			}},
		}, nil
	}
	return engine.ReadinessResult{
		State: engine.ReadinessReady, Summary: verification.Summary,
	}, nil
}

func (*ssoSessionParticipantApplicationIndexOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (*ssoSessionParticipantApplicationIndexOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifySSOSessionParticipantApplicationIndex(ctx, access, target)
}

func (o *ssoSessionParticipantApplicationIndexOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	verification, err := verifySSOSessionParticipantApplicationIndex(
		ctx, execution.Access(), execution.Target())
	if err != nil {
		return err
	}
	if verification.State == engine.VerificationTarget {
		return nil
	}
	if verification.State != engine.VerificationSource {
		return fmt.Errorf("prepare SSO session participant application index: %s", verification.Summary)
	}
	inspection, invalidSummary, err := inspectSSOSessionParticipantApplicationIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if invalidSummary != "" {
		return fmt.Errorf("prepare SSO session participant application index: %s", invalidSummary)
	}
	if inspection.present {
		if inspection.valid {
			return nil
		}
		if _, err := execution.Access().Execute(
			ctx, engine.StoreIDRuntimePersistent, o.dropIndexQuery); err != nil {
			return fmt.Errorf("drop invalid SSO session participant application index: %w", err)
		}
	}
	if _, err := execution.Access().Execute(
		ctx, engine.StoreIDRuntimePersistent, o.createIndexQuery); err != nil {
		return fmt.Errorf("create SSO session participant application index concurrently: %w", err)
	}
	return nil
}

func (*ssoSessionParticipantApplicationIndexOperation) Finalize(
	context.Context,
	engine.OperationExecution,
) error {
	return nil
}

func (*ssoSessionParticipantApplicationIndexOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifySSOSessionParticipantApplicationIndex(ctx, access, target)
}

func verifySSOSessionParticipantApplicationIndex(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	columnRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent,
		querySSOSessionParticipantApplicationColumns)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf(
			"inspect SSO_SESSION_PARTICIPANT application lookup columns: %w", err)
	}
	if len(columnRows) != 2 {
		return invalidSSOSessionParticipantApplicationIndex(fmt.Sprintf(
			"Expected two SSO_SESSION_PARTICIPANT application lookup columns, found %d", len(columnRows))), nil
	}
	expectedColumns := []string{"DEPLOYMENT_ID", "APP_ID"}
	for index, row := range columnRows {
		columnName, columnNameErr := row.String("column_name")
		nullable, nullableErr := row.String("is_nullable")
		if err := errorsJoin(columnNameErr, nullableErr); err != nil {
			return engine.VerificationResult{}, err
		}
		if columnName != expectedColumns[index] || nullable != "NO" {
			return invalidSSOSessionParticipantApplicationIndex(fmt.Sprintf(
				"Unexpected SSO_SESSION_PARTICIPANT application lookup column: name=%q, nullable=%q",
				columnName, nullable)), nil
		}
	}

	inspection, invalidSummary, err := inspectSSOSessionParticipantApplicationIndex(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if invalidSummary != "" {
		return invalidSSOSessionParticipantApplicationIndex(invalidSummary), nil
	}
	if inspection.present && inspection.valid {
		return engine.VerificationResult{
			State:   engine.VerificationTarget,
			Summary: "The SSO session participant application lookup index is present and valid",
		}, nil
	}
	if inspection.present {
		return engine.VerificationResult{
			State:   engine.VerificationSource,
			Summary: "The interrupted SSO session participant application lookup index is ready to be rebuilt",
		}, nil
	}
	return engine.VerificationResult{
		State:   engine.VerificationSource,
		Summary: "The SSO session participant application lookup index is ready to be created",
	}, nil
}

func inspectSSOSessionParticipantApplicationIndex(
	ctx context.Context,
	access engine.ReadAccess,
) (ssoSessionParticipantApplicationIndexInspection, string, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent,
		querySSOSessionParticipantApplicationIndex)
	if err != nil {
		return ssoSessionParticipantApplicationIndexInspection{}, "", fmt.Errorf(
			"inspect SSO session participant application index: %w", err)
	}
	if len(rows) == 0 {
		return ssoSessionParticipantApplicationIndexInspection{}, "", nil
	}
	if len(rows) != 1 {
		return ssoSessionParticipantApplicationIndexInspection{}, fmt.Sprintf(
			"Expected at most one SSO session participant application index, found %d", len(rows)), nil
	}
	isUnique, uniqueErr := rows[0].Bool("is_unique")
	isValid, validErr := rows[0].Bool("is_valid")
	isReady, readyErr := rows[0].Bool("is_ready")
	hasPredicate, predicateErr := rows[0].Bool("has_predicate")
	accessMethod, accessMethodErr := rows[0].String("access_method")
	columns, columnsErr := rows[0].String("column_names")
	if err := errorsJoin(uniqueErr, validErr, readyErr, predicateErr, accessMethodErr, columnsErr); err != nil {
		return ssoSessionParticipantApplicationIndexInspection{}, "", err
	}
	if isUnique || hasPredicate || accessMethod != "btree" ||
		columns != ssoSessionParticipantApplicationIndexColumns {
		return ssoSessionParticipantApplicationIndexInspection{}, fmt.Sprintf(
			"Unexpected idx_sso_session_participant_app definition: unique=%t, predicate=%t, method=%q, columns=%q",
			isUnique, hasPredicate, accessMethod, columns), nil
	}
	return ssoSessionParticipantApplicationIndexInspection{
		present: true, valid: isValid && isReady,
	}, "", nil
}

func invalidSSOSessionParticipantApplicationIndex(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{
			Code: "unexpected_sso_session_participant_application_index", Message: summary,
		}},
	}
}
