// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// The rename, the TYPE and OU_ID columns and both compatibility views are created in one transaction, so the 2.0.0
// binary, which still queries SSO_SESSION and SSO_SESSION_PARTICIPANT, never sees a missing relation.
// The views list the columns the 2.0.0 binary knows and expose only SSO sessions, and TYPE keeps its
// default until contract so the 2.0.0 insert, which does not write it, still produces an SSO session.
const (
	renameSessionStatement            = `ALTER TABLE "SSO_SESSION" RENAME TO "SESSION"`
	renameSessionParticipantStatement = `ALTER TABLE "SSO_SESSION_PARTICIPANT" RENAME TO "SESSION_PARTICIPANT"`
	addSessionTypeStatement           = `ALTER TABLE "SESSION" ADD COLUMN TYPE VARCHAR(10) NOT NULL DEFAULT 'SSO'`
	addSessionParticipantOUStatement  = `ALTER TABLE "SESSION_PARTICIPANT"
		ADD COLUMN OU_ID VARCHAR(36) NOT NULL DEFAULT ''`
	createLegacySessionViewStatement = `CREATE VIEW "SSO_SESSION" AS
		SELECT SESSION_ID, DEPLOYMENT_ID, SUBJECT_ID, FLOW_ID, FLOW_VERSION, FLOW_EXECUTION_ID, HANDLE_ID,
			AUTHENTICATED_AT, CREATED_AT, LAST_ACTIVE_AT, IDLE_EXPIRES_AT, ABSOLUTE_EXPIRES_AT, STATE, VERSION,
			UPDATED_AT
		FROM "SESSION" WHERE TYPE = 'SSO' WITH CHECK OPTION`
	createLegacySessionParticipantViewStatement = `CREATE VIEW "SSO_SESSION_PARTICIPANT" AS
		SELECT SESSION_ID, DEPLOYMENT_ID, APP_ID, TFID, FIRST_JOINED_AT, LAST_ACTIVE_AT
		FROM "SESSION_PARTICIPANT"`
)

func newRenameSessionTablesOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               renameSessionTablesOperationID,
			Description:      "Rename the SSO session tables and keep compatibility views for the previous binary",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkRenameSessionTablesReadiness,
		PreVerifier:      verifyRenameSessionTables,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "rename-session", Query: renameSessionStatement},
				{ID: "rename-session-participant", Query: renameSessionParticipantStatement},
				{ID: "add-session-type", Query: addSessionTypeStatement},
				{ID: "add-session-participant-ou", Query: addSessionParticipantOUStatement},
				{ID: "create-legacy-session-view", Query: createLegacySessionViewStatement},
				{ID: "create-legacy-session-participant-view", Query: createLegacySessionParticipantViewStatement},
			}},
		},
		PostVerifier: verifyRenameSessionTables,
	})
}

func checkRenameSessionTablesReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := verifyRenameSessionTables(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	return sessionReadiness(verification,
		"Restore the expected SSO_SESSION and SSO_SESSION_PARTICIPANT source schema, or reconcile the "+
			"SESSION, SESSION_PARTICIPANT and compatibility view definitions, before retrying"), nil
}

func verifyRenameSessionTables(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	const code = "unexpected_session_table_schema"
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	legacySession := relations[legacySessionTable]
	legacyParticipant := relations[legacySessionParticipantTable]
	session := relations[sessionTable]
	participant := relations[sessionParticipantTable]

	switch {
	case legacySession == relationKindTable && legacyParticipant == relationKindTable &&
		session == "" && participant == "":
		return verifyLegacySessionPrimaryKeys(ctx, access, code)
	case session == relationKindTable && participant == relationKindTable &&
		legacySession == relationKindView && legacyParticipant == relationKindView:
		column, err := inspectSessionTypeColumn(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if !column.matchesSessionType() {
			return invalidSessionVerification(code, fmt.Sprintf(
				"SESSION.TYPE has an unexpected definition: present=%t, nullable=%t, type=%q, length=%d",
				column.present, column.nullable, column.dataType, column.maxLength)), nil
		}
		participantOU, err := inspectSessionParticipantOUColumn(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if participantOU != sessionParticipantOUColumn {
			return invalidSessionVerification(code, fmt.Sprintf(
				"SESSION_PARTICIPANT.OU_ID has an unexpected definition: %+v", participantOU)), nil
		}
		return sessionVerification(engine.VerificationTarget,
			"The session tables are renamed and the compatibility views are present"), nil
	default:
		return invalidSessionVerification(code, fmt.Sprintf(
			"Unexpected session relations: %s", describeSessionRelations(relations))), nil
	}
}

func verifyLegacySessionPrimaryKeys(
	ctx context.Context,
	access engine.ReadAccess,
	code string,
) (engine.VerificationResult, error) {
	expected := map[string]string{
		legacySessionTable:            sessionPrimaryKeyColumns,
		legacySessionParticipantTable: sessionParticipantPrimaryKeyColumns,
	}
	for _, table := range []string{legacySessionTable, legacySessionParticipantTable} {
		columns, err := inspectSessionPrimaryKey(ctx, access, table)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if columns != expected[table] {
			return invalidSessionVerification(code, fmt.Sprintf(
				"Unexpected %s primary key columns %q", table, columns)), nil
		}
	}
	return sessionVerification(engine.VerificationSource,
		"The SSO session tables are ready to be renamed"), nil
}
