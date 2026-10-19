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

const entityTypeReferenceBatchSize = 100

type entityTypeReferenceDirection string

const (
	entityTypeReferenceForward  entityTypeReferenceDirection = "forward"
	entityTypeReferenceRollback entityTypeReferenceDirection = "rollback"
)

type entityTypeReferenceCursor struct {
	DeploymentID string `json:"deploymentID"`
	ID           string `json:"id"`
	Version      int64  `json:"version,omitempty"`
}

type entityTypeReferenceMapping struct {
	byName   map[string]string
	byHandle map[string]string
	// declaredByName and declaredByHandle hold the read-only entity types from declarative resource
	// files, keyed by category and value only. They are not scoped to a deployment.
	declaredByName   map[string]string
	declaredByHandle map[string]string
}

type entityTypeReferenceTransform func(
	payload string,
	deploymentID string,
	row engine.QueryRow,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (string, bool, error)

type entityTypeReferenceOperationConfig struct {
	definition      engine.OperationDefinition
	direction       entityTypeReferenceDirection
	dependency      engine.OperationID
	inspectQuery    dbmodel.DBQuery
	batchQuery      dbmodel.DBQuery
	updateQuery     dbmodel.DBQuery
	payloadColumn   string
	payloadOptional bool
	batchArgs       func(entityTypeReferenceCursor, engine.ExecutionTarget) []interface{}
	readCursor      func(engine.QueryRow) (entityTypeReferenceCursor, error)
	updateArgs      func(string, engine.QueryRow) ([]interface{}, error)
	transform       entityTypeReferenceTransform
	declared        *declarativeEntityTypeSource
	targetSummary   string
	sourceSummary   string
	findingCode     string
}

type entityTypeReferenceOperation struct {
	config entityTypeReferenceOperationConfig
}

type rollbackEntityTypeReferenceOperation struct {
	*entityTypeReferenceOperation
	forwardID engine.OperationID
}

func newEntityTypeReferenceOperation(config entityTypeReferenceOperationConfig) engine.Operation {
	config = normalizeEntityTypeReferenceOperationConfig(config)
	return &entityTypeReferenceOperation{config: config}
}

func newRollbackEntityTypeReferenceOperation(
	config entityTypeReferenceOperationConfig,
	forwardID engine.OperationID,
) engine.RollbackOperation {
	config = normalizeEntityTypeReferenceOperationConfig(config)
	return &rollbackEntityTypeReferenceOperation{
		entityTypeReferenceOperation: &entityTypeReferenceOperation{config: config},
		forwardID:                    forwardID,
	}
}

func normalizeEntityTypeReferenceOperationConfig(
	config entityTypeReferenceOperationConfig,
) entityTypeReferenceOperationConfig {
	if config.direction == entityTypeReferenceRollback {
		config.targetSummary, config.sourceSummary = config.sourceSummary, config.targetSummary
	}
	return config
}

func (o *entityTypeReferenceOperation) Definition() engine.OperationDefinition {
	definition := o.config.definition
	definition.ReadStoreIDs = append([]engine.StoreID(nil), o.config.definition.ReadStoreIDs...)
	return definition
}

func (o *rollbackEntityTypeReferenceOperation) ForwardOperationID() engine.OperationID {
	return o.forwardID
}

func (*entityTypeReferenceOperation) ResolveTargets(
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

func (o *entityTypeReferenceOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	handle, exists := columns["HANDLE"]
	if !exists {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Entity-type reference migration depends on the entity-type handle column",
			DeferredUntil: []engine.OperationID{addEntityTypeHandleOperationID},
		}, nil
	}
	if !matchesEntityTypeColumn(handle, "YES") && !matchesEntityTypeColumn(handle, "NO") {
		return engine.ReadinessResult{
			State:   engine.ReadinessBlocked,
			Summary: "The entity-type handle column has an unexpected definition",
			Findings: []engine.Finding{{
				Code:        o.config.findingCode,
				Message:     "The entity-type handle column has an unexpected definition",
				Remediation: "Reconcile the ENTITY_TYPES.HANDLE column before retrying",
			}},
		}, nil
	}
	mapping, complete, err := o.readMapping(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !complete {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Entity-type reference migration depends on the completed handle backfill",
			DeferredUntil: []engine.OperationID{o.config.dependency},
		}, nil
	}
	verification, err := o.verify(ctx, access, engine.ExecutionTarget{}, mapping)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State:    engine.ReadinessBlocked,
			Summary:  verification.Summary,
			Findings: verification.Findings,
		}, nil
	}
	return engine.ReadinessResult{
		State:   engine.ReadinessReady,
		Summary: "Entity-type references are ready to be migrated",
	}, nil
}

func (o *entityTypeReferenceOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	mapping, complete, err := o.readMapping(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !complete {
		return o.invalidVerification("Entity-type handles have not been completely backfilled"), nil
	}
	return o.verify(ctx, access, target, mapping)
}

func (o *entityTypeReferenceOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := entityTypeReferenceCursor{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode entity-type reference checkpoint: %w", err)
		}
	}
	mapping, complete, err := o.readMapping(
		ctx, execution.Access(), execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	if !complete {
		return engine.ResumableBatchResult{}, fmt.Errorf("entity-type handles have not been completely backfilled")
	}
	rows, err := execution.Access().Query(ctx, o.config.definition.StoreID, o.config.batchQuery,
		o.config.batchArgs(checkpoint, execution.Target())...)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read entity-type reference batch: %w", err)
	}
	if len(rows) == 0 {
		return entityTypeReferenceBatchResult(checkpoint, true)
	}

	for _, row := range rows {
		cursor, err := o.config.readCursor(row)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		payload, err := readEntityTypeReferencePayload(row, o.config.payloadColumn, o.config.payloadOptional)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		updated, changed, err := o.config.transform(
			payload, cursor.DeploymentID, row, mapping, o.config.direction)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		if changed {
			args, err := o.config.updateArgs(updated, row)
			if err != nil {
				return engine.ResumableBatchResult{}, err
			}
			if _, err := execution.Access().Execute(
				ctx, o.config.definition.StoreID, o.config.updateQuery, args...); err != nil {
				return engine.ResumableBatchResult{}, fmt.Errorf("update entity-type references: %w", err)
			}
		}
		checkpoint = cursor
	}
	return entityTypeReferenceBatchResult(checkpoint, len(rows) < entityTypeReferenceBatchSize)
}

func (o *entityTypeReferenceOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	mapping, complete, err := o.readMapping(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !complete {
		return o.invalidVerification("Entity-type handles have not been completely backfilled"), nil
	}
	return o.verifyTarget(ctx, access, target, mapping)
}

func (o *entityTypeReferenceOperation) verifyTarget(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
	mapping entityTypeReferenceMapping,
) (engine.VerificationResult, error) {
	rows, err := access.Query(ctx, o.config.definition.StoreID, o.config.inspectQuery, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect entity-type references: %w", err)
	}
	validationDirection := entityTypeReferenceRollback
	if o.config.direction == entityTypeReferenceRollback {
		validationDirection = entityTypeReferenceForward
	}
	for _, row := range rows {
		cursor, err := o.config.readCursor(row)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		payload, err := readEntityTypeReferencePayload(row, o.config.payloadColumn, o.config.payloadOptional)
		if err != nil {
			return o.invalidVerification(err.Error()), nil
		}
		if _, _, err := o.config.transform(
			payload, cursor.DeploymentID, row, mapping, validationDirection); err != nil {
			return o.invalidVerification(err.Error()), nil
		}
	}
	return engine.VerificationResult{
		State: engine.VerificationTarget, Summary: o.config.targetSummary,
	}, nil
}

func (o *entityTypeReferenceOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
	mapping entityTypeReferenceMapping,
) (engine.VerificationResult, error) {
	rows, err := access.Query(ctx, o.config.definition.StoreID, o.config.inspectQuery, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect entity-type references: %w", err)
	}
	needsMigration := false
	for _, row := range rows {
		cursor, err := o.config.readCursor(row)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		payload, err := readEntityTypeReferencePayload(row, o.config.payloadColumn, o.config.payloadOptional)
		if err != nil {
			return o.invalidVerification(err.Error()), nil
		}
		_, changed, err := o.config.transform(payload, cursor.DeploymentID, row, mapping, o.config.direction)
		if err != nil {
			return o.invalidVerification(err.Error()), nil
		}
		needsMigration = needsMigration || changed
	}
	if needsMigration {
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: o.config.sourceSummary,
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationTarget, Summary: o.config.targetSummary,
	}, nil
}

func (o *entityTypeReferenceOperation) invalidVerification(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State:   engine.VerificationInvalid,
		Summary: summary,
		Findings: []engine.Finding{{
			Code:        o.config.findingCode,
			Message:     summary,
			Remediation: "Reconcile the entity-type mapping and stored references before retrying",
		}},
	}
}

// readMapping reads the entity type mapping from the database and adds the declarative entity types.
func (o *entityTypeReferenceOperation) readMapping(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (entityTypeReferenceMapping, bool, error) {
	mapping, complete, err := readEntityTypeReferenceMapping(ctx, access, deploymentID)
	if err != nil {
		return entityTypeReferenceMapping{}, false, err
	}
	declared, err := o.config.declared.list(ctx)
	if err != nil {
		return entityTypeReferenceMapping{}, false, fmt.Errorf("read declarative entity types: %w", err)
	}
	mapping.addDeclared(declared)
	return mapping, complete, nil
}

func (m *entityTypeReferenceMapping) addDeclared(declared []DeclarativeEntityType) {
	if len(declared) == 0 {
		return
	}
	m.declaredByName = make(map[string]string, len(declared))
	m.declaredByHandle = make(map[string]string, len(declared))
	for _, entityType := range declared {
		prefix := entityType.Category + "\x00"
		m.declaredByName[prefix+entityType.DisplayName] = entityType.Handle
		m.declaredByHandle[prefix+entityType.Handle] = entityType.DisplayName
	}
}

func readEntityTypeReferenceMapping(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (entityTypeReferenceMapping, bool, error) {
	records, err := readEntityTypeHandleRecords(ctx, access, deploymentID)
	if err != nil {
		return entityTypeReferenceMapping{}, false, err
	}
	mapping := entityTypeReferenceMapping{
		byName:   make(map[string]string, len(records)),
		byHandle: make(map[string]string, len(records)),
	}
	complete := true
	for _, record := range records {
		if record.handle == "" {
			complete = false
			continue
		}
		keyPrefix := record.deploymentID + "\x00" + record.category + "\x00"
		mapping.byName[keyPrefix+record.name] = record.handle
		mapping.byHandle[keyPrefix+record.handle] = record.name
	}
	return mapping, complete, nil
}

func (m entityTypeReferenceMapping) translate(
	deploymentID string,
	category string,
	value string,
	direction entityTypeReferenceDirection,
) (string, bool, error) {
	keyPrefix := deploymentID + "\x00" + category + "\x00"
	declaredPrefix := category + "\x00"
	// Forward maps a type name to its handle, rollback maps a handle back to its name.
	stored, storedOpposite := m.byName, m.byHandle
	declared, declaredOpposite := m.declaredByName, m.declaredByHandle
	if direction != entityTypeReferenceForward {
		stored, storedOpposite = m.byHandle, m.byName
		declared, declaredOpposite = m.declaredByHandle, m.declaredByName
	}
	storedTranslated, inStored := stored[keyPrefix+value]
	declaredTranslated, inDeclared := declared[declaredPrefix+value]
	if inStored && inDeclared && storedTranslated != declaredTranslated {
		return "", false, fmt.Errorf(
			"entity-type reference %q is defined differently in deployment %q (%q) and in declarative "+
				"resources (%q) for category %q",
			value, deploymentID, storedTranslated, declaredTranslated, category)
	}
	if inStored {
		return storedTranslated, storedTranslated != value, nil
	}
	if _, exists := storedOpposite[keyPrefix+value]; exists {
		return value, false, nil
	}
	if inDeclared {
		return declaredTranslated, declaredTranslated != value, nil
	}
	if _, exists := declaredOpposite[declaredPrefix+value]; exists {
		return value, false, nil
	}
	return "", false, fmt.Errorf(
		"entity-type reference %q has no %s mapping in deployment %q for category %q",
		value, direction, deploymentID, category)
}

func (m entityTypeReferenceMapping) translateAnyCategory(
	deploymentID string,
	value string,
	direction entityTypeReferenceDirection,
) (string, bool, error) {
	var translated string
	found := false
	for _, category := range []string{"user", "agent"} {
		candidate, changed, err := m.translate(deploymentID, category, value, direction)
		if err != nil {
			continue
		}
		if !found {
			translated = candidate
			found = true
		} else if translated != candidate {
			return "", false, fmt.Errorf(
				"entity-type reference %q is ambiguous across categories in deployment %q",
				value, deploymentID)
		}
		if !changed && candidate == value {
			translated = value
		}
	}
	if !found {
		return "", false, fmt.Errorf(
			"entity-type reference %q has no %s mapping in deployment %q",
			value, direction, deploymentID)
	}
	return translated, translated != value, nil
}

func readEntityTypeReferencePayload(row engine.QueryRow, column string, optional bool) (string, error) {
	value, exists := row[column]
	if !exists || value == nil {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("column %q is missing", column)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("column %q has unsupported type %T", column, value)
	}
}

func entityTypeReferenceBatchResult(
	checkpoint entityTypeReferenceCursor,
	completed bool,
) (engine.ResumableBatchResult, error) {
	checkpointData, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("encode entity-type reference checkpoint: %w", err)
	}
	return engine.ResumableBatchResult{CheckpointData: checkpointData, Completed: completed}, nil
}

func simpleEntityTypeReferenceBatchArgs(
	checkpoint entityTypeReferenceCursor,
	target engine.ExecutionTarget,
) []interface{} {
	return []interface{}{checkpoint.DeploymentID, checkpoint.ID, target.DeploymentID}
}

func simpleEntityTypeReferenceCursor(row engine.QueryRow) (entityTypeReferenceCursor, error) {
	deploymentID, err := row.String("deployment_id")
	if err != nil {
		return entityTypeReferenceCursor{}, err
	}
	id, err := row.String("id")
	if err != nil {
		return entityTypeReferenceCursor{}, err
	}
	return entityTypeReferenceCursor{DeploymentID: deploymentID, ID: id}, nil
}

func flowEntityTypeReferenceCursor(row engine.QueryRow) (entityTypeReferenceCursor, error) {
	cursor, err := simpleEntityTypeReferenceCursor(row)
	if err != nil {
		return entityTypeReferenceCursor{}, err
	}
	version, err := row.Int64("version")
	if err != nil {
		return entityTypeReferenceCursor{}, err
	}
	cursor.Version = version
	return cursor, nil
}

func flowEntityTypeReferenceBatchArgs(
	checkpoint entityTypeReferenceCursor,
	target engine.ExecutionTarget,
) []interface{} {
	return []interface{}{checkpoint.DeploymentID, checkpoint.ID, checkpoint.Version, target.DeploymentID}
}
