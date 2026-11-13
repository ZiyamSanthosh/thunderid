// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func finalizedSessionState() sessionState {
	state := renamedSessionState()
	delete(state.relations, legacySessionTable)
	delete(state.relations, legacySessionParticipantTable)
	state.typeColumn = sessionTypeColumnRow("NO", "character varying", 10, "no")
	state.legacyPrimaryKeys = map[string]string{sessionParticipantTable: sessionParticipantTargetPrimaryKeyColumns}
	return state
}

func TestFinalizeSessionTablesOperation(t *testing.T) {
	operation, err := NewFinalizeSessionTablesOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, finalizeSessionTablesOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 6)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Equal(t, `DROP VIEW "SSO_SESSION"`, implementation.Statements[1].Query)
	assert.Equal(t, `DROP VIEW "SSO_SESSION_PARTICIPANT"`, implementation.Statements[2].Query)
	assert.Equal(t, `ALTER TABLE "SESSION" ALTER COLUMN TYPE DROP DEFAULT`, implementation.Statements[3].Query)
	assert.Contains(t, implementation.Statements[4].Query, `DROP CONSTRAINT "SESSION_PARTICIPANT_pkey"`)
	assert.Contains(t, implementation.Statements[5].Query, "PRIMARY KEY (SESSION_ID, DEPLOYMENT_ID, APP_ID, OU_ID)")
}

func TestFinalizeSessionTablesVerification(t *testing.T) {
	operation, err := NewFinalizeSessionTablesOperation()
	require.NoError(t, err)

	viewsDroppedDefaultKept := finalizedSessionState()
	viewsDroppedDefaultKept.typeColumn = sessionTypeColumnRow("NO", "character varying", 10, "yes")
	defaultDroppedViewsKept := renamedSessionState()
	defaultDroppedViewsKept.typeColumn = sessionTypeColumnRow("NO", "character varying", 10, "no")
	keyWidenedViewsKept := renamedSessionState()
	keyWidenedViewsKept.legacyPrimaryKeys = map[string]string{
		sessionParticipantTable: sessionParticipantTargetPrimaryKeyColumns,
	}
	badType := renamedSessionState()
	badType.typeColumn = sessionTypeColumnRow("YES", "character varying", 10, "yes")

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"source", renamedSessionState(), engine.VerificationSource, "ready to be removed"},
		{"target", finalizedSessionState(), engine.VerificationTarget, "are removed"},
		{"views dropped, default kept", viewsDroppedDefaultKept, engine.VerificationInvalid, "Unexpected session compatibility state"},
		{"default dropped, views kept", defaultDroppedViewsKept, engine.VerificationInvalid, "Unexpected session compatibility state"},
		{"key widened, views kept", keyWidenedViewsKept, engine.VerificationInvalid,
			"Unexpected session compatibility state"},
		{"bad type column", badType, engine.VerificationInvalid, "SESSION.TYPE"},
		{"tables not renamed", legacySessionState(), engine.VerificationInvalid, "not renamed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pre, err := operation.PreVerify(context.Background(), sessionAccess{test.state}, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, pre.State)
			assert.Contains(t, pre.Summary, test.text)
			post, err := operation.PostVerify(context.Background(), sessionAccess{test.state}, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, pre.State, post.State)
		})
	}
}

func TestFinalizeSessionTablesReadiness(t *testing.T) {
	operation, err := NewFinalizeSessionTablesOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	deferred, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID}, deferred.DeferredUntil)

	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	done, err := checker.CheckReadiness(context.Background(), sessionAccess{finalizedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, done.State)

	stale := renamedSessionState()
	stale.cleanupReferences = []engine.QueryRow{{"routine_name": "cleanup_expired_runtime_persistent_data"}}
	blockedByRoutine, err := checker.CheckReadiness(context.Background(), sessionAccess{stale}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blockedByRoutine.State)
	assert.Contains(t, blockedByRoutine.Summary, "cleanup_expired_runtime_persistent_data")
	require.Len(t, blockedByRoutine.Findings, 1)
	assert.Equal(t, "session_cleanup_routine_references_legacy_views", blockedByRoutine.Findings[0].Code)
	assert.Contains(t, blockedByRoutine.Findings[0].Remediation, "postgres-cleanup.sql")

	broken := renamedSessionState()
	broken.typeColumn = sessionTypeColumnRow("NO", "character varying", 10, "no")
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{broken}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.NotEmpty(t, blocked.Findings[0].Remediation)
}
