// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"strings"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// This contract step removes what only the 2.0.0 binary needs: the compatibility views and the TYPE
// default. It has no rollback operation. Once the views are gone the 2.0.0 binary cannot run, so
// completing it blocks a complete rollback by design.
func newFinalizeSessionTablesOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               finalizeSessionTablesOperationID,
			Description:      "Remove the session compatibility views and the session type default",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkFinalizeSessionTablesReadiness,
		PreVerifier:      verifyFinalizeSessionTables,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "drop-legacy-session-view", Query: `DROP VIEW "SSO_SESSION"`},
				{ID: "drop-legacy-session-participant-view", Query: `DROP VIEW "SSO_SESSION_PARTICIPANT"`},
				{ID: "drop-session-type-default", Query: `ALTER TABLE "SESSION" ALTER COLUMN TYPE DROP DEFAULT`},
				{ID: "drop-session-participant-primary-key", Query: `ALTER TABLE "SESSION_PARTICIPANT"
    DROP CONSTRAINT "SESSION_PARTICIPANT_pkey"`},
				{ID: "add-session-participant-primary-key", Query: `ALTER TABLE "SESSION_PARTICIPANT"
    ADD PRIMARY KEY (SESSION_ID, DEPLOYMENT_ID, APP_ID, OU_ID)`},
			}},
		},
		PostVerifier: verifyFinalizeSessionTables,
	})
}

func checkFinalizeSessionTablesReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !sessionTablesRenamed(relations) && relations[legacySessionTable] == relationKindTable {
		return deferredSessionReadiness(
			"Removing the compatibility views depends on the session table rename",
			renameSessionTablesOperationID), nil
	}
	verification, err := verifyFinalizeSessionTables(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State != engine.VerificationSource {
		return sessionReadiness(verification,
			"Reconcile the SSO_SESSION and SSO_SESSION_PARTICIPANT compatibility views and the SESSION.TYPE "+
				"default before retrying"), nil
	}
	// A cleanup routine that still reads the legacy names would fail once the views are dropped.
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionCleanupReferences)
	if err != nil {
		return engine.ReadinessResult{}, fmt.Errorf("inspect routines that reference the legacy session views: %w", err)
	}
	if len(rows) == 0 {
		return sessionReadiness(verification, ""), nil
	}
	routines := make([]string, 0, len(rows))
	for _, row := range rows {
		name, err := row.String("routine_name")
		if err != nil {
			return engine.ReadinessResult{}, fmt.Errorf("inspect routines that reference the legacy session views: %w", err)
		}
		routines = append(routines, name)
	}
	summary := fmt.Sprintf("Routines still reference the legacy session views: %s", strings.Join(routines, ", "))
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "session_cleanup_routine_references_legacy_views", Message: summary,
			Remediation: "Reload backend/dbscripts/runtime_persistent/postgres-cleanup.sql from the 2.0.0-m1.1 " +
				"release so it deletes from SESSION and SESSION_PARTICIPANT, then retry",
		}},
	}, nil
}

func verifyFinalizeSessionTables(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	const code = "unexpected_session_compatibility_views"
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !sessionTablesRenamed(relations) {
		return invalidSessionVerification(code, fmt.Sprintf(
			"The session tables are not renamed: %s", describeSessionRelations(relations))), nil
	}
	column, err := inspectSessionTypeColumn(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !column.matchesSessionType() {
		return invalidSessionVerification(code, fmt.Sprintf(
			"SESSION.TYPE has an unexpected definition: present=%t, nullable=%t, type=%q, length=%d",
			column.present, column.nullable, column.dataType, column.maxLength)), nil
	}
	participantKey, err := inspectSessionPrimaryKey(ctx, access, sessionParticipantTable)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	legacySession := relations[legacySessionTable]
	legacyParticipant := relations[legacySessionParticipantTable]
	switch {
	case legacySession == relationKindView && legacyParticipant == relationKindView && column.hasDefault &&
		participantKey == sessionParticipantPrimaryKeyColumns:
		return sessionVerification(engine.VerificationSource,
			"The session compatibility views and type default are ready to be removed"), nil
	case legacySession == "" && legacyParticipant == "" && !column.hasDefault &&
		participantKey == sessionParticipantTargetPrimaryKeyColumns:
		return sessionVerification(engine.VerificationTarget,
			"The session compatibility views and type default are removed"), nil
	default:
		return invalidSessionVerification(code, fmt.Sprintf(
			"Unexpected session compatibility state: %s, TYPE default=%t, participant key=%q",
			describeSessionRelations(relations), column.hasDefault, participantKey)), nil
	}
}
