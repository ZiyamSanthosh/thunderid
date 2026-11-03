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

const migrateDefaultAgentNameSchemaOperationID engine.OperationID = "2.0.0/configdb/48_migrate_default_agent_name_schema"

var (
	queryDefaultAgentTypes = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-36",
		PostgresQuery: `SELECT ID, SCHEMA_DEF, SYSTEM_ATTRIBUTES, DEPLOYMENT_ID
			FROM "ENTITY_TYPES"
			WHERE CATEGORY = 'agent' AND HANDLE = 'default'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryUpdateDefaultAgentTypeNameSchema = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-37",
		PostgresQuery: `UPDATE "ENTITY_TYPES"
			SET SCHEMA_DEF = $1, SYSTEM_ATTRIBUTES = $2
			WHERE ID = $3 AND DEPLOYMENT_ID = $4`,
	}
)

type defaultAgentNameSchemaDirection string

const (
	defaultAgentNameSchemaForward  defaultAgentNameSchemaDirection = "forward"
	defaultAgentNameSchemaRollback defaultAgentNameSchemaDirection = "rollback"
)

type defaultAgentNameSchemaOperation struct {
	id          engine.OperationID
	description string
	direction   defaultAgentNameSchemaDirection
}

type rollbackDefaultAgentNameSchemaOperation struct {
	*defaultAgentNameSchemaOperation
}

type defaultAgentTypeRecord struct {
	id               string
	deploymentID     string
	schema           map[string]interface{}
	systemAttributes map[string]interface{}
}

func newMigrateDefaultAgentNameSchemaOperation() engine.Operation {
	return &defaultAgentNameSchemaOperation{
		id:          migrateDefaultAgentNameSchemaOperationID,
		description: "Add the agent name to the default agent type schema",
		direction:   defaultAgentNameSchemaForward,
	}
}

func newRollbackDefaultAgentNameSchemaOperation() engine.RollbackOperation {
	return &rollbackDefaultAgentNameSchemaOperation{&defaultAgentNameSchemaOperation{
		id:          "2.0.0/configdb/48_migrate_default_agent_name_schema_rollback",
		description: "Restore the legacy default agent type schema",
		direction:   defaultAgentNameSchemaRollback,
	}}
}

func (*rollbackDefaultAgentNameSchemaOperation) ForwardOperationID() engine.OperationID {
	return migrateDefaultAgentNameSchemaOperationID
}

func (o *defaultAgentNameSchemaOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               o.id,
		Description:      o.description,
		StoreID:          engine.StoreIDConfigDB,
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeAtomic,
	}
}

func (*defaultAgentNameSchemaOperation) ResolveTargets(
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

func (o *defaultAgentNameSchemaOperation) CheckReadiness(
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
			Summary:       "Default agent schema migration depends on the entity-type handle column",
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
			Summary:       "Default agent schema migration depends on entity-type handles",
			DeferredUntil: []engine.OperationID{backfillEntityTypeHandleOperationID},
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
		State: engine.ReadinessReady, Summary: "The default agent type schema is ready to be migrated",
	}, nil
}

func (o *defaultAgentNameSchemaOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *defaultAgentNameSchemaOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	records, err := readDefaultAgentTypeRecords(ctx, execution.Access(), execution.Target().DeploymentID)
	if err != nil {
		return err
	}
	for _, record := range records {
		schema, systemAttributes, changed, err := transformDefaultAgentNameSchema(
			record.schema, record.systemAttributes, o.direction)
		if err != nil {
			return fmt.Errorf("transform default agent type %q: %w", record.id, err)
		}
		if !changed {
			continue
		}
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			return fmt.Errorf("encode default agent schema: %w", err)
		}
		systemAttributesJSON, err := json.Marshal(systemAttributes)
		if err != nil {
			return fmt.Errorf("encode default agent system attributes: %w", err)
		}
		if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB,
			queryUpdateDefaultAgentTypeNameSchema, string(schemaJSON), string(systemAttributesJSON),
			record.id, record.deploymentID); err != nil {
			return fmt.Errorf("update default agent type %q: %w", record.id, err)
		}
	}
	return nil
}

func (o *defaultAgentNameSchemaOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *defaultAgentNameSchemaOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	records, err := readDefaultAgentTypeRecords(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	sourceCount := 0
	for _, record := range records {
		state, err := classifyDefaultAgentNameSchema(record.schema, record.systemAttributes)
		if err != nil {
			return invalidAgentNameMigrationVerification(
				fmt.Sprintf("Default agent type %q is incompatible: %v", record.id, err)), nil
		}
		if state == engine.VerificationSource {
			sourceCount++
		}
	}
	if o.direction == defaultAgentNameSchemaForward {
		if sourceCount > 0 {
			return engine.VerificationResult{
				State: engine.VerificationSource, Summary: "The default agent type still uses the legacy schema",
			}, nil
		}
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "The default agent type declares name as its display attribute",
		}, nil
	}
	if sourceCount == len(records) {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "The legacy default agent type schema has been restored",
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationSource, Summary: "The target default agent type schema requires rollback",
	}, nil
}

func readDefaultAgentTypeRecords(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) ([]defaultAgentTypeRecord, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryDefaultAgentTypes, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect default agent type schemas: %w", err)
	}
	records := make([]defaultAgentTypeRecord, 0, len(rows))
	for _, row := range rows {
		id, err := row.String("id")
		if err != nil {
			return nil, err
		}
		rowDeploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		schema, err := readAgentNameJSONObject(row, "schema_def")
		if err != nil {
			return nil, fmt.Errorf("read default agent type %q schema: %w", id, err)
		}
		systemAttributes, err := readAgentNameJSONObject(row, "system_attributes")
		if err != nil {
			return nil, fmt.Errorf("read default agent type %q system attributes: %w", id, err)
		}
		records = append(records, defaultAgentTypeRecord{
			id: id, deploymentID: rowDeploymentID, schema: schema, systemAttributes: systemAttributes,
		})
	}
	return records, nil
}

func transformDefaultAgentNameSchema(
	schema map[string]interface{},
	systemAttributes map[string]interface{},
	direction defaultAgentNameSchemaDirection,
) (map[string]interface{}, map[string]interface{}, bool, error) {
	state, err := classifyDefaultAgentNameSchema(schema, systemAttributes)
	if err != nil {
		return nil, nil, false, err
	}
	if direction == defaultAgentNameSchemaForward && state == engine.VerificationTarget ||
		direction == defaultAgentNameSchemaRollback && state == engine.VerificationSource {
		return schema, systemAttributes, false, nil
	}
	if direction == defaultAgentNameSchemaForward {
		schema["name"] = map[string]interface{}{
			"type": "string", "displayName": "Name", "required": true, "unique": true,
		}
		systemAttributes["display"] = "name"
	} else {
		delete(schema, "name")
		delete(systemAttributes, "display")
	}
	return schema, systemAttributes, true, nil
}

func classifyDefaultAgentNameSchema(
	schema map[string]interface{},
	systemAttributes map[string]interface{},
) (engine.VerificationState, error) {
	name, hasName := schema["name"]
	display, hasDisplay := systemAttributes["display"]
	if !hasName && !hasDisplay {
		return engine.VerificationSource, nil
	}
	nameDefinition, ok := name.(map[string]interface{})
	if !hasName || !ok || nameDefinition["type"] != "string" ||
		nameDefinition["displayName"] != "Name" || nameDefinition["required"] != true ||
		nameDefinition["unique"] != true || !hasDisplay || display != "name" {
		return engine.VerificationInvalid, fmt.Errorf("name schema or display attribute conflicts with the target definition")
	}
	return engine.VerificationTarget, nil
}

func readAgentNameJSONObject(row engine.QueryRow, column string) (map[string]interface{}, error) {
	payload, err := readEntityTypeReferencePayload(row, column, true)
	if err != nil {
		return nil, err
	}
	if payload == "" || payload == "null" {
		return map[string]interface{}{}, nil
	}
	var value map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", column, err)
	}
	if value == nil {
		return map[string]interface{}{}, nil
	}
	return value, nil
}

func invalidAgentNameMigrationVerification(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State:   engine.VerificationInvalid,
		Summary: summary,
		Findings: []engine.Finding{{
			Code:        "invalid_agent_name_migration_state",
			Message:     summary,
			Remediation: "Reconcile agent names and agent type schemas before retrying",
		}},
	}
}
