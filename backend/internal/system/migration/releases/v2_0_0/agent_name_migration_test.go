// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type agentNameReadAccess struct {
	rows map[string][]engine.QueryRow
}

func (a agentNameReadAccess) Query(
	_ context.Context,
	_ engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	return a.rows[query.GetID()], nil
}

func (agentNameReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestDefaultAgentNameSchemaRoundTrip(t *testing.T) {
	schema := map[string]interface{}{
		"modelProvider": map[string]interface{}{"type": "string"},
	}
	systemAttributes := map[string]interface{}{"primaryContact": "owner"}

	targetSchema, targetSystemAttributes, changed, err := transformDefaultAgentNameSchema(
		schema, systemAttributes, defaultAgentNameSchemaForward)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "name", targetSystemAttributes["display"])
	nameDefinition := targetSchema["name"].(map[string]interface{})
	assert.Equal(t, "string", nameDefinition["type"])
	assert.Equal(t, true, nameDefinition["required"])
	assert.Equal(t, true, nameDefinition["unique"])

	sourceSchema, sourceSystemAttributes, changed, err := transformDefaultAgentNameSchema(
		targetSchema, targetSystemAttributes, defaultAgentNameSchemaRollback)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotContains(t, sourceSchema, "name")
	assert.NotContains(t, sourceSystemAttributes, "display")
	assert.Contains(t, sourceSchema, "modelProvider")
	assert.Equal(t, "owner", sourceSystemAttributes["primaryContact"])
}

func TestDefaultAgentNameSchemaRejectsConflictingDefinition(t *testing.T) {
	_, _, _, err := transformDefaultAgentNameSchema(
		map[string]interface{}{"name": map[string]interface{}{"type": "number"}},
		map[string]interface{}{"display": "name"},
		defaultAgentNameSchemaForward)
	require.Error(t, err)
}

func TestAgentNameRoundTrip(t *testing.T) {
	record := agentNameRecord{
		attributes: map[string]interface{}{"modelProvider": "openai"},
		systemAttributes: map[string]interface{}{
			"name": "Calendar Agent", "description": "Schedules meetings",
		},
	}
	attributes, systemAttributes, name, changed, err := transformAgentName(record, agentNameForward)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "Calendar Agent", name)
	assert.Equal(t, "Calendar Agent", attributes["name"])
	assert.NotContains(t, systemAttributes, "name")
	assert.Equal(t, "Schedules meetings", systemAttributes["description"])

	restoredAttributes, restoredSystemAttributes, _, changed, err := transformAgentName(agentNameRecord{
		attributes: attributes, systemAttributes: systemAttributes,
	}, agentNameRollback)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotContains(t, restoredAttributes, "name")
	assert.Equal(t, "Calendar Agent", restoredSystemAttributes["name"])
	assert.Equal(t, "openai", restoredAttributes["modelProvider"])
}

func TestAgentNameRejectsAmbiguousRepresentation(t *testing.T) {
	_, _, _, _, err := transformAgentName(agentNameRecord{
		attributes:       map[string]interface{}{"name": "Target"},
		systemAttributes: map[string]interface{}{"name": "Source"},
	}, agentNameForward)
	require.Error(t, err)
}

func TestAgentNameOperationsUseExpectedExecutionModes(t *testing.T) {
	schemaOperation := newMigrateDefaultAgentNameSchemaOperation()
	schemaDefinition := schemaOperation.Definition()
	assert.Equal(t, engine.StoreIDConfigDB, schemaDefinition.StoreID)
	assert.Equal(t, engine.InterruptionModeAtomic, schemaDefinition.InterruptionMode)
	assert.Implements(t, (*engine.AtomicOperation)(nil), schemaOperation)

	nameOperation := newMigrateAgentNamesToAttributesOperation(nil)
	nameDefinition := nameOperation.Definition()
	assert.Equal(t, engine.StoreIDEntityDB, nameDefinition.StoreID)
	assert.Equal(t, []engine.StoreID{engine.StoreIDConfigDB}, nameDefinition.ReadStoreIDs)
	assert.Equal(t, engine.InterruptionModeResumable, nameDefinition.InterruptionMode)
	assert.Implements(t, (*engine.ResumableOperation)(nil), nameOperation)
}

func TestAgentNameUpdateUsesCompareAndSetPredicates(t *testing.T) {
	assert.Contains(t, queryUpdateAgentNameRepresentation.PostgresQuery,
		"ATTRIBUTES IS NOT DISTINCT FROM $4")
	assert.Contains(t, queryUpdateAgentNameRepresentation.PostgresQuery,
		"SYSTEM_ATTRIBUTES IS NOT DISTINCT FROM $5")
	assert.Contains(t, queryUpdateAgentNameRepresentation.PostgresQuery,
		"DEPLOYMENT_ID = $6")
}

func TestAgentNameReadinessDefersUntilHandleColumnExists(t *testing.T) {
	operation := newMigrateAgentNamesToAttributesOperation(nil)
	result, err := operation.(engine.ReadinessChecker).CheckReadiness(
		context.Background(),
		agentNameReadAccess{rows: map[string][]engine.QueryRow{
			queryEntityTypeMigrationColumns.GetID(): entityTypeSourceColumnRows(),
		}},
		engine.PlanContext{},
	)

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, result.State)
	assert.Equal(t, []engine.OperationID{addEntityTypeHandleOperationID}, result.DeferredUntil)
}

func TestAgentNameReadinessBlocksNameCollisionsInvolvingAgents(t *testing.T) {
	operation := newMigrateAgentNamesToAttributesOperation(nil)
	result, err := operation.(engine.ReadinessChecker).CheckReadiness(
		context.Background(),
		agentNameReadAccess{rows: map[string][]engine.QueryRow{
			queryEntityTypeMigrationColumns.GetID(): entityTypeExpandedColumnRows(),
			queryEntityTypesForHandleBackfill.GetID(): {{
				"id": "agent-type", "name": "Default", "category": "agent", "handle": "default",
				"deployment_id": "deployment-a",
			}},
			queryDefaultAgentTypes.GetID(): {{
				"id": "agent-type", "deployment_id": "deployment-a",
				"schema_def":        `{"name":{"type":"string","displayName":"Name","required":true,"unique":true}}`,
				"system_attributes": `{"display":"name"}`,
			}},
			queryAgentNameCollisions.GetID(): {{
				"deployment_id": "deployment-a", "entity_count": int64(2),
			}},
		}},
		engine.PlanContext{},
	)

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, result.State)
	assert.Contains(t, result.Summary, "colliding name values")
	assert.Len(t, result.Findings, 1)
}

func TestAgentNameCollisionQueryIgnoresNonAgentOnlyGroups(t *testing.T) {
	assert.Contains(t, queryAgentNameCollisions.PostgresQuery,
		"COUNT(DISTINCT ID) > 1 AND BOOL_OR(CATEGORY = 'agent')")
}

func TestAgentNameSchemaSupportIncludesDeclarativeAgentTypes(t *testing.T) {
	declared := newDeclarativeEntityTypeSource(func() (DeclarativeEntityTypes, error) {
		return fakeDeclarativeEntityTypes{types: []DeclarativeEntityType{
			{
				Category: "agent", Handle: "assistant", DisplayName: "Assistant",
				SupportsNameAttribute: true,
			},
		}}, nil
	})

	supported, err := readAgentTypesSupportingName(
		context.Background(),
		agentNameReadAccess{rows: map[string][]engine.QueryRow{
			queryAgentTypeNameSchemas.GetID(): nil,
		}},
		"deployment-a",
		declared,
	)

	require.NoError(t, err)
	assert.True(t, supported[agentTypeKey("", "assistant")])
	assert.True(t, supported[agentTypeKey("", "Assistant")])
}
