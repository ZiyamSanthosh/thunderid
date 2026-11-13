// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	renameSessionTablesOperationID            engine.OperationID = "runtime_persistent/51_rename_session_tables"
	renameSessionIndexesOperationID           engine.OperationID = "runtime_persistent/52_rename_session_indexes"
	addSessionTypeCheckOperationID            engine.OperationID = "runtime_persistent/53_add_session_type_check"
	validateSessionTypeCheckOperationID       engine.OperationID = "runtime_persistent/54_validate_session_type_check"
	addSessionSubjectIndexOperationID         engine.OperationID = "runtime_persistent/55_add_session_subject_index"
	finalizeSessionTablesOperationID          engine.OperationID = "runtime_persistent/56_finalize_session_tables"
	legacySessionTable                                           = "SSO_SESSION"
	legacySessionParticipantTable                                = "SSO_SESSION_PARTICIPANT"
	sessionTable                                                 = "SESSION"
	sessionParticipantTable                                      = "SESSION_PARTICIPANT"
	sessionTypeCheckConstraint                                   = "SESSION_type_check"
	sessionSubjectIndex                                          = "idx_session_subject"
	sessionSubjectIndexColumns                                   = "DEPLOYMENT_ID,SUBJECT_ID"
	sessionPrimaryKeyColumns                                     = "SESSION_ID,DEPLOYMENT_ID"
	sessionParticipantPrimaryKeyColumns                          = "SESSION_ID,DEPLOYMENT_ID,APP_ID"
	sessionParticipantTargetPrimaryKeyColumns                    = "SESSION_ID,DEPLOYMENT_ID,APP_ID,OU_ID"
	relationKindTable                                            = "r"
	relationKindView                                             = "v"
)

var (
	querySessionRelations = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-10",
		PostgresQuery: `SELECT c.relname AS relation_name, c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA()
			AND c.relname IN ('SSO_SESSION', 'SESSION', 'SSO_SESSION_PARTICIPANT', 'SESSION_PARTICIPANT')`,
	}
	querySessionPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-11",
		PostgresQuery: `SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = $1 AND c.contype = 'p'
			GROUP BY c.oid`,
	}
	querySessionTypeColumn = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-12",
		PostgresQuery: `SELECT IS_NULLABLE AS is_nullable, DATA_TYPE AS data_type,
			COALESCE(CHARACTER_MAXIMUM_LENGTH, 0)::BIGINT AS max_length,
			CASE WHEN COLUMN_DEFAULT IS NULL THEN 'no' ELSE 'yes' END AS has_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SESSION' AND UPPER(COLUMN_NAME) = 'TYPE'`,
	}
	querySessionIndexNames = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-13",
		PostgresQuery: `SELECT t.relname AS table_name, idx.relname AS index_name
			FROM pg_index i
			INNER JOIN pg_class idx ON idx.oid = i.indexrelid
			INNER JOIN pg_class t ON t.oid = i.indrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname IN ('SESSION', 'SESSION_PARTICIPANT')
			ORDER BY t.relname, idx.relname`,
	}
	querySessionCheckConstraints = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-14",
		PostgresQuery: `SELECT c.conname AS constraint_name, c.convalidated AS is_validated,
			pg_get_constraintdef(c.oid) AS definition
			FROM pg_constraint c
			INNER JOIN pg_class t ON t.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'SESSION' AND c.contype = 'c'`,
	}
	querySessionSubjectColumns = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-15",
		PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'SESSION'
				AND UPPER(COLUMN_NAME) IN ('DEPLOYMENT_ID', 'SUBJECT_ID')
			ORDER BY ORDINAL_POSITION`,
	}
	querySessionSubjectIndex = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-16",
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
			WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = 'SESSION'
				AND idx.relname = 'idx_session_subject'
			GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready, i.indpred, am.amname`,
	}
	querySessionCleanupReferences = dbmodel.DBQuery{
		ID: "MIG-RTP_MGT-17",
		PostgresQuery: `SELECT p.proname AS routine_name
			FROM pg_proc p
			INNER JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = CURRENT_SCHEMA()
			AND (p.prosrc LIKE '%"SSO_SESSION"%' OR p.prosrc LIKE '%"SSO_SESSION_PARTICIPANT"%')
			ORDER BY p.proname`,
	}
)

var checkConstraintLiteralPattern = regexp.MustCompile(`'([^']*)'`)

func sessionVerification(state engine.VerificationState, summary string) engine.VerificationResult {
	return engine.VerificationResult{State: state, Summary: summary}
}

func invalidSessionVerification(code string, summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: code, Message: summary}},
	}
}

// sessionReadiness turns a verification into readiness. An invalid state blocks the operation with the
// supplied remediation. Every other state is ready, because both source and target are safe to run.
func sessionReadiness(verification engine.VerificationResult, remediation string) engine.ReadinessResult {
	if verification.State != engine.VerificationInvalid {
		return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}
	}
	findings := make([]engine.Finding, 0, len(verification.Findings))
	for _, finding := range verification.Findings {
		finding.Remediation = remediation
		findings = append(findings, finding)
	}
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: verification.Summary, Findings: findings,
	}
}

func deferredSessionReadiness(summary string, until ...engine.OperationID) engine.ReadinessResult {
	return engine.ReadinessResult{
		State: engine.ReadinessDeferred, Summary: summary, DeferredUntil: until,
	}
}

func inspectSessionRelations(ctx context.Context, access engine.ReadAccess) (map[string]string, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionRelations)
	if err != nil {
		return nil, fmt.Errorf("inspect session relations: %w", err)
	}
	relations := make(map[string]string, len(rows))
	for _, row := range rows {
		name, nameErr := row.String("relation_name")
		kind, kindErr := row.String("relation_kind")
		if err := errorsJoin(nameErr, kindErr); err != nil {
			return nil, fmt.Errorf("inspect session relations: %w", err)
		}
		relations[name] = kind
	}
	return relations, nil
}

// sessionTablesRenamed reports whether the session tables are already named SESSION and
// SESSION_PARTICIPANT, which is what every operation after the rename needs.
func sessionTablesRenamed(relations map[string]string) bool {
	return relations[sessionTable] == relationKindTable && relations[sessionParticipantTable] == relationKindTable
}

func describeSessionRelations(relations map[string]string) string {
	names := make([]string, 0, len(relations))
	for name := range relations {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", name, relations[name]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func inspectSessionPrimaryKey(
	ctx context.Context,
	access engine.ReadAccess,
	table string,
) (string, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionPrimaryKey, table)
	if err != nil {
		return "", fmt.Errorf("inspect %s primary key: %w", table, err)
	}
	if len(rows) != 1 {
		return "", nil
	}
	columns, err := rows[0].String("column_names")
	if err != nil {
		return "", fmt.Errorf("inspect %s primary key columns: %w", table, err)
	}
	return columns, nil
}

type sessionTypeColumn struct {
	present    bool
	nullable   bool
	dataType   string
	maxLength  int64
	hasDefault bool
}

func inspectSessionTypeColumn(ctx context.Context, access engine.ReadAccess) (sessionTypeColumn, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionTypeColumn)
	if err != nil {
		return sessionTypeColumn{}, fmt.Errorf("inspect SESSION.TYPE column: %w", err)
	}
	if len(rows) == 0 {
		return sessionTypeColumn{}, nil
	}
	nullable, nullableErr := rows[0].String("is_nullable")
	dataType, dataTypeErr := rows[0].String("data_type")
	maxLength, maxLengthErr := rows[0].Int64("max_length")
	hasDefault, hasDefaultErr := rows[0].String("has_default")
	if err := errorsJoin(nullableErr, dataTypeErr, maxLengthErr, hasDefaultErr); err != nil {
		return sessionTypeColumn{}, fmt.Errorf("inspect SESSION.TYPE column: %w", err)
	}
	return sessionTypeColumn{
		present: true, nullable: nullable == "YES", dataType: dataType, maxLength: maxLength,
		hasDefault: hasDefault == "yes",
	}, nil
}

// matchesSessionType reports whether the column has the exact shape the session type needs.
func (c sessionTypeColumn) matchesSessionType() bool {
	return c.present && !c.nullable && c.dataType == "character varying" && c.maxLength == 10
}

type sessionTypeCheck struct {
	present   bool
	validated bool
	valid     bool
}

func inspectSessionTypeCheck(ctx context.Context, access engine.ReadAccess) (sessionTypeCheck, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionCheckConstraints)
	if err != nil {
		return sessionTypeCheck{}, fmt.Errorf("inspect SESSION check constraints: %w", err)
	}
	for _, row := range rows {
		name, nameErr := row.String("constraint_name")
		validated, validatedErr := row.Bool("is_validated")
		definition, definitionErr := row.String("definition")
		if err := errorsJoin(nameErr, validatedErr, definitionErr); err != nil {
			return sessionTypeCheck{}, fmt.Errorf("inspect SESSION check constraints: %w", err)
		}
		if name != sessionTypeCheckConstraint {
			continue
		}
		return sessionTypeCheck{
			present: true, validated: validated, valid: isSessionTypeCheckDefinition(definition),
		}, nil
	}
	return sessionTypeCheck{}, nil
}

// isSessionTypeCheckDefinition accepts a check that restricts TYPE to exactly SSO and DIRECT, whatever
// formatting the database version uses for the expression.
func isSessionTypeCheckDefinition(definition string) bool {
	if !strings.Contains(strings.ToLower(definition), "type") {
		return false
	}
	literals := checkConstraintLiteralPattern.FindAllStringSubmatch(definition, -1)
	values := make(map[string]bool, len(literals))
	for _, literal := range literals {
		values[literal[1]] = true
	}
	return len(values) == 2 && values["SSO"] && values["DIRECT"]
}

// sessionParticipantOUColumn is the organization unit a participant signed in under. It defaults to the empty
// string, which means the sign-in had none, so the previous release's inserts through the view stay valid.
var sessionParticipantOUColumn = columnSpec{"ou_id", "NO", "character varying", "36", emptyStringDefault}

func inspectSessionParticipantOUColumn(ctx context.Context, access engine.ReadAccess) (columnSpec, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, queryTableColumns, sessionParticipantTable)
	if err != nil {
		return columnSpec{}, fmt.Errorf("inspect SESSION_PARTICIPANT columns: %w", err)
	}
	for _, row := range rows {
		column, err := readColumn(row)
		if err != nil {
			return columnSpec{}, err
		}
		if column.name == "ou_id" {
			return column, nil
		}
	}
	return columnSpec{}, nil
}
