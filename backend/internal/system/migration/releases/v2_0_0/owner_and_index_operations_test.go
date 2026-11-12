// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type fixedOrganizationUnits struct {
	units []DeclarativeOrganizationUnit
	err   error
}

func (f fixedOrganizationUnits) ListDeclarativeOrganizationUnits(context.Context) (
	[]DeclarativeOrganizationUnit, error) {
	return f.units, f.err
}

const testSystemOrganizationUnitID = "01900000-0000-7000-8000-000000000001"

func organizationUnitSource(units ...DeclarativeOrganizationUnit) *declarativeOrganizationUnitSource {
	return newDeclarativeOrganizationUnitSource(func() (DeclarativeOrganizationUnits, error) {
		return fixedOrganizationUnits{units: units}, nil
	}, testSystemOrganizationUnitID)
}

func columnRow(column columnSpec) engine.QueryRow {
	return engine.QueryRow{
		"column_name": column.name, "is_nullable": column.nullable, "data_type": column.dataType,
		"maximum_length": column.maximumLength, "column_default": column.defaultValue,
	}
}

func countRow(name string, count int64) engine.QueryRow {
	return engine.QueryRow{name: count}
}

func TestDeclarativeOrganizationUnitSource(t *testing.T) {
	t.Run("finds the system organization unit", func(t *testing.T) {
		source := organizationUnitSource(
			DeclarativeOrganizationUnit{ID: "other", Handle: "other"},
			DeclarativeOrganizationUnit{ID: testSystemOrganizationUnitID, Handle: "system"})
		unit, found, err := source.systemOrganizationUnit(context.Background())
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "system", unit.Handle)
	})
	t.Run("reports an undeclared system organization unit", func(t *testing.T) {
		_, found, err := organizationUnitSource(DeclarativeOrganizationUnit{ID: "other"}).
			systemOrganizationUnit(context.Background())
		require.NoError(t, err)
		assert.False(t, found)
	})
	t.Run("a nil source lists nothing", func(t *testing.T) {
		var source *declarativeOrganizationUnitSource
		_, found, err := source.systemOrganizationUnit(context.Background())
		require.NoError(t, err)
		assert.False(t, found)
	})
	t.Run("provider and lister failures are returned", func(t *testing.T) {
		failing := newDeclarativeOrganizationUnitSource(func() (DeclarativeOrganizationUnits, error) {
			return nil, errors.New("no resources")
		}, testSystemOrganizationUnitID)
		_, _, err := failing.systemOrganizationUnit(context.Background())
		assert.ErrorContains(t, err, "no resources")
		listing := newDeclarativeOrganizationUnitSource(func() (DeclarativeOrganizationUnits, error) {
			return fixedOrganizationUnits{err: errors.New("unreadable")}, nil
		}, testSystemOrganizationUnitID)
		_, _, err = listing.systemOrganizationUnit(context.Background())
		assert.ErrorContains(t, err, "unreadable")
	})
}

func TestSystemOrganizationUnitReadiness(t *testing.T) {
	absent := newFakeAccess().on(queryOrganizationUnitExists, countRow("units", 0))
	present := newFakeAccess().on(queryOrganizationUnitExists, countRow("units", 1))

	declared := organizationUnitSource(DeclarativeOrganizationUnit{ID: testSystemOrganizationUnitID})
	ready, err := systemOrganizationUnitReadiness(context.Background(), absent, declared)
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State, "a declared unit needs no database row")

	inDatabase, err := systemOrganizationUnitReadiness(context.Background(), present, organizationUnitSource())
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, inDatabase.State, "a unit in the database needs no declaration")

	blocked, err := systemOrganizationUnitReadiness(context.Background(), absent, organizationUnitSource())
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, blocked.State)
	require.Len(t, blocked.Findings, 1)
	assert.Equal(t, "system_organization_unit_not_found", blocked.Findings[0].Code)
	assert.Contains(t, blocked.Findings[0].Remediation, testSystemOrganizationUnitID)

	failing := newDeclarativeOrganizationUnitSource(func() (DeclarativeOrganizationUnits, error) {
		return nil, errors.New("no resources")
	}, testSystemOrganizationUnitID)
	_, err = systemOrganizationUnitReadiness(context.Background(), absent, failing)
	assert.ErrorContains(t, err, "no resources")

	broken := newFakeAccess()
	broken.err = errors.New("database unavailable")
	_, err = systemOrganizationUnitReadiness(context.Background(), broken, organizationUnitSource())
	assert.ErrorContains(t, err, "database unavailable")

	unconfigured := newDeclarativeOrganizationUnitSource(func() (DeclarativeOrganizationUnits, error) {
		return fixedOrganizationUnits{units: []DeclarativeOrganizationUnit{{ID: testSystemOrganizationUnitID}}}, nil
	}, "")
	notConfigured, err := systemOrganizationUnitReadiness(context.Background(), present, unconfigured)
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, notConfigured.State)
	require.Len(t, notConfigured.Findings, 1)
	assert.Equal(t, "system_organization_unit_not_configured", notConfigured.Findings[0].Code)
	assert.Contains(t, notConfigured.Findings[0].Remediation, "server.system_ou_id")
}

func ownerAccess(owner organizationUnitOwnerTable, column *columnSpec, missing int64) *fakeAccess {
	access := newFakeAccess().relations(owner.table)
	rows := []engine.QueryRow{}
	if column != nil {
		rows = append(rows, columnRow(*column))
	}
	access.on(queryTableColumns, rows...)
	access.on(dbQuery("MIG-OWN-"+owner.slug+"-missing"), countRow("missing", missing))
	access.on(dbQuery("MIG-OWN-"+owner.slug+"-unowned"), countRow("missing", missing))
	access.on(queryOrganizationUnitExists, countRow("units", 0))
	return access
}

func TestAddOrganizationUnitOwnerColumnOperation(t *testing.T) {
	for _, owner := range organizationUnitOwnerTables {
		t.Run(owner.slug, func(t *testing.T) {
			operation, err := newAddOrganizationUnitOwnerColumnOperation(owner)
			require.NoError(t, err)
			definition := operation.Definition()
			assert.Equal(t, owner.addID, definition.ID)
			assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
			assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

			sqlOperation := operation.(*engine.SQLOperation)
			implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
			require.NoError(t, err)
			require.Len(t, implementation.Statements, 2)
			assert.Contains(t, implementation.Statements[1].Query, "ADD COLUMN OU_ID VARCHAR(36)")
			assert.NotContains(t, implementation.Statements[1].Query, "NOT NULL")

			nullable := ownerColumnNullable
			source, err := operation.PreVerify(context.Background(), ownerAccess(owner, nil, 0),
				engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, engine.VerificationSource, source.State)
			target, err := operation.PostVerify(context.Background(), ownerAccess(owner, &nullable, 0),
				engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, engine.VerificationTarget, target.State)
			required := ownerColumnRequired
			invalid, err := operation.PreVerify(context.Background(), ownerAccess(owner, &required, 0),
				engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, engine.VerificationInvalid, invalid.State)
		})
	}
}

func TestBackfillOrganizationUnitOwnerOperation(t *testing.T) {
	owner := organizationUnitOwnerTables[0]
	declared := organizationUnitSource(DeclarativeOrganizationUnit{ID: testSystemOrganizationUnitID})
	operation, err := newBackfillOrganizationUnitOwnerOperation(owner, declared)
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	definition := operation.Definition()
	assert.Equal(t, owner.fillID, definition.ID)
	assert.Equal(t, engine.OperationKindDataChange, definition.Kind)

	targets, err := sqlOperation.ResolveTargets(context.Background(), newFakeAccess(),
		engine.MigrationSelection{DeploymentIDs: []string{"dep-a", "dep-b"}})
	require.NoError(t, err)
	assert.Equal(t, []engine.ExecutionTarget{{DeploymentID: "dep-a"}, {DeploymentID: "dep-b"}}, targets)

	implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	assert.Contains(t, implementation.Statements[0].Query, `UPDATE "FLOW" SET OU_ID = $2`)
	args, err := implementation.Statements[0].Args(engine.ExecutionTarget{DeploymentID: "dep-a"})
	require.NoError(t, err)
	assert.Equal(t, []interface{}{"dep-a", testSystemOrganizationUnitID}, args)

	nullable := ownerColumnNullable
	required := ownerColumnRequired
	tests := []struct {
		name     string
		column   *columnSpec
		missing  int64
		expected engine.VerificationState
	}{
		{"rows without an owner", &nullable, 3, engine.VerificationSource},
		{"every row has an owner", &nullable, 0, engine.VerificationTarget},
		{"finalized column", &required, 0, engine.VerificationTarget},
		{"missing column", nil, 0, engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.PreVerify(context.Background(), ownerAccess(owner, test.column, test.missing),
				engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
		})
	}

	t.Run("readiness follows the column and the declared organization unit", func(t *testing.T) {
		deferred, err := sqlOperation.CheckReadiness(context.Background(), ownerAccess(owner, nil, 0),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessDeferred, deferred.State)
		assert.Equal(t, []engine.OperationID{owner.addID}, deferred.DeferredUntil)

		ready, err := sqlOperation.CheckReadiness(context.Background(), ownerAccess(owner, &nullable, 3),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessReady, ready.State)

		undeclared, err := newBackfillOrganizationUnitOwnerOperation(owner, organizationUnitSource())
		require.NoError(t, err)
		blocked, err := undeclared.(*engine.SQLOperation).CheckReadiness(context.Background(),
			ownerAccess(owner, &nullable, 3), engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessBlocked, blocked.State)
		require.Len(t, blocked.Findings, 1)
		assert.Equal(t, "system_organization_unit_not_found", blocked.Findings[0].Code)
	})
}

func TestFinalizeOrganizationUnitOwnerOperation(t *testing.T) {
	owner := organizationUnitOwnerTables[1]
	operation, err := newFinalizeOrganizationUnitOwnerOperation(owner)
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)

	assert.Equal(t, owner.finalID, operation.Definition().ID)
	implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Equal(t, `ALTER TABLE "IDP" ALTER COLUMN OU_ID SET NOT NULL`, implementation.Statements[1].Query)

	nullable := ownerColumnNullable
	required := ownerColumnRequired
	tests := []struct {
		name      string
		column    *columnSpec
		missing   int64
		expected  engine.VerificationState
		readiness engine.ReadinessState
	}{
		{"ready to require", &nullable, 0, engine.VerificationSource, engine.ReadinessReady},
		{"already required", &required, 0, engine.VerificationTarget, engine.ReadinessReady},
		{"rows still unowned", &nullable, 2, engine.VerificationInvalid, engine.ReadinessDeferred},
		{"column missing", nil, 0, engine.VerificationInvalid, engine.ReadinessDeferred},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := ownerAccess(owner, test.column, test.missing)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
			readiness, err := sqlOperation.CheckReadiness(context.Background(), access, engine.PlanContext{})
			require.NoError(t, err)
			assert.Equal(t, test.readiness, readiness.State)
		})
	}

	odd := columnSpec{"ou_id", "YES", "text", "none", "none"}
	access := ownerAccess(owner, &odd, 0)
	result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, result.State)
	readiness, err := sqlOperation.CheckReadiness(context.Background(), access, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
}

func TestOrganizationUnitOwnerOperationsPropagateInspectionErrors(t *testing.T) {
	owner := organizationUnitOwnerTables[2]
	failing := newFakeAccess()
	failing.err = errors.New("database unavailable")

	backfill, err := newBackfillOrganizationUnitOwnerOperation(owner, organizationUnitSource())
	require.NoError(t, err)
	_, err = backfill.PreVerify(context.Background(), failing, engine.ExecutionTarget{})
	assert.ErrorContains(t, err, "database unavailable")

	finalize, err := newFinalizeOrganizationUnitOwnerOperation(owner)
	require.NoError(t, err)
	_, err = finalize.(*engine.SQLOperation).CheckReadiness(context.Background(), failing, engine.PlanContext{})
	assert.ErrorContains(t, err, "database unavailable")
}

func indexRow(valid bool, table string, definition string) engine.QueryRow {
	return engine.QueryRow{
		"is_valid": valid, "is_ready": valid, "is_unique": false, "has_predicate": false,
		"table_name": table, "definition": definition,
	}
}

func TestOnlineIndexOperation(t *testing.T) {
	operation := newAddEntityProjectIndexOperation().(*onlineIndexOperation)
	definition := operation.Definition()
	assert.Equal(t, addEntityProjectIndexOperationID, definition.ID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeOnline, definition.InterruptionMode)
	_, online := engine.Operation(operation).(engine.OnlineOperation)
	assert.True(t, online)

	const expected = `CREATE INDEX idx_entity_project ON public."ENTITY" USING btree ` +
		`(deployment_id, project_id, category)`
	tests := []struct {
		name     string
		rows     []engine.QueryRow
		expected engine.VerificationState
		leftover bool
	}{
		{"absent", nil, engine.VerificationSource, false},
		{"valid", []engine.QueryRow{indexRow(true, "ENTITY", expected)}, engine.VerificationTarget, false},
		{"interrupted build", []engine.QueryRow{indexRow(false, "ENTITY", expected)}, engine.VerificationSource, true},
		{"other columns", []engine.QueryRow{indexRow(true, "ENTITY",
			`CREATE INDEX idx_entity_project ON public."ENTITY" USING btree (deployment_id, category)`)},
			engine.VerificationInvalid, false},
		{"other table", []engine.QueryRow{indexRow(true, "GROUP", expected)}, engine.VerificationInvalid, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex, test.rows...)
			result, leftover, err := operation.inspect(context.Background(), access)
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
			assert.Equal(t, test.leftover, leftover)

			readiness, err := operation.CheckReadiness(context.Background(), access, engine.PlanContext{})
			require.NoError(t, err)
			if test.expected == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}

	t.Run("builds a missing index", func(t *testing.T) {
		access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex)
		require.NoError(t, operation.build(context.Background(), access))
		assert.Equal(t, []string{operation.create.GetID()}, access.executedIDs())
		assert.Contains(t, access.executed[0].queryID, "-02")
	})
	t.Run("rebuilds an interrupted index", func(t *testing.T) {
		access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex, indexRow(false, "ENTITY", expected))
		require.NoError(t, operation.build(context.Background(), access))
		assert.Equal(t, []string{operation.drop.GetID(), operation.create.GetID()}, access.executedIDs())
	})
	t.Run("keeps a valid index", func(t *testing.T) {
		access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex, indexRow(true, "ENTITY", expected))
		require.NoError(t, operation.build(context.Background(), access))
		assert.Empty(t, access.executed)
	})
	t.Run("refuses a conflicting index", func(t *testing.T) {
		access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex, indexRow(true, "GROUP", expected))
		require.ErrorContains(t, operation.build(context.Background(), access), "prepare index")
		assert.Empty(t, access.executed)
	})
	t.Run("a missing table is invalid", func(t *testing.T) {
		result, _, err := operation.inspect(context.Background(), newFakeAccess().relations())
		require.NoError(t, err)
		assert.Equal(t, engine.VerificationInvalid, result.State)
	})
	t.Run("post verification demands the built index", func(t *testing.T) {
		access := newFakeAccess().relations("ENTITY").on(queryOnlineIndex)
		result, err := operation.PostVerify(context.Background(), access, engine.ExecutionTarget{})
		require.NoError(t, err)
		assert.Equal(t, engine.VerificationInvalid, result.State)
	})
	t.Run("inspection failures are returned", func(t *testing.T) {
		failing := newFakeAccess()
		failing.err = errors.New("database unavailable")
		assert.ErrorContains(t, operation.build(context.Background(), failing), "database unavailable")
		_, err := operation.PreVerify(context.Background(), failing, engine.ExecutionTarget{})
		assert.ErrorContains(t, err, "database unavailable")
	})
}

func TestEntityAndOrganizationUnitIndexOperations(t *testing.T) {
	owner := newAddEntityOwnerIndexOperation().(*onlineIndexOperation)
	assert.Equal(t, addEntityOwnerIndexOperationID, owner.Definition().ID)
	assert.Contains(t, owner.spec.createSQL, "SYSTEM_ATTRIBUTES#>>'{owner,id}'")
	unit := newAddOrganizationUnitProjectIndexOperation().(*onlineIndexOperation)
	assert.Equal(t, addOrganizationUnitProjectIndexOperationID, unit.Definition().ID)
	assert.Equal(t, "ORGANIZATION_UNIT", unit.spec.table)
	assert.NotEqual(t, owner.create.GetID(), unit.create.GetID())
}

func TestRoleAssignmentAssigneeIndexAndBackfillAndFinalize(t *testing.T) {
	index := newAddRoleAssignmentAssigneeIndexOperation().(*onlineIndexOperation)
	assert.Equal(t, addRoleAssignmentAssigneeIndexOperationID, index.Definition().ID)
	assert.Equal(t, roleAssignmentAssigneeIndexNext, index.spec.name)

	backfill, err := newBackfillRoleAssignmentOperation()
	require.NoError(t, err)
	sqlBackfill := backfill.(*engine.SQLOperation)
	assert.Equal(t, engine.OperationKindDataChange, backfill.Definition().Kind)
	implementation, err := sqlBackfill.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	assert.Contains(t, implementation.Statements[0].Query, "SET ASSIGNMENT_OU_ID = r.OU_ID")

	added := roleAssignmentColumns("NO", "character varying", "36", "''::character varying")
	access := func(missing int64, columns []engine.QueryRow) *fakeAccess {
		return newFakeAccess().relations("ROLE_ASSIGNMENT").on(queryTableColumns, columns...).
			on(queryRoleAssignmentUnassigned, countRow("missing", missing))
	}
	source, err := backfill.PreVerify(context.Background(), access(4, added), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)
	target, err := backfill.PreVerify(context.Background(), access(0, added), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
	finalColumns := []engine.QueryRow{
		columnRow(columnSpec{"assignment_ou_id", "NO", "character varying", "36", "none"}),
	}
	finalized, err := backfill.PreVerify(context.Background(), access(0, finalColumns), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, finalized.State)
	absent, err := backfill.PreVerify(context.Background(), access(0, nil), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, absent.State)
	deferred, err := sqlBackfill.CheckReadiness(context.Background(), access(0, nil), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessDeferred, deferred.State)
	ready, err := sqlBackfill.CheckReadiness(context.Background(), access(2, added), engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessReady, ready.State)

	finalize, err := newFinalizeRoleAssignmentOperation()
	require.NoError(t, err)
	sqlFinalize := finalize.(*engine.SQLOperation)
	finalImplementation, err := sqlFinalize.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, finalImplementation.Statements, 4)
	assert.Contains(t, finalImplementation.Statements[1].Query, "DROP DEFAULT")
	assert.Contains(t, finalImplementation.Statements[2].Query, "DROP INDEX")
	assert.Contains(t, finalImplementation.Statements[3].Query, "RENAME TO")

	finalAccess := func(columns []engine.QueryRow, definition string) *fakeAccess {
		return access(0, columns).on(queryOnlineIndex, indexRow(true, "ROLE_ASSIGNMENT", definition))
	}
	const legacyIndex = `CREATE INDEX idx_role_assignment_assignee ON public."ROLE_ASSIGNMENT" USING btree ` +
		`(deployment_id, assignee_type, assignee_id)`
	const targetIndex = `CREATE INDEX idx_role_assignment_assignee ON public."ROLE_ASSIGNMENT" USING btree ` +
		`(deployment_id, assignee_type, assignee_id, assignment_ou_id)`
	tests := []struct {
		name     string
		access   *fakeAccess
		expected engine.VerificationState
	}{
		{"ready to finalize", finalAccess(added, legacyIndex), engine.VerificationSource},
		{"finalized", finalAccess(finalColumns, targetIndex), engine.VerificationTarget},
		{"default dropped but index not swapped", finalAccess(finalColumns, legacyIndex), engine.VerificationInvalid},
		{"columns missing", finalAccess(nil, legacyIndex), engine.VerificationInvalid},
		{"index missing", access(0, added).on(queryOnlineIndex), engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := finalize.PreVerify(context.Background(), test.access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State, result.Summary)
			readiness, err := sqlFinalize.CheckReadiness(context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			if test.expected == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}
}

func TestNewConfigAndEntityTableOperations(t *testing.T) {
	tests := []struct {
		name       string
		build      func() (engine.Operation, error)
		id         engine.OperationID
		store      engine.StoreID
		statements int
	}{
		{"project", newCreateProjectTableOperation, createProjectTableOperationID, engine.StoreIDEntityDB, 2},
		{"http connection", newCreateHTTPConnectionOperation, createHTTPConnectionOperationID,
			engine.StoreIDConfigDB, 2},
		{"authorization detail type", newCreateAuthorizationDetailTypeOperation,
			createAuthorizationDetailTypeOperationID, engine.StoreIDConfigDB, 3},
		{"managed resource", newCreateManagedResourceOperation, createManagedResourceOperationID,
			engine.StoreIDConfigDB, 1},
		{"gateway apply lease", newCreateGatewayApplyLeaseOperation, createGatewayApplyLeaseOperationID,
			engine.StoreIDConfigDB, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := test.build()
			require.NoError(t, err)
			definition := operation.Definition()
			assert.Equal(t, test.id, definition.ID)
			assert.Equal(t, test.store, definition.StoreID)
			assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
			implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
			require.NoError(t, err)
			assert.Len(t, implementation.Statements, test.statements)
		})
	}
}

func TestProjectColumnOperations(t *testing.T) {
	entity, err := newAddEntityProjectIDOperation()
	require.NoError(t, err)
	unit, err := newAddOrganizationUnitProjectIDOperation()
	require.NoError(t, err)
	for _, operation := range []engine.Operation{entity, unit} {
		implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
		require.NoError(t, err)
		require.Len(t, implementation.Statements, 2, "the index is built by its own online operation")
		assert.Contains(t, implementation.Statements[1].Query, "ADD COLUMN PROJECT_ID VARCHAR(36)")
		assert.Equal(t, engine.StoreIDEntityDB, operation.Definition().StoreID)
	}
}

func dbQuery(id string) dbmodel.DBQuery {
	return dbmodel.DBQuery{ID: id}
}

func TestDropGroupMemberLookupIndexOperation(t *testing.T) {
	operation, err := newDropGroupMemberLookupIndexOperation()
	require.NoError(t, err)
	sqlOperation := operation.(*engine.SQLOperation)
	assert.Equal(t, dropGroupMemberLookupIndexOperationID, operation.Definition().ID)
	assert.Equal(t, engine.StoreIDEntityDB, operation.Definition().StoreID)

	implementation, err := sqlOperation.Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Equal(t, `DROP INDEX "idx_group_member_lookup"`, implementation.Statements[1].Query)

	tests := []struct {
		name     string
		rows     []engine.QueryRow
		expected engine.VerificationState
	}{
		{"index present", []engine.QueryRow{indexRow(true, "GROUP_MEMBER_REFERENCE", "")}, engine.VerificationSource},
		{"index removed", nil, engine.VerificationTarget},
		{"name taken by another table", []engine.QueryRow{indexRow(true, "GROUP", "")}, engine.VerificationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := newFakeAccess().on(queryOnlineIndex, test.rows...)
			result, err := operation.PreVerify(context.Background(), access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.State)
			readiness, err := sqlOperation.CheckReadiness(context.Background(), access, engine.PlanContext{})
			require.NoError(t, err)
			if test.expected == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}
	failing := newFakeAccess()
	failing.err = errors.New("database unavailable")
	_, err = operation.PreVerify(context.Background(), failing, engine.ExecutionTarget{})
	assert.ErrorContains(t, err, "database unavailable")
}
