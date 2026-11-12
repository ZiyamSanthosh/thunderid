// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestRenameSessionIndexesOperation(t *testing.T) {
	operation, err := newRenameSessionIndexesOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, renameSessionIndexesOperationID, definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 7)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Equal(t, `ALTER INDEX "SSO_SESSION_pkey" RENAME TO "SESSION_pkey"`, implementation.Statements[1].Query)
	assert.Equal(t, `ALTER INDEX "idx_sso_session_participant_app" RENAME TO "idx_session_participant_app"`,
		implementation.Statements[6].Query)
}

func TestRenameSessionIndexesVerification(t *testing.T) {
	operation, err := newRenameSessionIndexesOperation()
	require.NoError(t, err)

	source := renamedSessionState()
	target := renamedSessionState()
	target.indexes = sessionIndexRows(renamedIndexNames...)
	partial := renamedSessionState()
	partial.indexes = sessionIndexRows(append([]string{"SESSION_pkey"}, legacyIndexNames[1:]...)...)
	missing := renamedSessionState()
	missing.indexes = sessionIndexRows(legacyIndexNames[:5]...)
	both := renamedSessionState()
	both.indexes = sessionIndexRows(append(append([]string{}, legacyIndexNames...), "idx_session_handle_id")...)
	notRenamed := legacySessionState()

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"source", source, engine.VerificationSource, "ready to be renamed"},
		{"target", target, engine.VerificationTarget, "are renamed"},
		{"partly renamed", partial, engine.VerificationInvalid, "partly renamed"},
		{"missing index", missing, engine.VerificationInvalid, "idx_sso_session_participant_app"},
		{"old and new name together", both, engine.VerificationInvalid, "idx_sso_session_handle_id"},
		{"tables not renamed", notRenamed, engine.VerificationInvalid, "not renamed"},
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

func TestRenameSessionIndexesReadiness(t *testing.T) {
	operation, err := newRenameSessionIndexesOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	deferred, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID}, deferred.DeferredUntil)

	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	missing := renamedSessionState()
	missing.indexes = sessionIndexRows(legacyIndexNames[:5]...)
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{missing}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.NotEmpty(t, blocked.Findings[0].Remediation)
}
