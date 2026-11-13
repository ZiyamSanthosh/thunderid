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

func TestAddSessionSubjectIndexOperation(t *testing.T) {
	operation, err := NewAddSessionSubjectIndexOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, addSessionSubjectIndexOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)
	online := operation.(*sessionSubjectIndexOperation)
	assert.Contains(t, online.createIndexQuery.PostgresQuery, "CREATE INDEX CONCURRENTLY")
	assert.Contains(t, online.createIndexQuery.PostgresQuery, `ON "SESSION" (DEPLOYMENT_ID, SUBJECT_ID)`)
	assert.Contains(t, online.dropIndexQuery.PostgresQuery, "DROP INDEX CONCURRENTLY")

	targets, err := operation.(engine.OnlineOperation).ResolveTargets(
		context.Background(), sessionAccess{renamedSessionState()}, engine.MigrationSelection{DeploymentIDs: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, []engine.ExecutionTarget{{}}, targets)
	assert.NoError(t, operation.(engine.OnlineOperation).Finalize(context.Background(), engine.OperationExecution{}))
}

func TestAddSessionSubjectIndexVerification(t *testing.T) {
	operation, err := NewAddSessionSubjectIndexOperation()
	require.NoError(t, err)

	absent := renamedSessionState()
	valid := renamedSessionState()
	valid.subjectIndex = subjectIndexRow(false, true, true, false, "btree", sessionSubjectIndexColumns)
	interrupted := renamedSessionState()
	interrupted.subjectIndex = subjectIndexRow(false, false, false, false, "btree", sessionSubjectIndexColumns)
	unique := renamedSessionState()
	unique.subjectIndex = subjectIndexRow(true, true, true, false, "btree", sessionSubjectIndexColumns)
	partial := renamedSessionState()
	partial.subjectIndex = subjectIndexRow(false, true, true, true, "btree", sessionSubjectIndexColumns)
	wrongColumns := renamedSessionState()
	wrongColumns.subjectIndex = subjectIndexRow(false, true, true, false, "btree", "SUBJECT_ID,DEPLOYMENT_ID")
	hash := renamedSessionState()
	hash.subjectIndex = subjectIndexRow(false, true, true, false, "hash", sessionSubjectIndexColumns)
	duplicated := renamedSessionState()
	duplicated.subjectIndex = append(
		subjectIndexRow(false, true, true, false, "btree", sessionSubjectIndexColumns),
		subjectIndexRow(false, true, true, false, "btree", sessionSubjectIndexColumns)...)
	missingColumns := renamedSessionState()
	missingColumns.subjectColumns = nil
	nullableColumn := renamedSessionState()
	nullableColumn.subjectColumns = []engine.QueryRow{
		{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO"},
		{"column_name": "SUBJECT_ID", "is_nullable": "YES"},
	}

	tests := []struct {
		name     string
		state    sessionState
		expected engine.VerificationState
		text     string
	}{
		{"index absent", absent, engine.VerificationSource, "ready to be created"},
		{"index valid", valid, engine.VerificationTarget, "present and valid"},
		{"interrupted build", interrupted, engine.VerificationSource, "ready to be rebuilt"},
		{"unique index", unique, engine.VerificationInvalid, "Unexpected idx_session_subject"},
		{"partial index", partial, engine.VerificationInvalid, "Unexpected idx_session_subject"},
		{"wrong columns", wrongColumns, engine.VerificationInvalid, "Unexpected idx_session_subject"},
		{"wrong access method", hash, engine.VerificationInvalid, "Unexpected idx_session_subject"},
		{"duplicated index rows", duplicated, engine.VerificationInvalid, "at most one"},
		{"missing columns", missingColumns, engine.VerificationInvalid, "found 0"},
		{"nullable column", nullableColumn, engine.VerificationInvalid, "Unexpected SESSION subject lookup column"},
		{"tables not renamed", legacySessionState(), engine.VerificationInvalid, "not renamed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pre, err := operation.PreVerify(context.Background(), sessionAccess{test.state}, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, pre.State)
			assert.Contains(t, pre.Summary, test.text)
			post, err := operation.(engine.OnlineOperation).PostVerify(
				context.Background(), sessionAccess{test.state}, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, pre.State, post.State)
			if test.expected == engine.VerificationInvalid {
				require.Len(t, post.Findings, 1)
				assert.Equal(t, "unexpected_session_subject_index", post.Findings[0].Code)
			}
		})
	}
}

func TestAddSessionSubjectIndexReadiness(t *testing.T) {
	operation, err := NewAddSessionSubjectIndexOperation()
	require.NoError(t, err)
	checker := operation.(*sessionSubjectIndexOperation)

	deferred, err := checker.CheckReadiness(context.Background(), sessionAccess{legacySessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID}, deferred.DeferredUntil)

	ready, err := checker.CheckReadiness(context.Background(), sessionAccess{renamedSessionState()}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	conflicting := renamedSessionState()
	conflicting.subjectIndex = subjectIndexRow(true, true, true, false, "btree", sessionSubjectIndexColumns)
	blocked, err := checker.CheckReadiness(context.Background(), sessionAccess{conflicting}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.NotEmpty(t, blocked.Findings[0].Remediation)

	result, err := checker.CheckReadiness(context.Background(),
		sessionAccess{sessionState{err: errors.New("database unavailable")}}, engine.PlanContext{})
	assert.ErrorContains(t, err, "database unavailable")
	assert.Empty(t, result.State)
}
