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

func TestValidateSessionTypeCheckOperation(t *testing.T) {
	operation, err := NewValidateSessionTypeCheckOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, validateSessionTypeCheckOperationID, definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	assert.Equal(t, `ALTER TABLE "SESSION" VALIDATE CONSTRAINT "SESSION_type_check"`,
		implementation.Statements[0].Query)
}

func TestValidateSessionTypeCheckVerification(t *testing.T) {
	operation, err := NewValidateSessionTypeCheckOperation()
	require.NoError(t, err)

	notValidated := renamedSessionState()
	notValidated.checks = sessionTypeCheckRows(false, sessionTypeCheckDefinition)
	validated := renamedSessionState()
	validated.checks = sessionTypeCheckRows(true, sessionTypeCheckDefinition)
	wrongDefinition := renamedSessionState()
	wrongDefinition.checks = sessionTypeCheckRows(false, `CHECK (type IN ('SSO'))`)

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"not validated", notValidated, engine.VerificationSource, "ready to be validated"},
		{"validated", validated, engine.VerificationTarget, "is validated"},
		{"check absent", renamedSessionState(), engine.VerificationInvalid, "absent"},
		{"wrong definition", wrongDefinition, engine.VerificationInvalid, "SSO and DIRECT"},
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

func TestValidateSessionTypeCheckReadiness(t *testing.T) {
	operation, err := NewValidateSessionTypeCheckOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	beforeRename, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, beforeRename.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID, addSessionTypeCheckOperationID},
		beforeRename.DeferredUntil)

	beforeCheck, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, beforeCheck.State)
	assert.Equal(t, []engine.OperationID{addSessionTypeCheckOperationID}, beforeCheck.DeferredUntil)

	added := renamedSessionState()
	added.checks = sessionTypeCheckRows(false, sessionTypeCheckDefinition)
	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{added}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	wrong := renamedSessionState()
	wrong.checks = sessionTypeCheckRows(false, `CHECK (type IN ('SSO'))`)
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{wrong}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
}
