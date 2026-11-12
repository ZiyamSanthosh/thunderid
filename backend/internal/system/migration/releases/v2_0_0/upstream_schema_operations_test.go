// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestProjectScopedTablesHaveUniqueOperationsAndIndexNames(t *testing.T) {
	ids := map[engine.OperationID]bool{}
	indexes := map[string]bool{}
	for _, scoped := range projectScopedTables {
		for _, id := range []engine.OperationID{scoped.columnID, scoped.indexID} {
			assert.False(t, ids[id], "operation id %s is used twice", id)
			ids[id] = true
		}
		assert.False(t, indexes[scoped.indexName], "index %s is used twice", scoped.indexName)
		indexes[scoped.indexName] = true
	}
	assert.Len(t, projectScopedTables, 8)
}

func TestAddProjectIDColumnOperations(t *testing.T) {
	for _, scoped := range projectScopedTables {
		t.Run(scoped.slug, func(t *testing.T) {
			operation, err := newAddProjectIDColumnOperation(scoped)
			require.NoError(t, err)
			definition := operation.Definition()
			assert.Equal(t, scoped.columnID, definition.ID)
			assert.Equal(t, scoped.storeID, definition.StoreID)
			assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
			implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
			require.NoError(t, err)
			require.Len(t, implementation.Statements, 2)
			assert.Contains(t, implementation.Statements[1].Query, "ADD COLUMN PROJECT_ID VARCHAR(36)")
			assert.NotContains(t, implementation.Statements[1].Query, "NOT NULL")

			index := newAddProjectIndexOperation(scoped).(*onlineIndexOperation)
			assert.Equal(t, scoped.indexID, index.Definition().ID)
			assert.Equal(t, scoped.table, index.spec.table)
			assert.Contains(t, index.spec.createSQL, "CREATE INDEX CONCURRENTLY")
		})
	}
}

func TestAddAgentTypeOrganizationUnitIndexBlocksOnDuplicates(t *testing.T) {
	operation := newAddAgentTypeOrganizationUnitIndexOperation().(*onlineIndexOperation)
	assert.True(t, operation.spec.unique)
	assert.True(t, operation.spec.partial)
	assert.Contains(t, operation.spec.createSQL, "WHERE CATEGORY = 'agent'")

	const absent = ""
	clean := newFakeAccess().relations("ENTITY_TYPES").on(queryOnlineIndex).
		on(queryOrganizationUnitsWithSeveralAgentTypes)
	ready, err := operation.CheckReadiness(context.Background(), clean, engine.PlanContext{})
	require.NoError(t, err, absent)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	duplicated := newFakeAccess().relations("ENTITY_TYPES").on(queryOnlineIndex).
		on(queryOrganizationUnitsWithSeveralAgentTypes, engine.QueryRow{
			"deployment_id": "dep", "ou_id": "ou-1", "agent_types": int64(2),
		})
	blocked, err := operation.CheckReadiness(context.Background(), duplicated, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.Equal(t, "several_agent_types_in_organization_unit", blocked.Findings[0].Code)
	assert.Contains(t, blocked.Findings[0].Message, "ou-1")

	// Once the index exists the duplicates check is not asked for.
	const existing = `CREATE UNIQUE INDEX idx_entity_types_agent_deployment_ou ON public."ENTITY_TYPES" USING btree ` +
		`(deployment_id, ou_id) WHERE ((category)::text = 'agent'::text)`
	built := newFakeAccess().relations("ENTITY_TYPES").on(queryOnlineIndex, engine.QueryRow{
		"is_valid": true, "is_ready": true, "is_unique": true, "has_predicate": true,
		"table_name": "ENTITY_TYPES", "definition": existing,
	})
	done, err := operation.PreVerify(context.Background(), built, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, done.State)

	failing := newFakeAccess().relations("ENTITY_TYPES").on(queryOnlineIndex)
	failing.err = errors.New("database unavailable")
	_, err = checkOneAgentTypePerOrganizationUnit(context.Background(), failing)
	assert.ErrorContains(t, err, "database unavailable")
}

func ownerCount(count int64) engine.QueryRow { return engine.QueryRow{"owners": count} }

func TestConvertAgentOwnerOperation(t *testing.T) {
	operation, err := newConvertAgentOwnerOperation()
	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, convertAgentOwnerOperationID, definition.ID)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
	assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)

	sqlOperation := operation.(*engine.SQLOperation)
	implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	query := implementation.Statements[0].Query
	assert.Contains(t, query, "'idType', 'uuid', 'isLocal', TRUE")
	assert.Contains(t, query, "JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'string'")
	args, err := implementation.Statements[0].Args(engine.ExecutionTarget{DeploymentID: "dep"})
	require.NoError(t, err)
	assert.Equal(t, []interface{}{"dep"}, args)

	targets, err := sqlOperation.ResolveTargets(context.Background(), newFakeAccess(),
		engine.MigrationSelection{DeploymentIDs: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, []engine.ExecutionTarget{{DeploymentID: "a"}}, targets)

	source, err := operation.PreVerify(context.Background(),
		newFakeAccess().on(queryLegacyAgentOwners, ownerCount(2)), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)
	target, err := operation.PostVerify(context.Background(),
		newFakeAccess().on(queryLegacyAgentOwners, ownerCount(0)), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	_, err = operation.PreVerify(context.Background(), newFakeAccess().on(queryLegacyAgentOwners),
		engine.ExecutionTarget{})
	assert.ErrorContains(t, err, "no result")
	failing := newFakeAccess()
	failing.err = errors.New("database unavailable")
	_, err = operation.PreVerify(context.Background(), failing, engine.ExecutionTarget{})
	assert.ErrorContains(t, err, "database unavailable")
}

func TestRollbackConvertAgentOwnerOperation(t *testing.T) {
	rollback, err := newRollbackConvertAgentOwnerOperation()
	require.NoError(t, err)
	assert.Equal(t, convertAgentOwnerOperationID, rollback.ForwardOperationID())
	assert.Equal(t, convertAgentOwnerRollbackOperationID, rollback.Definition().ID)

	implementation, err := rollback.(*rollbackSQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Contains(t, implementation.Statements[0].Query, "TO_JSONB(SYSTEM_ATTRIBUTES #>> '{owner,id}')")

	access := func(unrevertible, revertible int64) *fakeAccess {
		return newFakeAccess().on(queryUnrevertibleAgentOwners, ownerCount(unrevertible)).
			on(queryRevertibleAgentOwners, ownerCount(revertible))
	}
	source, err := rollback.PreVerify(context.Background(), access(0, 3), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)
	target, err := rollback.PostVerify(context.Background(), access(0, 0), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
	invalid, err := rollback.PreVerify(context.Background(), access(1, 3), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, invalid.State)
	require.Len(t, invalid.Findings, 1)
	assert.Equal(t, "agent_owner_not_representable", invalid.Findings[0].Code)
}

func TestAddSessionPropertiesOperation(t *testing.T) {
	operation, err := newAddSessionPropertiesOperation()
	require.NoError(t, err)
	assert.Equal(t, addSessionPropertiesOperationID, operation.Definition().ID)
	assert.Equal(t, engine.StoreIDRuntimePersistent, operation.Definition().StoreID)
	sqlOperation := operation.(*engine.SQLOperation)
	implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Contains(t, implementation.Statements[1].Query, "ADD COLUMN PROPERTIES JSONB")

	renamed := func(columns ...engine.QueryRow) *fakeAccess {
		return newFakeAccess().relations("SESSION").on(querySessionRelations,
			engine.QueryRow{"relation_name": sessionTable, "relation_kind": relationKindTable},
			engine.QueryRow{"relation_name": sessionParticipantTable, "relation_kind": relationKindTable},
		).on(queryTableColumns, columns...)
	}
	source, err := operation.PreVerify(context.Background(), renamed(), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)
	target, err := operation.PostVerify(context.Background(), renamed(columnRow(sessionPropertiesColumn)),
		engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)

	waiting := newFakeAccess().relations("SSO_SESSION").on(querySessionRelations,
		engine.QueryRow{"relation_name": legacySessionTable, "relation_kind": relationKindTable},
		engine.QueryRow{"relation_name": legacySessionParticipantTable, "relation_kind": relationKindTable})
	deferred, err := sqlOperation.CheckReadiness(context.Background(), waiting, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	assert.Equal(t, []engine.OperationID{renameSessionTablesOperationID}, deferred.DeferredUntil)

	ready, err := sqlOperation.CheckReadiness(context.Background(), renamed(), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	unrenamedOther := newFakeAccess().on(querySessionRelations)
	blocked, err := sqlOperation.CheckReadiness(context.Background(), unrenamedOther, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
}
