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
)

const (
	agentNameBatchSize                                          = 100
	migrateAgentNamesToAttributesOperationID engine.OperationID = "2.0.0/entitydb/49_migrate_agent_names_to_attributes"
)

var (
	queryAgentTypeNameSchemas = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-38",
		PostgresQuery: `SELECT NAME, HANDLE, SCHEMA_DEF, DEPLOYMENT_ID
			FROM "ENTITY_TYPES" WHERE CATEGORY = 'agent'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
	queryAgentNames = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-11",
		PostgresQuery: `SELECT ID, TYPE, ATTRIBUTES, SYSTEM_ATTRIBUTES, DEPLOYMENT_ID
			FROM "ENTITY" WHERE CATEGORY = 'agent'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryAgentNameBatch = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-12",
		PostgresQuery: `SELECT ID, TYPE, ATTRIBUTES, SYSTEM_ATTRIBUTES, DEPLOYMENT_ID
			FROM "ENTITY" WHERE CATEGORY = 'agent'
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ID > $2))
			AND ($3 = '' OR DEPLOYMENT_ID = $3)
			ORDER BY DEPLOYMENT_ID, ID LIMIT 100`,
	}
	queryUpdateAgentNameRepresentation = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-13",
		PostgresQuery: `UPDATE "ENTITY" SET ATTRIBUTES = $1, SYSTEM_ATTRIBUTES = $2
			WHERE ID = $3
			AND ATTRIBUTES IS NOT DISTINCT FROM $4
			AND SYSTEM_ATTRIBUTES IS NOT DISTINCT FROM $5
			AND DEPLOYMENT_ID = $6`,
	}
	queryUpsertAgentNameIdentifier = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-14",
		PostgresQuery: `INSERT INTO "ENTITY_IDENTIFIER"
			(ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT, DEPLOYMENT_ID)
			VALUES ($1, 'name', $2, $3, NOW(), $4)
			ON CONFLICT (ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)
			DO UPDATE SET SOURCE = EXCLUDED.SOURCE`,
	}
	queryAgentNameIdentifiers = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-15",
		PostgresQuery: `SELECT ENTITY_ID, VALUE, SOURCE, DEPLOYMENT_ID
			FROM "ENTITY_IDENTIFIER" WHERE NAME = 'name'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ENTITY_ID, VALUE`,
	}
	queryAgentNameCollisions = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-16",
		PostgresQuery: `SELECT DEPLOYMENT_ID, COUNT(DISTINCT ID) AS ENTITY_COUNT
			FROM "ENTITY"
			WHERE COALESCE(ATTRIBUTES ->> 'name', SYSTEM_ATTRIBUTES ->> 'name') IS NOT NULL
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			GROUP BY DEPLOYMENT_ID,
				COALESCE(ATTRIBUTES ->> 'name', SYSTEM_ATTRIBUTES ->> 'name')
			HAVING COUNT(DISTINCT ID) > 1 AND BOOL_OR(CATEGORY = 'agent')`,
	}
)

type agentNameDirection string

const (
	agentNameForward  agentNameDirection = "forward"
	agentNameRollback agentNameDirection = "rollback"
)

type agentNameCheckpoint struct {
	DeploymentID string `json:"deploymentID"`
	ID           string `json:"id"`
}

type agentNameRecord struct {
	id                       string
	entityType               string
	deploymentID             string
	attributes               map[string]interface{}
	systemAttributes         map[string]interface{}
	originalAttributes       interface{}
	originalSystemAttributes interface{}
}

type agentNameIdentifier struct {
	value  string
	source string
}

type agentNameOperation struct {
	id          engine.OperationID
	description string
	direction   agentNameDirection
	declared    *declarativeEntityTypeSource
}

type rollbackAgentNameOperation struct {
	*agentNameOperation
}

func newMigrateAgentNamesToAttributesOperation(declared *declarativeEntityTypeSource) engine.Operation {
	return &agentNameOperation{
		id:          migrateAgentNamesToAttributesOperationID,
		description: "Move agent names from system attributes to schema attributes",
		direction:   agentNameForward,
		declared:    declared,
	}
}

func newRollbackAgentNamesToAttributesOperation(declared *declarativeEntityTypeSource) engine.RollbackOperation {
	return &rollbackAgentNameOperation{&agentNameOperation{
		id:          "2.0.0/entitydb/49_migrate_agent_names_to_attributes_rollback",
		description: "Restore agent names to system attributes",
		direction:   agentNameRollback,
		declared:    declared,
	}}
}

func (*rollbackAgentNameOperation) ForwardOperationID() engine.OperationID {
	return migrateAgentNamesToAttributesOperationID
}

func (o *agentNameOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               o.id,
		Description:      o.description,
		StoreID:          engine.StoreIDEntityDB,
		ReadStoreIDs:     []engine.StoreID{engine.StoreIDConfigDB},
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (*agentNameOperation) ResolveTargets(
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

func (o *agentNameOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if _, exists := columns["HANDLE"]; !exists {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Agent name migration depends on the entity-type handle column",
			DeferredUntil: []engine.OperationID{addEntityTypeHandleOperationID},
		}, nil
	}
	_, complete, err := readEntityTypeReferenceMapping(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !complete {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Agent name migration depends on entity-type handles",
			DeferredUntil: []engine.OperationID{backfillEntityTypeHandleOperationID},
		}, nil
	}
	defaultSchemaReady, err := defaultAgentNameSchemaReady(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !defaultSchemaReady {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Agent name migration depends on the default agent name schema",
			DeferredUntil: []engine.OperationID{migrateDefaultAgentNameSchemaOperationID},
		}, nil
	}
	verification, err := o.verify(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary, Findings: verification.Findings,
		}, nil
	}
	return engine.ReadinessResult{
		State: engine.ReadinessReady, Summary: "Agent names are ready to be migrated",
	}, nil
}

func (o *agentNameOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *agentNameOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := agentNameCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode agent name checkpoint: %w", err)
		}
	}
	rows, err := execution.Access().Query(ctx, engine.StoreIDEntityDB, queryAgentNameBatch,
		checkpoint.DeploymentID, checkpoint.ID, execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read agent name batch: %w", err)
	}
	if len(rows) == 0 {
		return agentNameBatchResult(checkpoint, true)
	}
	for _, row := range rows {
		record, err := readAgentNameRecord(row)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		attributes, systemAttributes, name, changed, err := transformAgentName(record, o.direction)
		if err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("transform agent %q: %w", record.id, err)
		}
		if changed {
			attributesJSON, err := json.Marshal(attributes)
			if err != nil {
				return engine.ResumableBatchResult{}, fmt.Errorf("encode agent attributes: %w", err)
			}
			systemAttributesJSON, err := json.Marshal(systemAttributes)
			if err != nil {
				return engine.ResumableBatchResult{}, fmt.Errorf("encode agent system attributes: %w", err)
			}
			affected, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB,
				queryUpdateAgentNameRepresentation, string(attributesJSON), string(systemAttributesJSON),
				record.id, record.originalAttributes, record.originalSystemAttributes,
				record.deploymentID)
			if err != nil {
				return engine.ResumableBatchResult{}, fmt.Errorf("update agent %q name representation: %w", record.id, err)
			}
			if affected != 1 {
				return engine.ResumableBatchResult{}, fmt.Errorf(
					"update agent %q name representation: source value changed concurrently", record.id)
			}
		}
		source := "attribute"
		if o.direction == agentNameRollback {
			source = "system"
		}
		if _, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB,
			queryUpsertAgentNameIdentifier, record.id, name, source, record.deploymentID); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("upsert agent %q name identifier: %w", record.id, err)
		}
		checkpoint = agentNameCheckpoint{DeploymentID: record.deploymentID, ID: record.id}
	}
	return agentNameBatchResult(checkpoint, len(rows) < agentNameBatchSize)
}

func (o *agentNameOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *agentNameOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	collisions, err := access.Query(ctx, engine.StoreIDEntityDB, queryAgentNameCollisions, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect agent name collisions: %w", err)
	}
	if len(collisions) > 0 {
		return invalidAgentNameMigrationVerification(
			"One or more deployments contain entities with colliding name values"), nil
	}
	supportedTypes, err := readAgentTypesSupportingName(ctx, access, target.DeploymentID, o.declared)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	records, err := readAgentNameRecords(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	identifiers, err := readAgentNameIdentifiers(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	needsMigration := 0
	for _, record := range records {
		if !supportedTypes[agentTypeKey(record.deploymentID, record.entityType)] &&
			!supportedTypes[agentTypeKey("", record.entityType)] {
			return invalidAgentNameMigrationVerification(fmt.Sprintf(
				"Agent %q uses an entity type that does not declare a string name attribute", record.id)), nil
		}
		state, name, err := classifyAgentName(record)
		if err != nil {
			return invalidAgentNameMigrationVerification(
				fmt.Sprintf("Agent %q has an invalid name representation: %v", record.id, err)), nil
		}
		identifierRows := identifiers[agentIdentifierKey(record.deploymentID, record.id)]
		if len(identifierRows) > 1 || len(identifierRows) == 1 && identifierRows[0].value != name {
			return invalidAgentNameMigrationVerification(
				fmt.Sprintf("Agent %q has unexpected name identifier rows", record.id)), nil
		}
		expectedState := engine.VerificationTarget
		expectedSource := "attribute"
		if o.direction == agentNameRollback {
			expectedState = engine.VerificationSource
			expectedSource = "system"
		}
		identifierReady := len(identifierRows) == 1 && identifierRows[0].source == expectedSource
		if state != expectedState || !identifierReady {
			needsMigration++
		}
	}
	if needsMigration > 0 {
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: "Some agent names still require migration",
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationTarget, Summary: "Every agent name uses the required representation",
	}, nil
}

func defaultAgentNameSchemaReady(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (bool, error) {
	records, err := readDefaultAgentTypeRecords(ctx, access, deploymentID)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		state, err := classifyDefaultAgentNameSchema(record.schema, record.systemAttributes)
		if err != nil {
			return false, err
		}
		if state != engine.VerificationTarget {
			return false, nil
		}
	}
	return true, nil
}

func readAgentTypesSupportingName(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
	declared *declarativeEntityTypeSource,
) (map[string]bool, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryAgentTypeNameSchemas, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect agent type name schemas: %w", err)
	}
	result := make(map[string]bool, len(rows)*2)
	for _, row := range rows {
		name, err := row.String("name")
		if err != nil {
			return nil, err
		}
		handle, err := row.String("handle")
		if err != nil {
			return nil, err
		}
		rowDeploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		schema, err := readAgentNameJSONObject(row, "schema_def")
		if err != nil {
			return nil, err
		}
		definition, ok := schema["name"].(map[string]interface{})
		supported := ok && definition["type"] == "string"
		result[agentTypeKey(rowDeploymentID, name)] = supported
		result[agentTypeKey(rowDeploymentID, handle)] = supported
	}
	declaredTypes, err := declared.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("read declarative agent type schemas: %w", err)
	}
	for _, entityType := range declaredTypes {
		if !strings.EqualFold(entityType.Category, "agent") {
			continue
		}
		result[agentTypeKey("", entityType.DisplayName)] = entityType.SupportsNameAttribute
		result[agentTypeKey("", entityType.Handle)] = entityType.SupportsNameAttribute
	}
	return result, nil
}

func readAgentNameRecords(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) ([]agentNameRecord, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryAgentNames, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect agent names: %w", err)
	}
	records := make([]agentNameRecord, 0, len(rows))
	for _, row := range rows {
		record, err := readAgentNameRecord(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func readAgentNameRecord(row engine.QueryRow) (agentNameRecord, error) {
	id, err := row.String("id")
	if err != nil {
		return agentNameRecord{}, err
	}
	entityType, err := row.String("type")
	if err != nil {
		return agentNameRecord{}, err
	}
	deploymentID, err := row.String("deployment_id")
	if err != nil {
		return agentNameRecord{}, err
	}
	attributes, err := readAgentNameJSONObject(row, "attributes")
	if err != nil {
		return agentNameRecord{}, fmt.Errorf("read agent %q attributes: %w", id, err)
	}
	systemAttributes, err := readAgentNameJSONObject(row, "system_attributes")
	if err != nil {
		return agentNameRecord{}, fmt.Errorf("read agent %q system attributes: %w", id, err)
	}
	originalAttributes, hasAttributes := row["attributes"]
	if !hasAttributes {
		return agentNameRecord{}, fmt.Errorf("read agent %q attributes: column is missing", id)
	}
	originalSystemAttributes, hasSystemAttributes := row["system_attributes"]
	if !hasSystemAttributes {
		return agentNameRecord{}, fmt.Errorf("read agent %q system attributes: column is missing", id)
	}
	return agentNameRecord{
		id: id, entityType: entityType, deploymentID: deploymentID,
		attributes: attributes, systemAttributes: systemAttributes,
		originalAttributes: originalAttributes, originalSystemAttributes: originalSystemAttributes,
	}, nil
}

func readAgentNameIdentifiers(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (map[string][]agentNameIdentifier, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryAgentNameIdentifiers, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect agent name identifiers: %w", err)
	}
	result := make(map[string][]agentNameIdentifier)
	for _, row := range rows {
		entityID, err := row.String("entity_id")
		if err != nil {
			return nil, err
		}
		rowDeploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		value, err := row.String("value")
		if err != nil {
			return nil, err
		}
		source, err := row.String("source")
		if err != nil {
			return nil, err
		}
		key := agentIdentifierKey(rowDeploymentID, entityID)
		result[key] = append(result[key], agentNameIdentifier{value: value, source: source})
	}
	return result, nil
}

func classifyAgentName(record agentNameRecord) (engine.VerificationState, string, error) {
	attributeName, hasAttributeName := record.attributes["name"]
	systemName, hasSystemName := record.systemAttributes["name"]
	if hasAttributeName && hasSystemName {
		return engine.VerificationInvalid, "", fmt.Errorf("name exists in both attribute objects")
	}
	if !hasAttributeName && !hasSystemName {
		return engine.VerificationInvalid, "", fmt.Errorf("name is missing")
	}
	value := systemName
	state := engine.VerificationSource
	if hasAttributeName {
		value = attributeName
		state = engine.VerificationTarget
	}
	name, ok := value.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return engine.VerificationInvalid, "", fmt.Errorf("name is not a non-empty string")
	}
	return state, name, nil
}

func transformAgentName(
	record agentNameRecord,
	direction agentNameDirection,
) (map[string]interface{}, map[string]interface{}, string, bool, error) {
	state, name, err := classifyAgentName(record)
	if err != nil {
		return nil, nil, "", false, err
	}
	if direction == agentNameForward {
		if state == engine.VerificationTarget {
			return record.attributes, record.systemAttributes, name, false, nil
		}
		record.attributes["name"] = name
		delete(record.systemAttributes, "name")
	} else {
		if state == engine.VerificationSource {
			return record.attributes, record.systemAttributes, name, false, nil
		}
		record.systemAttributes["name"] = name
		delete(record.attributes, "name")
	}
	return record.attributes, record.systemAttributes, name, true, nil
}

func agentNameBatchResult(
	checkpoint agentNameCheckpoint,
	completed bool,
) (engine.ResumableBatchResult, error) {
	checkpointData, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("encode agent name checkpoint: %w", err)
	}
	return engine.ResumableBatchResult{CheckpointData: checkpointData, Completed: completed}, nil
}

func agentTypeKey(deploymentID string, entityType string) string {
	return deploymentID + "\x00" + entityType
}

func agentIdentifierKey(deploymentID string, entityID string) string {
	return deploymentID + "\x00" + entityID
}
