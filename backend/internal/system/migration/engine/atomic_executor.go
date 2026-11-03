// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/system/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// OperationExecutionResult describes the completed execution of one operation.
type OperationExecutionResult struct {
	Progress         Progress
	ExecutionSkipped bool
	CommittedBatches int
}

// AtomicOperationExecutor runs one operation target and its progress update in one transaction.
type AtomicOperationExecutor struct {
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	stores        map[StoreID]ReadStore
	newID         func() (string, error)
	now           func() time.Time
}

// NewAtomicOperationExecutor creates an executor for one writable database namespace.
func NewAtomicOperationExecutor(
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
	stores map[StoreID]ReadStore,
) (*AtomicOperationExecutor, error) {
	if metadataStore == nil {
		return nil, errors.New("create atomic operation executor: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create atomic operation executor: transactioner is required")
	}
	storeCopy := make(map[StoreID]ReadStore, len(stores))
	for storeID, store := range stores {
		storeCopy[storeID] = store
	}
	return &AtomicOperationExecutor{
		metadataStore: metadataStore,
		transactioner: transactioner,
		stores:        storeCopy,
		newID:         utils.GenerateUUIDv7,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// Execute runs one newly reserved atomic operation and records its completed progress.
func (e *AtomicOperationExecutor) Execute(
	ctx context.Context,
	operation AtomicOperation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
) (OperationExecutionResult, error) {
	if operation == nil {
		return OperationExecutionResult{}, errors.New("execute atomic operation: operation is required")
	}
	if !isUUIDv7(runID) {
		return OperationExecutionResult{}, errors.New("execute atomic operation: run ID must be a canonical UUIDv7")
	}
	if operationOrder < 0 {
		return OperationExecutionResult{}, errors.New("execute atomic operation: operation order cannot be negative")
	}
	definition := operation.Definition()
	if definition.InterruptionMode != InterruptionModeAtomic {
		return OperationExecutionResult{}, fmt.Errorf("execute operation %q: interruption mode must be atomic", definition.ID)
	}

	readAccess, err := newOperationReadAccess(definition, e.stores)
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("execute operation %q: %w", definition.ID, err)
	}
	targets, err := operation.ResolveTargets(ctx, readAccess, selection)
	if err != nil {
		return OperationExecutionResult{}, err
	}
	if err := validateExecutionTargets(definition, targets); err != nil {
		return OperationExecutionResult{}, err
	}
	if len(targets) != 1 {
		return OperationExecutionResult{}, fmt.Errorf(
			"execute operation %q: atomic executor currently requires exactly one execution target", definition.ID)
	}
	target := targets[0]

	progressID, err := e.newID()
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("execute operation %q: generate progress ID: %w", definition.ID, err)
	}

	result := OperationExecutionResult{}
	err = e.transactioner.Transact(ctx, func(txCtx context.Context) error {
		state, err := e.metadataStore.GetState(txCtx, definition.StoreID)
		if err != nil {
			return err
		}
		if err := validateOperationReservation(definition, state, runID); err != nil {
			return err
		}

		executionAccess, err := newOperationExecutionAccess(definition, e.stores)
		if err != nil {
			return fmt.Errorf("execute operation %q: %w", definition.ID, err)
		}
		if definition.Kind == OperationKindSchemaChange {
			existing, err := e.metadataStore.GetProgress(
				txCtx, state.ID, state.TargetLevel, definition.ID, target.DeploymentID)
			if err != nil {
				return err
			}
			if existing != nil {
				if existing.Status != StatusCompleted {
					return fmt.Errorf(
						"execute operation %q: existing complete-store progress has status %q",
						definition.ID, existing.Status)
				}
				if err := verifyOperationTarget(txCtx, operation, executionAccess, target); err != nil {
					return err
				}
				result.Progress = *existing
				result.ExecutionSkipped = true
				return nil
			}
		}
		progress := Progress{
			ID: progressID, MigrationStateID: state.ID, RunID: runID, TargetLevel: state.TargetLevel,
			OperationID: string(definition.ID), OperationOrder: operationOrder,
			ExecutionTargetID: target.DeploymentID,
			Status:            StatusInProgress, UpdatedAt: e.now(),
		}
		if err := e.metadataStore.InsertProgress(txCtx, progress); err != nil {
			return err
		}

		verification, err := operation.PreVerify(txCtx, executionAccess, target)
		if err != nil {
			return err
		}
		if err := verification.validate(); err != nil {
			return fmt.Errorf("execute operation %q target %q: invalid pre-verification result: %w",
				definition.ID, executionTargetName(target), err)
		}
		switch verification.State {
		case VerificationSource:
			if err := operation.Execute(txCtx, newOperationExecution(executionAccess, target)); err != nil {
				return err
			}
		case VerificationTarget:
			result.ExecutionSkipped = true
		case VerificationInvalid:
			return fmt.Errorf("execute operation %q target %q: pre-verification is invalid: %s",
				definition.ID, executionTargetName(target), verification.Summary)
		default:
			return fmt.Errorf("execute operation %q target %q: unsupported pre-verification state %q",
				definition.ID, executionTargetName(target), verification.State)
		}

		verification, err = operation.PostVerify(txCtx, executionAccess, target)
		if err != nil {
			return err
		}
		if err := verification.validate(); err != nil {
			return fmt.Errorf("execute operation %q target %q: invalid post-verification result: %w",
				definition.ID, executionTargetName(target), err)
		}
		if verification.State != VerificationTarget {
			return fmt.Errorf("execute operation %q target %q: post-verification returned %q instead of %q",
				definition.ID, executionTargetName(target), verification.State, VerificationTarget)
		}

		progress.Status = StatusCompleted
		progress.UpdatedAt = e.now()
		if err := e.metadataStore.UpdateProgress(txCtx, progress); err != nil {
			return err
		}
		result.Progress = progress
		return nil
	})
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("execute atomic operation %q: %w", definition.ID, err)
	}
	return result, nil
}

func validateOperationReservation(definition OperationDefinition, state *State, runID string) error {
	if state == nil {
		return fmt.Errorf("execute operation %q: migration state for store %q is missing",
			definition.ID, definition.StoreID)
	}
	if state.Status != StatusInProgress {
		return fmt.Errorf("execute operation %q: store %q status is %q instead of %q",
			definition.ID, definition.StoreID, state.Status, StatusInProgress)
	}
	if state.ActiveRunID != runID {
		return fmt.Errorf("execute operation %q: store %q is reserved by a different run",
			definition.ID, definition.StoreID)
	}
	return nil
}
