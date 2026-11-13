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

func TestAddSessionTypeCheckOperation(t *testing.T) {
	operation, err := NewAddSessionTypeCheckOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, addSessionTypeCheckOperationID, definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Contains(t, implementation.Statements[1].Query, `ADD CONSTRAINT "SESSION_type_check"`)
	assert.Contains(t, implementation.Statements[1].Query, "NOT VALID")
}

func TestAddSessionTypeCheckVerification(t *testing.T) {
	operation, err := NewAddSessionTypeCheckOperation()
	require.NoError(t, err)

	source := renamedSessionState()
	notValidated := renamedSessionState()
	notValidated.checks = sessionTypeCheckRows(false, sessionTypeCheckDefinition)
	validated := renamedSessionState()
	validated.checks = sessionTypeCheckRows(true, sessionTypeCheckDefinition)
	wrongDefinition := renamedSessionState()
	wrongDefinition.checks = sessionTypeCheckRows(true, `CHECK (type IN ('SSO'))`)
	badColumn := renamedSessionState()
	badColumn.typeColumn = nil

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"source", source, engine.VerificationSource, "ready to be added"},
		{"added not validated", notValidated, engine.VerificationTarget, "is present"},
		{"validated", validated, engine.VerificationTarget, "is present"},
		{"wrong definition", wrongDefinition, engine.VerificationInvalid, "SSO and DIRECT"},
		{"missing type column", badColumn, engine.VerificationInvalid, "SESSION.TYPE"},
		{"tables not renamed", legacySessionState(), engine.VerificationInvalid, "not renamed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pre, err := operation.PreVerify(context.Background(), sessionAccess{test.state}, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, pre.State)
			assert.Contains(t, pre.Summary, test.text)
		})
	}
}

func TestAddSessionTypeCheckReadiness(t *testing.T) {
	operation, err := NewAddSessionTypeCheckOperation()
	require.NoError(t, err)
	checker := operation.(*engine.SQLOperation)

	deferred, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID}, deferred.DeferredUntil)

	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	wrong := renamedSessionState()
	wrong.checks = sessionTypeCheckRows(true, `CHECK (type IN ('SSO'))`)
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{wrong}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.NotEmpty(t, blocked.Findings[0].Remediation)
}
