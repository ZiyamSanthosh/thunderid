// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var (
	queryEntityTypeHandleRollbackBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-11",
		PostgresQuery: `SELECT ID, DEPLOYMENT_ID FROM "ENTITY_TYPES"
			WHERE HANDLE IS NOT NULL AND ID > $1 AND ($2 = '' OR DEPLOYMENT_ID = $2)
			ORDER BY ID LIMIT 100`,
	}
	queryClearEntityTypeHandle = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-12",
		PostgresQuery: `UPDATE "ENTITY_TYPES" SET HANDLE = NULL
			WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}
)

type rollbackEntityTypeHandleBackfillOperation struct{}

func newRollbackEntityTypeHandleBackfillOperation() engine.RollbackOperation {
	return &rollbackEntityTypeHandleBackfillOperation{}
}

func (*rollbackEntityTypeHandleBackfillOperation) ForwardOperationID() engine.OperationID {
	return "2.0.0/configdb/04_backfill_entity_type_handle"
}

func (*rollbackEntityTypeHandleBackfillOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               "2.0.0/configdb/04_backfill_entity_type_handle_rollback",
		Description:      "Remove the entity-type handle representation before returning to the source version",
		StoreID:          engine.StoreIDConfigDB,
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (*rollbackEntityTypeHandleBackfillOperation) ResolveTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	if len(selection.DeploymentIDs) == 0 {
		return []engine.ExecutionTarget{{}}, nil
	}
	targets := make([]engine.ExecutionTarget, 0, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		targets = append(targets, engine.ExecutionTarget{DeploymentID: deploymentID})
	}
	return targets, nil
}

func (*rollbackEntityTypeHandleBackfillOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleRollback(ctx, access, target.DeploymentID)
}

func (*rollbackEntityTypeHandleBackfillOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := entityTypeHandleCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode entity-type handle rollback checkpoint: %w", err)
		}
	}
	rows, err := execution.Access().Query(ctx, engine.StoreIDConfigDB,
		queryEntityTypeHandleRollbackBatch, checkpoint.LastID, execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read entity-type handle rollback batch: %w", err)
	}
	if len(rows) == 0 {
		return entityTypeHandleBatchResult(checkpoint, true)
	}
	for _, row := range rows {
		id, err := row.String("id")
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		deploymentID, err := row.String("deployment_id")
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		if _, err := execution.Access().Execute(
			ctx, engine.StoreIDConfigDB, queryClearEntityTypeHandle, id, deploymentID); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("clear entity type %q handle: %w", id, err)
		}
		checkpoint.LastID = id
	}
	return entityTypeHandleBatchResult(checkpoint, len(rows) < entityTypeHandleBatchSize)
}

func (*rollbackEntityTypeHandleBackfillOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleRollback(ctx, access, target.DeploymentID)
}

func verifyEntityTypeHandleRollback(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	records, err := readEntityTypeHandleRecords(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	for _, record := range records {
		if record.handle != "" {
			return engine.VerificationResult{
				State:   engine.VerificationSource,
				Summary: "The entity-type handle representation requires rollback",
			}, nil
		}
	}
	return engine.VerificationResult{
		State:   engine.VerificationTarget,
		Summary: "The entity-type handle representation has been removed",
	}, nil
}
