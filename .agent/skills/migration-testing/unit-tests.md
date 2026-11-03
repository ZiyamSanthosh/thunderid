# Unit tests for operations

Unit tests run without a database. They use fakes for read access and exercise everything that does
not need a live PostgreSQL.

## What can and cannot be unit tested

| Can | Cannot (use integration tests) |
|---|---|
| `Definition()`, constructor validation | Real SQL text executing |
| `PreVerify`, `PostVerify`, `CheckReadiness` with fake rows | `Execute`, `Finalize` and `ExecuteBatch` through the engine: the constructor for `OperationExecution` is private to the engine package |
| `ResolveTargets` with and without a deployment selection | Locks, transactions, crash recovery |
| SQL statements as text (`SQLOperation.Implementation`) | Concurrency and timing |
| Pure helpers: classify, convert, next value, batch result | |

Because `Execute` paths are hard to reach, put decisions into **pure functions** and test those:
`convertName`, `nextAvailableHandle`, `classifyWidget`, `batchResult(checkpoint, completed)`. The
operation method then only moves data between the database and those functions.

## A fake read access

Rows are `engine.QueryRow` maps keyed by the lower case column aliases of your query. Key the fake by
query id.

```go
type fakeReadAccess struct {
	rowsByQueryID map[string][]engine.QueryRow
	err           error
}

func (a fakeReadAccess) Query(
	_ context.Context, _ engine.StoreID, query dbmodel.DBQuery, _ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.rowsByQueryID[query.GetID()], nil
}

func (a fakeReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func widgetColumnRow(name, nullable string) engine.QueryRow {
	return engine.QueryRow{"column_name": name, "is_nullable": nullable}
}
```

Existing test files in the release package define small fakes like this. A new release should add one
shared test helper file rather than copy it per operation.

## Definition and statements

```go
func TestAddWidgetColorOperation(t *testing.T) {
	operation, err := newAddWidgetColorOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.1.0/configdb/60_add_widget_color"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[0].Query, "lock_timeout")
	assert.Contains(t, implementation.Statements[1].Query, "ADD COLUMN COLOR")
}
```

For resumable and online types assert the interface: `assert.Implements(t, (*engine.ResumableOperation)(nil), operation)`.

## Verifier cases

Cover each state through the same classifier the operation uses:

| Case | Rows | Expect |
|---|---|---|
| source | accepted starting shape | `VerificationSource` |
| target | result shape | `VerificationTarget` |
| partial or leftover | half done shape (invalid index, one of two columns) | the state the plan says, usually `source` or `invalid` |
| wrong | unexpected column definition or conflicting data | `VerificationInvalid` with at least one finding |
| inspection failure | fake returns an error | the error is returned, wrapped |
| empty scope (data) | no rows | `VerificationTarget` |

Assert `Summary` is not empty and that invalid results carry a finding code, because the engine
rejects results that do not.

## Readiness cases

Table test with `ready`, `deferred`, `blocked`. For deferred assert `DeferredUntil` contains the
expected operation id. For blocked assert `Findings[0].Code` and a non empty `Remediation`.

```go
{name: "column missing defers", rows: ..., state: engine.ReadinessDeferred, deferredUntil: backfillID},
{name: "wrong column blocks",   rows: ..., state: engine.ReadinessBlocked,  findings: 1},
```

## Pure helper tests

Test boundaries and the awkward inputs: empty, maximum length, collisions, already converted, mixed
case, unicode, ids at batch edges. A resumable batch helper test checks `Completed` is true exactly
when fewer than a full batch was found and that the checkpoint advances.

## Release plan test

In the release package, pin what the plan must contain. When you add an operation, this test must
change in the same commit.

```go
require.Len(t, definition.Plan.Expand, 39)
assert.Equal(t, "2.1.0/configdb/60_add_widget_color", string(definition.Plan.Expand[38].Definition().ID))
require.Len(t, definition.Plan.Rollback, 9)
```

Also keep the query id uniqueness test green.

## Engine level tests

Tests that need `OperationExecution` or the coordinator live in the `engine` package and use the mock
database client. See [migration-engine](../migration-engine/SKILL.md).
