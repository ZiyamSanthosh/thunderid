// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/system/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// ResumableOperationExecutor runs one operation as bounded, independently committed batches.
type ResumableOperationExecutor struct {
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	stores        map[StoreID]ReadStore
	newID         func() (string, error)
	now           func() time.Time
}

// NewResumableOperationExecutor creates an executor for one writable database namespace.
func NewResumableOperationExecutor(
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
	stores map[StoreID]ReadStore,
) (*ResumableOperationExecutor, error) {
	if metadataStore == nil {
		return nil, errors.New("create resumable operation executor: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create resumable operation executor: transactioner is required")
	}
	storeCopy := make(map[StoreID]ReadStore, len(stores))
	for storeID, store := range stores {
		storeCopy[storeID] = store
	}
	return &ResumableOperationExecutor{
		metadataStore: metadataStore,
		transactioner: transactioner,
		stores:        storeCopy,
		newID:         utils.GenerateUUIDv7,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// Execute initializes and runs one newly reserved resumable operation.
func (e *ResumableOperationExecutor) Execute(
	ctx context.Context,
	operation ResumableOperation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
) (OperationExecutionResult, error) {
	definition, target, err := e.validateExecution(ctx, operation, selection, runID, operationOrder)
	if err != nil {
		return OperationExecutionResult{}, err
	}
	progressID, err := e.newID()
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf(
			"execute operation %q: generate progress ID: %w", definition.ID, err)
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
		access, err := newOperationExecutionAccess(definition, e.stores)
		if err != nil {
			return fmt.Errorf("execute operation %q: %w", definition.ID, err)
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
		verification, err := operation.PreVerify(txCtx, access, target)
		if err != nil {
			return err
		}
		if err := verification.validate(); err != nil {
			return fmt.Errorf("execute operation %q target %q: invalid pre-verification result: %w",
				definition.ID, executionTargetName(target), err)
		}
		switch verification.State {
		case VerificationSource:
			result.Progress = progress
			return nil
		case VerificationTarget:
			if err := verifyOperationTarget(txCtx, operation, access, target); err != nil {
				return err
			}
			progress.Status = StatusCompleted
			progress.UpdatedAt = e.now()
			if err := e.metadataStore.UpdateProgress(txCtx, progress); err != nil {
				return err
			}
			result.Progress = progress
			result.ExecutionSkipped = true
			return nil
		case VerificationInvalid:
			return fmt.Errorf("execute operation %q target %q: pre-verification is invalid: %s",
				definition.ID, executionTargetName(target), verification.Summary)
		default:
			return fmt.Errorf("execute operation %q target %q: unsupported pre-verification state %q",
				definition.ID, executionTargetName(target), verification.State)
		}
	})
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("initialize resumable operation %q: %w", definition.ID, err)
	}
	if result.ExecutionSkipped {
		return result, nil
	}
	return e.executeBatches(ctx, operation, target, result.Progress)
}

// Resume continues a resumable operation from its last committed checkpoint.
func (e *ResumableOperationExecutor) Resume(
	ctx context.Context,
	operation ResumableOperation,
	selection MigrationSelection,
	progress Progress,
) (OperationExecutionResult, error) {
	definition, target, err := e.validateExecution(
		ctx, operation, selection, progress.RunID, progress.OperationOrder)
	if err != nil {
		return OperationExecutionResult{}, err
	}
	if err := progress.Validate(); err != nil {
		return OperationExecutionResult{}, fmt.Errorf("resume operation %q: invalid progress: %w", definition.ID, err)
	}
	if progress.OperationID != string(definition.ID) || progress.Status != StatusInProgress {
		return OperationExecutionResult{}, fmt.Errorf(
			"resume operation %q: progress does not identify an in-progress execution", definition.ID)
	}
	if progress.ExecutionTargetID != target.DeploymentID {
		return OperationExecutionResult{}, fmt.Errorf(
			"resume operation %q: progress execution target %q does not match resolved target %q",
			definition.ID, progress.ExecutionTargetID, target.DeploymentID)
	}
	return e.executeBatches(ctx, operation, target, progress)
}

func (e *ResumableOperationExecutor) validateExecution(
	ctx context.Context,
	operation ResumableOperation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
) (OperationDefinition, ExecutionTarget, error) {
	if operation == nil {
		return OperationDefinition{}, ExecutionTarget{}, errors.New("execute resumable operation: operation is required")
	}
	if !isUUIDv7(runID) {
		return OperationDefinition{}, ExecutionTarget{}, errors.New(
			"execute resumable operation: run ID must be a canonical UUIDv7")
	}
	if operationOrder < 0 {
		return OperationDefinition{}, ExecutionTarget{}, errors.New(
			"execute resumable operation: operation order cannot be negative")
	}
	definition := operation.Definition()
	if definition.InterruptionMode != InterruptionModeResumable {
		return OperationDefinition{}, ExecutionTarget{}, fmt.Errorf(
			"execute operation %q: interruption mode must be resumable", definition.ID)
	}
	readAccess, err := newOperationReadAccess(definition, e.stores)
	if err != nil {
		return OperationDefinition{}, ExecutionTarget{}, fmt.Errorf("execute operation %q: %w", definition.ID, err)
	}
	targets, err := operation.ResolveTargets(ctx, readAccess, selection)
	if err != nil {
		return OperationDefinition{}, ExecutionTarget{}, err
	}
	if err := validateExecutionTargets(definition, targets); err != nil {
		return OperationDefinition{}, ExecutionTarget{}, err
	}
	if len(targets) != 1 {
		return OperationDefinition{}, ExecutionTarget{}, fmt.Errorf(
			"execute operation %q: resumable executor currently requires exactly one execution target", definition.ID)
	}
	return definition, targets[0], nil
}

func (e *ResumableOperationExecutor) executeBatches(
	ctx context.Context,
	operation ResumableOperation,
	target ExecutionTarget,
	progress Progress,
) (OperationExecutionResult, error) {
	definition := operation.Definition()
	result := OperationExecutionResult{Progress: progress}
	for progress.Status != StatusCompleted {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("execute resumable operation %q: %w", definition.ID, err)
		}
		next := progress
		err := e.transactioner.Transact(ctx, func(txCtx context.Context) error {
			state, err := e.metadataStore.GetState(txCtx, definition.StoreID)
			if err != nil {
				return err
			}
			if err := validateOperationReservation(definition, state, progress.RunID); err != nil {
				return err
			}
			if state.ID != progress.MigrationStateID || state.TargetLevel != progress.TargetLevel {
				return fmt.Errorf("execute operation %q: progress does not match the active store reservation",
					definition.ID)
			}
			access, err := newOperationExecutionAccess(definition, e.stores)
			if err != nil {
				return fmt.Errorf("execute operation %q: %w", definition.ID, err)
			}
			batch, err := operation.ExecuteBatch(txCtx, newOperationExecution(access, target),
				append(json.RawMessage(nil), progress.CheckpointData...))
			if err != nil {
				return err
			}
			if err := validateResumableBatchResult(progress.CheckpointData, batch); err != nil {
				return fmt.Errorf("execute operation %q: invalid batch result: %w", definition.ID, err)
			}
			next.CheckpointData = append(json.RawMessage(nil), batch.CheckpointData...)
			next.UpdatedAt = e.now()
			if batch.Completed {
				if err := verifyOperationTarget(txCtx, operation, access, target); err != nil {
					return err
				}
				next.Status = StatusCompleted
			}
			return e.metadataStore.UpdateProgress(txCtx, next)
		})
		if err != nil {
			return result, fmt.Errorf("execute resumable operation %q: %w", definition.ID, err)
		}
		progress = next
		result.Progress = progress
		result.CommittedBatches++
	}
	return result, nil
}

func validateResumableBatchResult(previous json.RawMessage, result ResumableBatchResult) error {
	if len(result.CheckpointData) > maxCheckpointSize {
		return fmt.Errorf("checkpoint exceeds %d bytes", maxCheckpointSize)
	}
	if len(result.CheckpointData) > 0 && !json.Valid(result.CheckpointData) {
		return errors.New("checkpoint must be valid JSON")
	}
	if !result.Completed {
		if len(result.CheckpointData) == 0 {
			return errors.New("incomplete batch requires a checkpoint")
		}
		if bytes.Equal(previous, result.CheckpointData) {
			return errors.New("incomplete batch must advance the checkpoint")
		}
	}
	return nil
}

func verifyOperationTarget(
	ctx context.Context,
	operation Operation,
	access ReadAccess,
	target ExecutionTarget,
) error {
	definition := operation.Definition()
	verification, err := operation.PostVerify(ctx, access, target)
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
	return nil
}
