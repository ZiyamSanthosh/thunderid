// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestRenameSessionTablesOperation(t *testing.T) {
	operation, err := NewRenameSessionTablesOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, renameSessionTablesOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 7)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Contains(t, implementation.Statements[1].Query, `RENAME TO "SESSION"`)
	assert.Contains(t, implementation.Statements[2].Query, `RENAME TO "SESSION_PARTICIPANT"`)
	assert.Contains(t, implementation.Statements[3].Query, "NOT NULL DEFAULT 'SSO'")
	assert.Contains(t, implementation.Statements[4].Query, `ADD COLUMN OU_ID VARCHAR(36) NOT NULL DEFAULT ''`)
	assert.Contains(t, implementation.Statements[5].Query, `CREATE VIEW "SSO_SESSION"`)
	assert.Contains(t, implementation.Statements[5].Query, "WHERE TYPE = 'SSO' WITH CHECK OPTION")
	assert.NotContains(t, implementation.Statements[5].Query, "SELECT *")
	assert.Contains(t, implementation.Statements[6].Query, `CREATE VIEW "SSO_SESSION_PARTICIPANT"`)
	assert.NotContains(t, implementation.Statements[6].Query, "SELECT *")
	assert.NotContains(t, implementation.Statements[6].Query, "OU_ID")
}

func TestRenameSessionTablesVerification(t *testing.T) {
	operation, err := NewRenameSessionTablesOperation()
	require.NoError(t, err)

	renamed := renamedSessionState()
	wrongPrimaryKey := legacySessionState()
	wrongPrimaryKey.legacyPrimaryKeys[legacySessionTable] = "DEPLOYMENT_ID,SESSION_ID"
	missingParticipantPrimaryKey := legacySessionState()
	delete(missingParticipantPrimaryKey.legacyPrimaryKeys, legacySessionParticipantTable)
	partial := legacySessionState()
	partial.relations[sessionTable] = relationKindTable
	viewOnly := renamedSessionState()
	viewOnly.relations[legacySessionTable] = relationKindTable
	nullableType := renamedSessionState()
	nullableType.typeColumn = sessionTypeColumnRow("YES", "character varying", 10, "yes")
	shortType := renamedSessionState()
	shortType.typeColumn = sessionTypeColumnRow("NO", "character varying", 5, "yes")
	missingType := renamedSessionState()
	missingType.typeColumn = nil
	missingParticipantOU := renamedSessionState()
	missingParticipantOU.participantColumns = nil
	noRelations := sessionState{relations: map[string]string{}}

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"source", legacySessionState(), engine.VerificationSource, "ready to be renamed"},
		{"target", renamed, engine.VerificationTarget, "compatibility views are present"},
		{"wrong primary key", wrongPrimaryKey, engine.VerificationInvalid, "primary key"},
		{"missing participant primary key", missingParticipantPrimaryKey, engine.VerificationInvalid, "primary key"},
		{"half renamed", partial, engine.VerificationInvalid, "Unexpected session relations"},
		{"legacy table beside renamed table", viewOnly, engine.VerificationInvalid, "Unexpected session relations"},
		{"nullable type", nullableType, engine.VerificationInvalid, "SESSION.TYPE"},
		{"short type", shortType, engine.VerificationInvalid, "SESSION.TYPE"},
		{"missing type", missingType, engine.VerificationInvalid, "SESSION.TYPE"},
		{"missing participant organization unit", missingParticipantOU, engine.VerificationInvalid,
			"SESSION_PARTICIPANT.OU_ID"},
		{"no session relations", noRelations, engine.VerificationInvalid, "none"},
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
			if test.expected == engine.VerificationInvalid {
				require.Len(t, post.Findings, 1)
				assert.Equal(t, "unexpected_session_table_schema", post.Findings[0].Code)
			}
		})
	}
}

func TestRenameSessionTablesReadiness(t *testing.T) {
	operation, err := NewRenameSessionTablesOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	already, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, already.State)

	partial := legacySessionState()
	partial.relations[sessionTable] = relationKindTable
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{partial}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.NotEmpty(t, blocked.Findings[0].Remediation)
}

func TestRenameSessionTablesPropagatesInspectionFailure(t *testing.T) {
	operation, err := NewRenameSessionTablesOperation()
	require.NoError(t, err)
	failing := sessionAccess{sessionState{err: errors.New("database unavailable")}}

	_, err = operation.PreVerify(context.Background(), failing, engine.ExecutionTarget{})
	assert.ErrorContains(t, err, "database unavailable")
	result, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(), failing, engine.PlanContext{})
	assert.ErrorContains(t, err, "database unavailable")
	assert.Empty(t, result.State)
}
