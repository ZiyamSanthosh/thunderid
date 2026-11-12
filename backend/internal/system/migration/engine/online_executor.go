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

// OnlineOperationExecutor runs restartable schema work outside a database transaction.
type OnlineOperationExecutor struct {
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	stores        map[StoreID]ReadStore
	newID         func() (string, error)
	now           func() time.Time
}

// NewOnlineOperationExecutor creates an executor for non-transactional online schema operations.
func NewOnlineOperationExecutor(
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
	stores map[StoreID]ReadStore,
) (*OnlineOperationExecutor, error) {
	if metadataStore == nil {
		return nil, errors.New("create online operation executor: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create online operation executor: transactioner is required")
	}
	storeCopy := make(map[StoreID]ReadStore, len(stores))
	for storeID, store := range stores {
		storeCopy[storeID] = store
	}
	return &OnlineOperationExecutor{
		metadataStore: metadataStore,
		transactioner: transactioner,
		stores:        storeCopy,
		newID:         utils.GenerateUUIDv7,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// Execute initializes or resumes one online operation, runs it in autocommit mode, and records completion.
func (e *OnlineOperationExecutor) Execute(
	ctx context.Context,
	operation OnlineOperation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
	existing *Progress,
) (OperationExecutionResult, error) {
	definition, target, err := e.validateExecution(ctx, operation, selection, runID, operationOrder)
	if err != nil {
		return OperationExecutionResult{}, err
	}

	progress, skipped, err := e.prepare(ctx, operation, target, runID, operationOrder, existing)
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("prepare online operation %q: %w", definition.ID, err)
	}
	result := OperationExecutionResult{Progress: progress, ExecutionSkipped: skipped}
	if skipped {
		return result, nil
	}

	access, err := newOperationExecutionAccess(definition, e.stores)
	if err != nil {
		return OperationExecutionResult{}, fmt.Errorf("execute online operation %q: %w", definition.ID, err)
	}
	if err := operation.Execute(ctx, newOperationExecution(access, target)); err != nil {
		return result, fmt.Errorf("execute online operation %q: %w", definition.ID, err)
	}

	progress, err = e.complete(ctx, operation, target, progress)
	if err != nil {
		return result, fmt.Errorf("complete online operation %q: %w", definition.ID, err)
	}
	result.Progress = progress
	return result, nil
}

func (e *OnlineOperationExecutor) validateExecution(
	ctx context.Context,
	operation OnlineOperation,
	selection MigrationSelection,
	runID string,
	operationOrder int,
) (OperationDefinition, ExecutionTarget, error) {
	if operation == nil {
		return OperationDefinition{}, ExecutionTarget{}, errors.New("execute online operation: operation is required")
	}
	if !isUUIDv7(runID) {
		return OperationDefinition{}, ExecutionTarget{}, errors.New(
			"execute online operation: run ID must be a canonical UUIDv7")
	}
	if operationOrder < 0 {
		return OperationDefinition{}, ExecutionTarget{}, errors.New(
			"execute online operation: operation order cannot be negative")
	}
	definition := operation.Definition()
	if definition.InterruptionMode != InterruptionModeOnline {
		return OperationDefinition{}, ExecutionTarget{}, fmt.Errorf(
			"execute operation %q: interruption mode must be online", definition.ID)
	}
	if definition.Kind != OperationKindSchemaChange {
		return OperationDefinition{}, ExecutionTarget{}, fmt.Errorf(
			"execute operation %q: online execution requires a schema-change operation", definition.ID)
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
			"execute operation %q: online executor requires exactly one execution target", definition.ID)
	}
	return definition, targets[0], nil
}

func (e *OnlineOperationExecutor) prepare(
	ctx context.Context,
	operation OnlineOperation,
	target ExecutionTarget,
	runID string,
	operationOrder int,
	existing *Progress,
) (Progress, bool, error) {
	definition := operation.Definition()
	progress := Progress{}
	if existing != nil {
		progress = *existing
	} else {
		progressID, err := e.newID()
		if err != nil {
			return Progress{}, false, fmt.Errorf("generate progress ID: %w", err)
		}
		progress.ID = progressID
	}
	skipped := false
	err := e.transactioner.Transact(ctx, func(txCtx context.Context) error {
		state, err := e.metadataStore.GetState(txCtx, definition.StoreID)
		if err != nil {
			return err
		}
		if err := validateOperationReservation(definition, state, runID); err != nil {
			return err
		}
		if existing == nil {
			previous, err := e.metadataStore.GetProgress(
				txCtx, state.ID, state.TargetLevel, definition.ID, target.DeploymentID)
			if err != nil {
				return err
			}
			if previous != nil {
				if previous.Status != StatusCompleted {
					return fmt.Errorf("existing complete-store progress has status %q", previous.Status)
				}
				access, err := newOperationExecutionAccess(definition, e.stores)
				if err != nil {
					return err
				}
				if err := verifyOperationTarget(txCtx, operation, access, target); err != nil {
					return err
				}
				progress = *previous
				skipped = true
				return nil
			}
			progress = Progress{
				ID: progress.ID, MigrationStateID: state.ID, RunID: runID, TargetLevel: state.TargetLevel,
				OperationID: string(definition.ID), OperationOrder: operationOrder,
				ExecutionTargetID: target.DeploymentID, Status: StatusInProgress, UpdatedAt: e.now(),
			}
			if err := e.metadataStore.InsertProgress(txCtx, progress); err != nil {
				return err
			}
		} else if err := validateOnlineProgress(progress, state, definition, target, runID, operationOrder); err != nil {
			return err
		}

		access, err := newOperationExecutionAccess(definition, e.stores)
		if err != nil {
			return err
		}
		verification, err := operation.PreVerify(txCtx, access, target)
		if err != nil {
			return err
		}
		if err := verification.validate(); err != nil {
			return fmt.Errorf("invalid pre-verification result: %w", err)
		}
		switch verification.State {
		case VerificationSource:
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
			skipped = true
			return nil
		case VerificationInvalid:
			return fmt.Errorf("pre-verification is invalid: %s", verification.Summary)
		default:
			return fmt.Errorf("unsupported pre-verification state %q", verification.State)
		}
	})
	return progress, skipped, err
}

func (e *OnlineOperationExecutor) complete(
	ctx context.Context,
	operation OnlineOperation,
	target ExecutionTarget,
	progress Progress,
) (Progress, error) {
	definition := operation.Definition()
	err := e.transactioner.Transact(ctx, func(txCtx context.Context) error {
		state, err := e.metadataStore.GetState(txCtx, definition.StoreID)
		if err != nil {
			return err
		}
		if err := validateOperationReservation(definition, state, progress.RunID); err != nil {
			return err
		}
		access, err := newOperationExecutionAccess(definition, e.stores)
		if err != nil {
			return err
		}
		if err := operation.Finalize(txCtx, newOperationExecution(access, target)); err != nil {
			return err
		}
		if err := verifyOperationTarget(txCtx, operation, access, target); err != nil {
			return err
		}
		progress.Status = StatusCompleted
		progress.UpdatedAt = e.now()
		return e.metadataStore.UpdateProgress(txCtx, progress)
	})
	return progress, err
}

func validateOnlineProgress(
	progress Progress,
	state *State,
	definition OperationDefinition,
	target ExecutionTarget,
	runID string,
	operationOrder int,
) error {
	if err := progress.Validate(); err != nil {
		return fmt.Errorf("invalid progress: %w", err)
	}
	if progress.Status != StatusInProgress || progress.MigrationStateID != state.ID ||
		progress.RunID != runID || progress.TargetLevel != state.TargetLevel ||
		progress.OperationID != string(definition.ID) || progress.OperationOrder != operationOrder ||
		progress.ExecutionTargetID != target.DeploymentID {
		return errors.New("progress does not identify this in-progress online operation")
	}
	return nil
}
