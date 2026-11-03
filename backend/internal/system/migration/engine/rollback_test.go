// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedOperationCandidatesSelectsRecordedOperationTarget(t *testing.T) {
	forwardID := OperationID("2.0.0/configdb/forward")
	rollbackOperation := rollbackTestOperation{
		resumableTestOperation: resumableTestOperation{testOperation: testOperation{definition: OperationDefinition{
			ID: "2.0.0/configdb/rollback", StoreID: StoreIDConfigDB,
			InterruptionMode: InterruptionModeResumable,
		}}},
		forwardID: forwardID,
	}
	candidates := []rollbackCandidate{
		{operation: rollbackOperation, forwardProgress: Progress{
			OperationID: string(forwardID), ExecutionTargetID: "deployment-a", Status: StatusCompleted,
		}},
		{operation: rollbackOperation, forwardProgress: Progress{
			OperationID: string(forwardID), ExecutionTargetID: "deployment-b", Status: StatusInProgress,
		}},
	}

	selected, err := failedOperationCandidates(migrationRollbackContext{
		candidates: candidates,
		failure: &MigrationFailure{
			ForwardOperationID: forwardID, ExecutionTargetID: "deployment-b",
		},
	})

	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "deployment-b", selected[0].forwardProgress.ExecutionTargetID)
}

func TestFailedOperationCandidatesFallsBackToSingleInProgressOperation(t *testing.T) {
	forwardID := OperationID("2.0.0/configdb/forward")
	rollbackOperation := rollbackTestOperation{
		resumableTestOperation: resumableTestOperation{testOperation: testOperation{definition: OperationDefinition{
			ID: "2.0.0/configdb/rollback", StoreID: StoreIDConfigDB,
			InterruptionMode: InterruptionModeResumable,
		}}},
		forwardID: forwardID,
	}

	selected, err := failedOperationCandidates(migrationRollbackContext{candidates: []rollbackCandidate{
		{operation: rollbackOperation, forwardProgress: Progress{Status: StatusCompleted}},
		{operation: rollbackOperation, forwardProgress: Progress{
			ExecutionTargetID: "deployment-a", Status: StatusInProgress,
		}},
	}})

	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "deployment-a", selected[0].forwardProgress.ExecutionTargetID)
}

func TestFailedOperationCandidatesRejectsAmbiguousFallback(t *testing.T) {
	rollback := migrationRollbackContext{candidates: []rollbackCandidate{
		{forwardProgress: Progress{ExecutionTargetID: "deployment-a", Status: StatusInProgress}},
		{forwardProgress: Progress{ExecutionTargetID: "deployment-b", Status: StatusInProgress}},
	}}

	selected, err := failedOperationCandidates(rollback)

	require.Error(t, err)
	assert.Nil(t, selected)
	assert.ErrorContains(t, err, "ambiguous")
}

func TestFailedOperationCandidatesSkipsRolledBackFailure(t *testing.T) {
	selected, err := failedOperationCandidates(migrationRollbackContext{
		candidates: []rollbackCandidate{{forwardProgress: Progress{Status: StatusInProgress}}},
		failure:    &MigrationFailure{RolledBack: true},
	})

	require.ErrorIs(t, err, ErrRollbackAlreadyCompleted)
	assert.Empty(t, selected)
}

func TestFailedOperationCandidatesReportsUnavailableRollback(t *testing.T) {
	selected, err := failedOperationCandidates(migrationRollbackContext{
		failure: &MigrationFailure{ForwardOperationID: "2.0.0/configdb/atomic"},
	})

	require.ErrorIs(t, err, ErrRollbackUnavailable)
	assert.Empty(t, selected)
}

func TestSortRollbackCandidatesUsesReverseOperationOrder(t *testing.T) {
	candidates := []rollbackCandidate{
		{operationOrder: 2, forwardProgress: Progress{ExecutionTargetID: "deployment-a"}},
		{operationOrder: 1, forwardProgress: Progress{ExecutionTargetID: "deployment-c"}},
		{operationOrder: 2, forwardProgress: Progress{ExecutionTargetID: "deployment-b"}},
	}

	sortRollbackCandidates(candidates)

	assert.Equal(t, []int{2, 2, 1}, []int{
		candidates[0].operationOrder,
		candidates[1].operationOrder,
		candidates[2].operationOrder,
	})
	assert.Equal(t, []string{"deployment-b", "deployment-a", "deployment-c"}, []string{
		candidates[0].forwardProgress.ExecutionTargetID,
		candidates[1].forwardProgress.ExecutionTargetID,
		candidates[2].forwardProgress.ExecutionTargetID,
	})
}
