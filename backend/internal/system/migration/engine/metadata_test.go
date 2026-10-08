// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testStateID    = "0199a123-4567-7abc-8def-0123456789ab"
	testRunID      = "0199a123-4568-7abc-8def-0123456789ab"
	testProgressID = "0199a123-4569-7abc-8def-0123456789ab"
)

func TestStateValidate(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*State)
		errorMatch string
	}{
		{name: "valid clean state"},
		{
			name: "valid in-progress state",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusInProgress
				state.TargetLevel = "1.1.0"
				state.ActiveRunID = testRunID
			},
		},
		{
			name: "valid failed state",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusFailed
				state.TargetLevel = "1.1.0"
				state.ActiveRunID = testRunID
				state.FailureSummary = "operation verification failed"
			},
		},
		{
			name: "valid expanded state",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanded
				state.Status = StatusCompleted
				state.TargetLevel = "1.1.0"
			},
		},
		{
			name: "valid contract-ready state",
			mutate: func(state *State) {
				state.Stage = MigrationStageContractReady
				state.Status = StatusCompleted
				state.TargetLevel = "1.1.0"
			},
		},
		{name: "invalid ID", mutate: func(state *State) { state.ID = "invalid" }, errorMatch: "state ID"},
		{name: "unknown store", mutate: func(state *State) { state.StoreID = "unknown" }, errorMatch: "store"},
		{name: "invalid metadata version", mutate: func(state *State) { state.MetadataVersion = 0 }, errorMatch: "metadata version"},
		{name: "invalid completed level", mutate: func(state *State) { state.CompletedLevel = "v1.0.0" }, errorMatch: "completed level"},
		{name: "missing update time", mutate: func(state *State) { state.UpdatedAt = time.Time{} }, errorMatch: "update time"},
		{name: "long failure summary", mutate: func(state *State) { state.FailureSummary = strings.Repeat("x", maxFailureSummaryLength+1) }, errorMatch: "failure summary exceeds"},
		{name: "clean with target", mutate: func(state *State) { state.TargetLevel = "1.1.0" }, errorMatch: "clean migration state"},
		{name: "clean in expanded stage", mutate: func(state *State) {
			state.Stage = MigrationStageExpanded
		}, errorMatch: "complete stage"},
		{name: "completed in executing stage", mutate: func(state *State) {
			state.Stage = MigrationStageExpanding
			state.Status = StatusCompleted
			state.TargetLevel = "1.1.0"
		}, errorMatch: "expanded or contract-ready"},
		{
			name: "active with invalid target",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusInProgress
				state.TargetLevel = "1.1"
				state.ActiveRunID = testRunID
			},
			errorMatch: "target level",
		},
		{
			name: "active without advancement",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusInProgress
				state.TargetLevel = state.CompletedLevel
				state.ActiveRunID = testRunID
			},
			errorMatch: "must differ",
		},
		{
			name: "active with invalid run ID",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusInProgress
				state.TargetLevel = "1.1.0"
				state.ActiveRunID = "invalid"
			},
			errorMatch: "active run ID",
		},
		{
			name: "in-progress with failure",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusInProgress
				state.TargetLevel = "1.1.0"
				state.ActiveRunID = testRunID
				state.FailureSummary = "failed"
			},
			errorMatch: "cannot contain a failure",
		},
		{
			name: "failed without failure",
			mutate: func(state *State) {
				state.Stage = MigrationStageExpanding
				state.Status = StatusFailed
				state.TargetLevel = "1.1.0"
				state.ActiveRunID = testRunID
			},
			errorMatch: "requires a failure",
		},
		{name: "unsupported status", mutate: func(state *State) { state.Status = "unknown" }, errorMatch: "unsupported migration state status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := validState()
			if test.mutate != nil {
				test.mutate(&state)
			}

			err := state.Validate()
			if test.errorMatch == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func TestProgressValidate(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Progress)
		errorMatch string
	}{
		{name: "valid progress"},
		{name: "valid progress without checkpoint", mutate: func(progress *Progress) { progress.CheckpointData = nil }},
		{name: "invalid ID", mutate: func(progress *Progress) { progress.ID = "invalid" }, errorMatch: "progress ID"},
		{name: "invalid state ID", mutate: func(progress *Progress) { progress.MigrationStateID = "invalid" }, errorMatch: "state ID"},
		{name: "invalid run ID", mutate: func(progress *Progress) { progress.RunID = "invalid" }, errorMatch: "run ID"},
		{name: "invalid target level", mutate: func(progress *Progress) { progress.TargetLevel = "1.1" }, errorMatch: "target level"},
		{name: "missing operation ID", mutate: func(progress *Progress) { progress.OperationID = "  " }, errorMatch: "operation ID is required"},
		{name: "long operation ID", mutate: func(progress *Progress) { progress.OperationID = strings.Repeat("x", maxOperationIDLength+1) }, errorMatch: "operation ID exceeds"},
		{name: "negative operation order", mutate: func(progress *Progress) { progress.OperationOrder = -1 }, errorMatch: "operation order"},
		{name: "execution target with whitespace", mutate: func(progress *Progress) {
			progress.ExecutionTargetID = " deployment-a"
		}, errorMatch: "execution target ID"},
		{name: "long execution target", mutate: func(progress *Progress) {
			progress.ExecutionTargetID = strings.Repeat("x", maxExecutionTargetIDLength+1)
		}, errorMatch: "execution target ID exceeds"},
		{name: "clean progress status", mutate: func(progress *Progress) { progress.Status = StatusClean }, errorMatch: "progress status"},
		{name: "oversized checkpoint", mutate: func(progress *Progress) {
			progress.CheckpointData = json.RawMessage(strings.Repeat("x", maxCheckpointSize+1))
		}, errorMatch: "checkpoint exceeds"},
		{name: "invalid checkpoint", mutate: func(progress *Progress) { progress.CheckpointData = json.RawMessage(`{"cursor":`) }, errorMatch: "valid JSON"},
		{name: "missing update time", mutate: func(progress *Progress) { progress.UpdatedAt = time.Time{} }, errorMatch: "update time"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			progress := validProgress()
			if test.mutate != nil {
				test.mutate(&progress)
			}

			err := progress.Validate()
			if test.errorMatch == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func TestDeploymentStateValidate(t *testing.T) {
	state := DeploymentState{
		ID: testProgressID, MigrationStateID: testStateID, DeploymentID: "deployment-a",
		CompletedLevel: "2.0.0", UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, state.Validate())

	state.DeploymentID = " "
	require.Error(t, state.Validate())
}

func TestIsUUIDv7(t *testing.T) {
	assert.True(t, isUUIDv7(testStateID))
	assert.False(t, isUUIDv7("0199A123-4567-7ABC-8DEF-0123456789AB"))
	assert.False(t, isUUIDv7("0199a123-4567-4abc-8def-0123456789ab"))
	assert.False(t, isUUIDv7("0199a123-4567-7abc-0def-0123456789ab"))
}

func validState() State {
	return State{
		ID:              testStateID,
		StoreID:         StoreIDConfigDB,
		MetadataVersion: 1,
		CompletedLevel:  "1.0.0",
		Stage:           MigrationStageComplete,
		Status:          StatusClean,
		UpdatedAt:       time.Now().UTC(),
	}
}

func validProgress() Progress {
	return Progress{
		ID:                testProgressID,
		MigrationStateID:  testStateID,
		RunID:             testRunID,
		TargetLevel:       "1.1.0",
		OperationID:       "1.1.0/add-session-subject",
		OperationOrder:    0,
		ExecutionTargetID: "deployment-a",
		Status:            StatusInProgress,
		CheckpointData:    json.RawMessage(`{"lastID":"0199a123-4567-7abc-8def-0123456789ab"}`),
		UpdatedAt:         time.Now().UTC(),
	}
}
