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

func userTypeRow(name, handle, schema string) engine.QueryRow {
	return engine.QueryRow{
		"name": name, "handle": handle, "deployment_id": testDeployment, "schema_def": schema,
	}
}

// removalAccess builds read access for operation 52. The entity type columns and handle backfill queries
// answer the readiness prerequisites with the schema of a finished Expand.
func removalAccess(users, identifiers, userTypes []engine.QueryRow) agentNameReadAccess {
	return agentNameReadAccess{rows: map[string][]engine.QueryRow{
		queryLegacySubjectUsers.GetID():           users,
		queryLinkedSubjectIdentifiers.GetID():     identifiers,
		queryUserTypeSubDeclarations.GetID():      userTypes,
		queryEntityTypeMigrationColumns.GetID():   entityTypeColumnRows(true),
		queryEntityTypesForHandleBackfill.GetID(): entityTypeHandleRows("customer"),
	}}
}

func entityTypeColumnRows(withHandle bool) []engine.QueryRow {
	rows := []engine.QueryRow{{
		"column_name": "NAME", "is_nullable": "NO", "data_type": "character varying", "maximum_length": int64(100),
	}}
	if withHandle {
		rows = append(rows, engine.QueryRow{
			"column_name": "HANDLE", "is_nullable": "YES", "data_type": "character varying",
			"maximum_length": int64(100),
		})
	}
	return rows
}

func entityTypeHandleRows(handle string) []engine.QueryRow {
	row := engine.QueryRow{
		"id": "type-1", "name": "Customer", "category": "user", "deployment_id": testDeployment,
	}
	if handle != "" {
		row["handle"] = handle
	}
	return []engine.QueryRow{row}
}

func removalOperation(declared ...DeclarativeEntityType) engine.Operation {
	var provider func() (DeclarativeEntityTypes, error)
	if declared != nil {
		provider = func() (DeclarativeEntityTypes, error) {
			return fakeDeclarativeEntityTypes{types: declared}, nil
		}
	}
	return newRemoveLegacySubjectAttributeOperation(
		googleAndGitHubConnections(), newDeclarativeEntityTypeSource(provider))
}

func TestLegacySubjectRemovalVerification(t *testing.T) {
	const schemaWithoutSub = `{"email":{"type":"string"}}`
	const schemaWithSub = `{"email":{"type":"string"},"sub":{"type":"string"}}`
	linkedUser := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`,
		linkedSystemAttributes(testGoogleConnectionID, testGoogleSubject))
	unlinkedUser := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`, `{}`)
	identifier := linkedIdentifierRow("user-1", testGoogleConnectionID, testGoogleSubject, "system")

	tests := []struct {
		name        string
		users       []engine.QueryRow
		identifiers []engine.QueryRow
		userTypes   []engine.QueryRow
		want        engine.VerificationState
	}{
		{name: "no user holds sub is the target", userTypes: []engine.QueryRow{
			userTypeRow("Customer", "customer", schemaWithoutSub)}, want: engine.VerificationTarget},
		{name: "a linked user whose type omits sub is the source",
			users: []engine.QueryRow{linkedUser}, identifiers: []engine.QueryRow{identifier},
			userTypes: []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithoutSub)},
			want:      engine.VerificationSource},
		{name: "a user whose type declares sub keeps it",
			users: []engine.QueryRow{linkedUser}, identifiers: []engine.QueryRow{identifier},
			userTypes: []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithSub)},
			want:      engine.VerificationTarget},
		{name: "a user whose type is unknown keeps sub",
			users: []engine.QueryRow{linkedUser}, identifiers: []engine.QueryRow{identifier},
			userTypes: []engine.QueryRow{userTypeRow("Other", "other", schemaWithoutSub)},
			want:      engine.VerificationTarget},
		{name: "an unlinked user is invalid because removal would lose the identity",
			users:     []engine.QueryRow{unlinkedUser},
			userTypes: []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithoutSub)},
			want:      engine.VerificationInvalid},
		{name: "a link without an identifier row is invalid",
			users:     []engine.QueryRow{linkedUser},
			userTypes: []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithoutSub)},
			want:      engine.VerificationInvalid},
		{name: "a subject that fits no provider is invalid",
			users:     []engine.QueryRow{legacyUserRow("user-1", `{"sub":"abc"}`, `{}`)},
			userTypes: []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithoutSub)},
			want:      engine.VerificationInvalid},
		{name: "a handle wins over a name that equals it",
			users: []engine.QueryRow{linkedUser}, identifiers: []engine.QueryRow{identifier},
			userTypes: []engine.QueryRow{
				userTypeRow("customer", "legacy-customer", schemaWithSub),
				userTypeRow("Customer", "customer", schemaWithoutSub),
			},
			want: engine.VerificationSource},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := removalOperation().PreVerify(context.Background(),
				removalAccess(tt.users, tt.identifiers, tt.userTypes), engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.State, result.Summary)
		})
	}
}

func TestLegacySubjectRemovalUsesDeclarativeUserTypes(t *testing.T) {
	linkedUser := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`,
		linkedSystemAttributes(testGoogleConnectionID, testGoogleSubject))
	identifier := linkedIdentifierRow("user-1", testGoogleConnectionID, testGoogleSubject, "system")

	for name, test := range map[string]struct {
		declared DeclarativeEntityType
		want     engine.VerificationState
	}{
		"a declarative type that omits sub": {
			declared: DeclarativeEntityType{Category: "user", Handle: "customer", DisplayName: "Customer"},
			want:     engine.VerificationSource,
		},
		"a declarative type that declares sub": {
			declared: DeclarativeEntityType{
				Category: "user", Handle: "customer", DisplayName: "Customer", DeclaresSubAttribute: true,
			},
			want: engine.VerificationTarget,
		},
		"a declarative agent type is ignored": {
			declared: DeclarativeEntityType{Category: "agent", Handle: "customer", DisplayName: "Customer"},
			want:     engine.VerificationTarget,
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := removalOperation(test.declared).PreVerify(context.Background(),
				removalAccess([]engine.QueryRow{linkedUser}, []engine.QueryRow{identifier}, nil),
				engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.want, result.State, result.Summary)
		})
	}
}

func TestLegacySubjectRemovalReadiness(t *testing.T) {
	const schemaWithoutSub = `{"email":{"type":"string"}}`
	userTypes := []engine.QueryRow{userTypeRow("Customer", "customer", schemaWithoutSub)}
	linkedUser := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`,
		linkedSystemAttributes(testGoogleConnectionID, testGoogleSubject))
	unlinkedUser := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`, `{}`)
	identifier := linkedIdentifierRow("user-1", testGoogleConnectionID, testGoogleSubject, "system")
	checker, ok := removalOperation().(engine.ReadinessChecker)
	require.True(t, ok)

	t.Run("is ready when every removable user is linked", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(),
			removalAccess([]engine.QueryRow{linkedUser}, []engine.QueryRow{identifier}, userTypes),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessReady, result.State)
	})

	t.Run("is ready when no user holds sub", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(), removalAccess(nil, nil, userTypes),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessReady, result.State)
	})

	t.Run("waits for the link operations when a user is not linked yet", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(),
			removalAccess([]engine.QueryRow{unlinkedUser}, nil, userTypes), engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessDeferred, result.State)
		assert.Equal(t, []engine.OperationID{
			linkLegacyFederatedSubjectsOperationID, linkLegacyFederatedSubjectsCatchUpOperationID,
		}, result.DeferredUntil)
	})

	t.Run("is blocked for a subject that cannot be classified", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(), removalAccess(
			[]engine.QueryRow{legacyUserRow("user-1", `{"sub":"abc"}`, `{}`)}, nil, userTypes),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessBlocked, result.State)
		require.Len(t, result.Findings, 1)
		assert.NotContains(t, result.Findings[0].Message, "abc")
	})

	t.Run("waits for the entity-type handle column", func(t *testing.T) {
		access := removalAccess(nil, nil, userTypes)
		access.rows[queryEntityTypeMigrationColumns.GetID()] = entityTypeColumnRows(false)
		result, err := checker.CheckReadiness(context.Background(), access, engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessDeferred, result.State)
		assert.Equal(t, []engine.OperationID{addEntityTypeHandleOperationID}, result.DeferredUntil)
	})

	t.Run("waits for the entity-type handle backfill", func(t *testing.T) {
		access := removalAccess(nil, nil, userTypes)
		access.rows[queryEntityTypesForHandleBackfill.GetID()] = entityTypeHandleRows("")
		result, err := checker.CheckReadiness(context.Background(), access, engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessDeferred, result.State)
		assert.Equal(t, []engine.OperationID{backfillEntityTypeHandleOperationID}, result.DeferredUntil)
	})
}

func TestLegacySubjectRemovable(t *testing.T) {
	declarations := map[string]bool{
		userTypeKey(testDeployment, "customer"): false,
		userTypeKey(testDeployment, "keeps"):    true,
		userTypeKey("", "declared"):             false,
	}
	record := func(entityType string) legacySubjectRecord {
		return legacySubjectRecord{deploymentID: testDeployment, entityType: entityType}
	}

	assert.True(t, legacySubjectRemovable(record("customer"), declarations))
	assert.False(t, legacySubjectRemovable(record("keeps"), declarations))
	assert.True(t, legacySubjectRemovable(record("declared"), declarations))
	assert.False(t, legacySubjectRemovable(record("unknown"), declarations))
}

func TestRemoveLegacySubjectAttributeDefinition(t *testing.T) {
	operation := removalOperation()
	definition := operation.Definition()

	assert.Equal(t, removeLegacySubjectAttributeOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
	assert.Equal(t, []engine.StoreID{engine.StoreIDConfigDB}, definition.ReadStoreIDs)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)
	assert.Implements(t, (*engine.ResumableOperation)(nil), operation)
}
