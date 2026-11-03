# Atomic SQL operations

Use for short changes that fit one transaction. The engine runs target resolution, pre verification,
your statements, post verification and the progress update inside **one transaction**. Any error rolls
all of it back, including the progress row, so a failed atomic operation leaves nothing behind.

`engine.NewSQLOperation` builds these. It validates the definition and requires provider explicit SQL
and both verifiers.

## Schema change template

```go
var queryWidgetColumns = dbmodel.DBQuery{
	ID: "MIG-CFG_MGT-40", // next unused id in the package
	PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'WIDGET'
		AND COLUMN_NAME IN ('id', 'color')`,
}

func newAddWidgetColorOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.1.0/configdb/60_add_widget_color",
			Description:      "Add a nullable color column to the widget table",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkWidgetColorReadiness, // optional but recommended
		PreVerifier:      verifyWidgetColor,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "add-color", Query: `ALTER TABLE "WIDGET" ADD COLUMN COLOR VARCHAR(50)`},
			}},
		},
		PostVerifier: verifyWidgetColor,
	})
}
```

The verifier and readiness functions share one classifier, see
[verifiers-and-readiness.md](verifiers-and-readiness.md).

Notes:

- Statements run in order. Each has an id used in error messages.
- `SET LOCAL lock_timeout` must come first for any statement that takes a table lock.
- Table and column names in the catalogue queries are matched case insensitively through `UPPER` or
  `LOWER`, because ThunderID schemas use upper case names in quoted tables.
- Quote table names in DDL: `"WIDGET"`.

## New table

Creating a table is an atomic schema change. Verify the **whole shape**, not just existence:

- the relation exists and is an ordinary table (`relkind`);
- columns, types, lengths, nullability and defaults;
- primary and foreign keys including referenced table and delete action;
- unique and check constraints;
- indexes with their columns and uniqueness.

A half created or differently shaped table is `invalid`, with a finding. An absent table is `source`.
Include the indexes in the same operation so the table is complete or absent.

If a dependent table must exist first (foreign key), return `deferred` readiness naming the operation
that creates it. The 2.0.0 release builds these through a spec driven helper that checks every
listed column, key and index; a new release package should follow that structure rather than
asserting a bare `CREATE TABLE`.

## Data change template

A data change uses a target resolver and binds arguments per target.

```go
func resolveDeployments(
	_ context.Context, _ engine.ReadAccess, selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	if len(selection.DeploymentIDs) == 0 {
		return []engine.ExecutionTarget{{}}, nil // whole store
	}
	targets := make([]engine.ExecutionTarget, 0, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		targets = append(targets, engine.ExecutionTarget{DeploymentID: deploymentID})
	}
	return targets, nil
}

func newDefaultWidgetColorOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.1.0/configdb/61_default_widget_color",
			Description:      "Set the default color on widgets that have none",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		TargetResolver: resolveDeployments,
		PreVerifier:    verifyWidgetColorData,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{{
				ID:    "default-color",
				Query: `UPDATE "WIDGET" SET COLOR = 'blue' WHERE COLOR IS NULL AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
				Args: func(target engine.ExecutionTarget) ([]interface{}, error) {
					return []interface{}{target.DeploymentID}, nil
				},
			}}},
		},
		PostVerifier: verifyWidgetColorData,
	})
}
```

Use atomic for data only when one statement per target is fast on the largest realistic table. If in
doubt, use [resumable.md](resumable.md). A long running atomic statement holds locks and may be
cancelled by the database.

## Online mode with SQLOperation

`InterruptionModeOnline` is also accepted by `NewSQLOperation`. The statements then run in autocommit,
not in a transaction, and `Finalize` does nothing. This fits a single statement such as
`CREATE INDEX CONCURRENTLY`. For multi step schema work (build, then attach), write a custom type, see
[online.md](online.md).

## Checklist

- [ ] Kind and mode are a valid pair. Schema has no resolver, data has one.
- [ ] `PreVerifier` and `PostVerifier` share a classifier.
- [ ] Lock timeout is the first statement for table locking DDL.
- [ ] Data statements filter by deployment and take the deployment from the target.
- [ ] Running it twice is harmless, either by `IF NOT EXISTS` or because `PreVerify` returns `target`.
- [ ] The unit test asserts the statements (count and key fragments), see
      [migration-testing](../migration-testing/unit-tests.md).
