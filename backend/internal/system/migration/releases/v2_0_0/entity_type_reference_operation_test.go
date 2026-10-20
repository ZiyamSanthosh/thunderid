// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestEntityTypeReferenceReadinessDefersUntilHandleColumnExists(t *testing.T) {
	operation := newMigrateInboundClientEntityTypeReferencesOperation(nil)
	result, err := operation.(engine.ReadinessChecker).CheckReadiness(
		context.Background(),
		authorizationPDPReadAccess{rows: map[string][]engine.QueryRow{
			queryEntityTypeMigrationColumns.GetID(): entityTypeSourceColumnRows(),
		}},
		engine.PlanContext{},
	)

	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, result.State)
	assert.Equal(t, []engine.OperationID{addEntityTypeHandleOperationID}, result.DeferredUntil)
}

func TestEntityTypeReferenceMappingUsesPersistedHandles(t *testing.T) {
	mapping := testEntityTypeReferenceMapping()

	handle, changed, err := mapping.translate(
		"deployment-a", "user", "Legacy Person", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "legacy-person", handle)

	collidingHandle, changed, err := mapping.translate(
		"deployment-a", "user", "Legacy-Person", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "legacy-person_1", collidingHandle)

	name, changed, err := mapping.translate(
		"deployment-a", "user", "legacy-person", entityTypeReferenceRollback)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "Legacy Person", name)
}

func TestTransformInboundClientEntityTypeReferences(t *testing.T) {
	mapping := testEntityTypeReferenceMapping()
	source := `{
		"allowedUserTypes":["Legacy Person","Customer"],
		"allowedAgentTypes":["Legacy Agent"],
		"subjectAttribute":{"Legacy Person":"email","Customer":"username"},
		"unrelated":true
	}`
	target := `{
		"allowedUserTypes":["legacy-person","customer"],
		"allowedAgentTypes":["legacy-agent"],
		"subjectAttribute":{"legacy-person":"email","customer":"username"},
		"unrelated":true
	}`

	assertEntityTypeReferenceRoundTrip(t, source, target, func(payload string,
		direction entityTypeReferenceDirection,
	) (string, bool, error) {
		return transformInboundClientEntityTypeReferences(payload, "deployment-a", mapping, direction)
	})
}

func TestTransformIDPEntityTypeReferences(t *testing.T) {
	mapping := testEntityTypeReferenceMapping()
	source := `{
		"userTypeResolution":{"default":"Legacy Person","valueMapping":{"employee":"Customer"}},
		"userTypeAttributeMappings":[{"userType":"Legacy Person","attributes":[]}]
	}`
	target := `{
		"userTypeResolution":{"default":"legacy-person","valueMapping":{"employee":"customer"}},
		"userTypeAttributeMappings":[{"userType":"legacy-person","attributes":[]}]
	}`

	assertEntityTypeReferenceRoundTrip(t, source, target, func(payload string,
		direction entityTypeReferenceDirection,
	) (string, bool, error) {
		return transformIDPEntityTypeReferences(payload, "deployment-a", mapping, direction)
	})
}

func TestTransformFlowEntityTypeReferences(t *testing.T) {
	mapping := testEntityTypeReferenceMapping()
	source := `[
		{"id":"user","executor":{"name":"UserTypeResolver"},"properties":{"allowedUserTypes":["Legacy Person"]}},
		{"id":"agent","executor":{"name":"AgentTypeResolver"},"properties":{"allowedAgentTypes":["Legacy Agent"]}},
		{"id":"other","executor":{"name":"Other"},"properties":{"allowedUserTypes":["Legacy Person"]}}
	]`
	target := `[
		{"id":"user","executor":{"name":"UserTypeResolver"},"properties":{"allowedUserTypes":["legacy-person"]}},
		{"id":"agent","executor":{"name":"AgentTypeResolver"},"properties":{"allowedAgentTypes":["legacy-agent"]}},
		{"id":"other","executor":{"name":"Other"},"properties":{"allowedUserTypes":["Legacy Person"]}}
	]`

	assertEntityTypeReferenceRoundTrip(t, source, target, func(payload string,
		direction entityTypeReferenceDirection,
	) (string, bool, error) {
		return transformFlowEntityTypeReferences(payload, "deployment-a", mapping, direction)
	})
}

func TestEntityTypeReferenceOperationsAreResumableAndUseTheExpectedStores(t *testing.T) {
	tests := []struct {
		operation  engine.Operation
		store      engine.StoreID
		readStores []engine.StoreID
	}{
		{newMigrateEntityTypeReferencesOperation(nil), engine.StoreIDEntityDB, []engine.StoreID{engine.StoreIDConfigDB}},
		{newMigrateInboundClientEntityTypeReferencesOperation(nil), engine.StoreIDConfigDB, nil},
		{newMigrateIDPEntityTypeReferencesOperation(nil), engine.StoreIDConfigDB, nil},
		{newMigrateFlowEntityTypeReferencesOperation(nil), engine.StoreIDConfigDB, nil},
	}

	for _, test := range tests {
		definition := test.operation.Definition()
		assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
		assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)
		assert.Equal(t, test.store, definition.StoreID)
		assert.Equal(t, test.readStores, definition.ReadStoreIDs)
		assert.Implements(t, (*engine.ResumableOperation)(nil), test.operation)
		assert.Implements(t, (*engine.ReadinessChecker)(nil), test.operation)
	}
}

func TestEntityTypeReferenceUpdatesUseCompareAndSetPredicates(t *testing.T) {
	tests := []struct {
		name              string
		config            entityTypeReferenceOperationConfig
		row               engine.QueryRow
		expectedPredicate string
		expectedArgs      []interface{}
	}{
		{
			name: "entity", config: entityTypeReferencesOperationConfig(
				migrateEntityTypeReferencesOperationID, "test", entityTypeReferenceForward),
			row:               engine.QueryRow{"id": "entity-id", "deployment_id": "deployment-a"},
			expectedPredicate: "TYPE = $3",
			expectedArgs:      []interface{}{"target", "entity-id", "source", "deployment-a"},
		},
		{
			name: "inbound client", config: inboundClientEntityTypeReferencesOperationConfig(
				migrateInboundClientEntityTypeReferencesOperationID, "test", entityTypeReferenceForward),
			row:               engine.QueryRow{"id": "client-id", "deployment_id": "deployment-a"},
			expectedPredicate: "PROPERTIES = $3",
			expectedArgs:      []interface{}{"target", "client-id", "source", "deployment-a"},
		},
		{
			name: "identity provider", config: idpEntityTypeReferencesOperationConfig(
				migrateIDPEntityTypeReferencesOperationID, "test", entityTypeReferenceForward),
			row:               engine.QueryRow{"id": "idp-id", "deployment_id": "deployment-a"},
			expectedPredicate: "ATTRIBUTE_CONFIGURATION = $3",
			expectedArgs:      []interface{}{"target", "idp-id", "source", "deployment-a"},
		},
		{
			name: "flow", config: flowEntityTypeReferencesOperationConfig(
				migrateFlowEntityTypeReferencesOperationID, "test", entityTypeReferenceForward),
			row:               engine.QueryRow{"id": "flow-id", "version": int64(2), "deployment_id": "deployment-a"},
			expectedPredicate: "NODES = $4",
			expectedArgs:      []interface{}{"target", "flow-id", int64(2), "source", "deployment-a"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Contains(t, test.config.updateQuery.PostgresQuery, test.expectedPredicate)
			args, err := test.config.updateArgs("target", "source", test.row)
			require.NoError(t, err)
			assert.Equal(t, test.expectedArgs, args)
		})
	}
}

func assertEntityTypeReferenceRoundTrip(
	t *testing.T,
	source string,
	target string,
	transform func(string, entityTypeReferenceDirection) (string, bool, error),
) {
	t.Helper()

	updated, changed, err := transform(source, entityTypeReferenceForward)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, target, updated)

	restored, changed, err := transform(updated, entityTypeReferenceRollback)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, source, restored)
}

func testEntityTypeReferenceMapping() entityTypeReferenceMapping {
	return entityTypeReferenceMapping{
		byName: map[string]string{
			"deployment-a\x00user\x00Legacy Person": "legacy-person",
			"deployment-a\x00user\x00Legacy-Person": "legacy-person_1",
			"deployment-a\x00user\x00Customer":      "customer",
			"deployment-a\x00agent\x00Legacy Agent": "legacy-agent",
		},
		byHandle: map[string]string{
			"deployment-a\x00user\x00legacy-person":   "Legacy Person",
			"deployment-a\x00user\x00legacy-person_1": "Legacy-Person",
			"deployment-a\x00user\x00customer":        "Customer",
			"deployment-a\x00agent\x00legacy-agent":   "Legacy Agent",
		},
	}
}
