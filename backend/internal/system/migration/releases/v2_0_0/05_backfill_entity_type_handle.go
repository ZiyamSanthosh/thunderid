// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	systemutils "github.com/thunder-id/thunderid/internal/system/utils"
)

const (
	entityTypeHandleBatchSize                              = 100
	maxEntityTypeHandleLength                              = 100
	addEntityTypeHandleOperationID      engine.OperationID = "2.0.0/configdb/03_add_entity_type_handle"
	backfillEntityTypeHandleOperationID engine.OperationID = "2.0.0/configdb/05_backfill_entity_type_handle"
)

var (
	queryEntityTypesForHandleBackfill = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-06",
		PostgresQuery: `SELECT ID, NAME, CATEGORY, HANDLE, DEPLOYMENT_ID
			FROM "ENTITY_TYPES" WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, CATEGORY, ID`,
	}
	queryEntityTypeHandleBackfillBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-07",
		PostgresQuery: `SELECT ID, NAME, CATEGORY, DEPLOYMENT_ID FROM "ENTITY_TYPES"
			WHERE HANDLE IS NULL AND ID > $1 AND ($2 = '' OR DEPLOYMENT_ID = $2)
			ORDER BY ID LIMIT 100`,
	}
	queryExistingEntityTypeHandles = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-08",
		PostgresQuery: `SELECT HANDLE, CATEGORY, DEPLOYMENT_ID FROM "ENTITY_TYPES"
			WHERE HANDLE IS NOT NULL AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
	queryUpdateEntityTypeHandle = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-09",
		PostgresQuery: `UPDATE "ENTITY_TYPES" SET HANDLE = $1
			WHERE ID = $2 AND DEPLOYMENT_ID = $3`,
	}
)

type backfillEntityTypeHandleOperation struct{}

type entityTypeHandleCheckpoint struct {
	LastID string `json:"lastID"`
}

type entityTypeHandleRecord struct {
	id           string
	name         string
	category     string
	handle       string
	deploymentID string
}

func newBackfillEntityTypeHandleOperation() engine.Operation {
	return &backfillEntityTypeHandleOperation{}
}

func (*backfillEntityTypeHandleOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               backfillEntityTypeHandleOperationID,
		Description:      "Create valid entity-type handles from legacy names",
		StoreID:          engine.StoreIDConfigDB,
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (*backfillEntityTypeHandleOperation) ResolveTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	if len(selection.DeploymentIDs) > 0 {
		targets := make([]engine.ExecutionTarget, 0, len(selection.DeploymentIDs))
		for _, deploymentID := range selection.DeploymentIDs {
			targets = append(targets, engine.ExecutionTarget{DeploymentID: deploymentID})
		}
		return targets, nil
	}
	return []engine.ExecutionTarget{{}}, nil
}

func (*backfillEntityTypeHandleOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	name, hasName := columns["NAME"]
	if !hasName || !matchesEntityTypeColumn(name, "NO") {
		return blockedEntityTypeHandleBackfillReadiness(
			"ENTITY_TYPES.NAME does not match the accepted source schema"), nil
	}
	handle, hasHandle := columns["HANDLE"]
	if !hasHandle {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Entity-type handle readiness depends on the preceding column expansion",
			DeferredUntil: []engine.OperationID{addEntityTypeHandleOperationID},
		}, nil
	}
	if !matchesEntityTypeColumn(handle, "YES") {
		return blockedEntityTypeHandleBackfillReadiness(
			"The entity-type handle column has an unexpected definition"), nil
	}

	verification, err := verifyEntityTypeHandleBackfill(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return blockedEntityTypeHandleBackfillReadiness(verification.Summary), nil
	}
	if verification.State == engine.VerificationTarget {
		return engine.ReadinessResult{
			State:   engine.ReadinessReady,
			Summary: "Every entity type already has a valid unique handle",
		}, nil
	}
	return engine.ReadinessResult{
		State:   engine.ReadinessReady,
		Summary: "Entity-type handles are ready to be backfilled",
	}, nil
}

func (*backfillEntityTypeHandleOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleBackfill(ctx, access, target.DeploymentID)
}

func (*backfillEntityTypeHandleOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := entityTypeHandleCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode entity-type handle checkpoint: %w", err)
		}
	}

	usedHandles, err := readExistingEntityTypeHandles(
		ctx, execution.Access(), execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	rows, err := execution.Access().Query(ctx, engine.StoreIDConfigDB,
		queryEntityTypeHandleBackfillBatch, checkpoint.LastID, execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read entity-type handle backfill batch: %w", err)
	}
	if len(rows) == 0 {
		return entityTypeHandleBatchResult(checkpoint, true)
	}

	for _, row := range rows {
		record, err := readEntityTypeHandleBatchRecord(row)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		baseHandle := convertEntityTypeNameToHandle(record.name)
		handle, err := nextAvailableEntityTypeHandle(
			baseHandle, record.deploymentID, record.category, usedHandles)
		if err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf(
				"resolve entity type %q handle collision: %w", record.id, err)
		}
		if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB,
			queryUpdateEntityTypeHandle, handle, record.id, record.deploymentID); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("update entity type %q handle: %w", record.id, err)
		}
		usedHandles[entityTypeHandleKey(record.deploymentID, record.category, handle)] = struct{}{}
		checkpoint.LastID = record.id
	}

	return entityTypeHandleBatchResult(checkpoint, len(rows) < entityTypeHandleBatchSize)
}

func (*backfillEntityTypeHandleOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifyEntityTypeHandleBackfill(ctx, access, target.DeploymentID)
}

func verifyEntityTypeHandleBackfill(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (engine.VerificationResult, error) {
	records, err := readEntityTypeHandleRecords(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if len(records) == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "There are no entity-type handles to backfill",
		}, nil
	}
	sourceCount := 0
	usedHandles := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.handle == "" {
			sourceCount++
			continue
		}
		if !systemutils.IsValidHandle(record.handle) {
			return invalidEntityTypeHandleBackfill(fmt.Sprintf(
				"Entity type %q has invalid handle %q", record.id, record.handle)), nil
		}
		key := entityTypeHandleKey(record.deploymentID, record.category, record.handle)
		if _, exists := usedHandles[key]; exists {
			return invalidEntityTypeHandleBackfill(fmt.Sprintf(
				"Entity type %q has duplicate handle %q", record.id, record.handle)), nil
		}
		usedHandles[key] = struct{}{}
	}
	if sourceCount == len(records) {
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: "Entity-type handles have not been backfilled",
		}, nil
	}
	if sourceCount == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "Every entity type has a valid unique handle",
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationSource, Summary: "Some entity-type handles still require backfilling",
	}, nil
}

func readEntityTypeHandleRecords(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) ([]entityTypeHandleRecord, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryEntityTypesForHandleBackfill, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect entity-type handles: %w", err)
	}
	records := make([]entityTypeHandleRecord, 0, len(rows))
	for _, row := range rows {
		record, err := readEntityTypeHandleBatchRecord(row)
		if err != nil {
			return nil, err
		}
		if value, exists := row["handle"]; exists && value != nil {
			switch typed := value.(type) {
			case string:
				record.handle = typed
			case []byte:
				record.handle = string(typed)
			default:
				return nil, fmt.Errorf("column %q has unsupported type %T", "handle", value)
			}
		}
		records = append(records, record)
	}
	return records, nil
}

func readEntityTypeHandleBatchRecord(row engine.QueryRow) (entityTypeHandleRecord, error) {
	id, err := row.String("id")
	if err != nil {
		return entityTypeHandleRecord{}, err
	}
	name, err := row.String("name")
	if err != nil {
		return entityTypeHandleRecord{}, err
	}
	category, err := row.String("category")
	if err != nil {
		return entityTypeHandleRecord{}, err
	}
	deploymentID, err := row.String("deployment_id")
	if err != nil {
		return entityTypeHandleRecord{}, err
	}
	return entityTypeHandleRecord{
		id: id, name: name, category: category, deploymentID: deploymentID,
	}, nil
}

func readExistingEntityTypeHandles(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (map[string]struct{}, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryExistingEntityTypeHandles, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect existing entity-type handles: %w", err)
	}
	usedHandles := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		handle, err := row.String("handle")
		if err != nil {
			return nil, err
		}
		category, err := row.String("category")
		if err != nil {
			return nil, err
		}
		deploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		usedHandles[entityTypeHandleKey(deploymentID, category, handle)] = struct{}{}
	}
	return usedHandles, nil
}

func convertEntityTypeNameToHandle(name string) string {
	if systemutils.IsValidHandle(name) {
		return name
	}
	var builder strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
			previousSeparator = false
		case character == '-' || character == '_':
			builder.WriteRune(character)
			previousSeparator = true
		default:
			if builder.Len() > 0 && !previousSeparator {
				builder.WriteByte('-')
				previousSeparator = true
			}
		}
	}
	handle := strings.Trim(builder.String(), "-_")
	if len(handle) > maxEntityTypeHandleLength {
		handle = strings.TrimRight(handle[:maxEntityTypeHandleLength], "-_")
	}
	if systemutils.IsValidHandle(handle) {
		return handle
	}
	return "entity-type"
}

func nextAvailableEntityTypeHandle(
	baseHandle string,
	deploymentID string,
	category string,
	usedHandles map[string]struct{},
) (string, error) {
	if _, exists := usedHandles[entityTypeHandleKey(deploymentID, category, baseHandle)]; !exists {
		return baseHandle, nil
	}
	for suffixNumber := 1; ; suffixNumber++ {
		suffix := fmt.Sprintf("_%d", suffixNumber)
		maximumBaseLength := maxEntityTypeHandleLength - len(suffix)
		if maximumBaseLength < 1 {
			return "", fmt.Errorf("numeric suffix %q leaves no room for a handle", suffix)
		}
		trimmedBase := baseHandle
		if len(trimmedBase) > maximumBaseLength {
			trimmedBase = strings.TrimRight(trimmedBase[:maximumBaseLength], "-_")
		}
		candidate := trimmedBase + suffix
		if !systemutils.IsValidHandle(candidate) {
			return "", fmt.Errorf("converted handle %q is invalid", candidate)
		}
		if _, exists := usedHandles[entityTypeHandleKey(deploymentID, category, candidate)]; !exists {
			return candidate, nil
		}
	}
}

func entityTypeHandleBatchResult(
	checkpoint entityTypeHandleCheckpoint,
	completed bool,
) (engine.ResumableBatchResult, error) {
	checkpointData, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("encode entity-type handle checkpoint: %w", err)
	}
	return engine.ResumableBatchResult{CheckpointData: checkpointData, Completed: completed}, nil
}

func entityTypeHandleKey(deploymentID string, category string, handle string) string {
	return deploymentID + "\x00" + category + "\x00" + handle
}

func invalidEntityTypeHandleBackfill(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "unexpected_entity_type_handle_backfill", Message: summary}},
	}
}

func blockedEntityTypeHandleBackfillReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State:   engine.ReadinessBlocked,
		Summary: summary,
		Findings: []engine.Finding{{
			Code:        "unexpected_entity_type_handle_backfill",
			Message:     summary,
			Remediation: "Reconcile the ENTITY_TYPES schema and handle values before retrying",
		}},
	}
}
