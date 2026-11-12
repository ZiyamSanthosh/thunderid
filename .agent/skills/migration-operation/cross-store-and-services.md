# Cross store reads and service backed inputs

## Reading another store

An operation writes one store but may read others by listing them in `ReadStoreIDs`.

```go
engine.OperationDefinition{
	ID:           "2.1.0/entitydb/64_convert_widget_references",
	StoreID:      engine.StoreIDEntityDB,                  // the one store it writes
	ReadStoreIDs: []engine.StoreID{engine.StoreIDConfigDB}, // read only
	Kind:         engine.OperationKindDataChange,
	InterruptionMode: engine.InterruptionModeResumable,
}
```

Rules:

- The writable store is never repeated in `ReadStoreIDs`, and entries are not duplicated.
- Read only. The engine refuses a write to any store but the operation's own.
- There is no transaction spanning stores. The read may race with changes made elsewhere. Make the
  operation correct if the other store changes between batches, and re-read per batch rather than
  caching across batches.
- Express ordering through readiness. If this operation needs the config store to be converted first,
  return `deferred` naming that operation, which must precede this one.
- When data must move between stores, write two operations, one per store, each verifying its own
  store's state.

## Why not a cross database transaction

Stores can sit in different databases. Even when they share a database, each is migrated and
recovered independently. Design for each operation to be restartable alone.

## When SQL is not enough

Some conversions need information the database does not hold. Example from release 2.0.0: entity types
defined in declarative resource files have no row, but a converted reference may point at one, and a
generated handle must not collide with one.

Use an **injected provider**:

1. Define the smallest interface the operation needs in the release package, next to the operation.
2. Add a narrowly typed, lazy field to the release's `Dependencies` struct.
3. Wrap it in a small source type that resolves the provider on first use, so phases that do not need
   it never build the services behind it. A nil provider yields no extra data.
4. Pass the source to the operation constructor in `release.go`.
5. Build the concrete provider in `backend/cmd/server`, from the real service, and pass it through
   `current` to the release. The engine never imports business packages.

```go
type Dependencies struct {
	// WidgetPolicies lazily provides the policies defined in declarative resource files.
	WidgetPolicies func() (WidgetPolicies, error)
}

func newBackfillWidgetOperation(policies *widgetPolicySource) engine.Operation { ... }
```

Cautions:

- **Prefer read only providers.** Every provider in release 2.0.0 only reads (declared entity types and
  connections) and the operation writes through its own execution access, so the engine's write fence,
  verifiers and transaction boundary all apply. Use a service to decide or look up, and write with SQL.
- **A service that writes is allowed by the design but carries extra obligations.** It bypasses the
  write fence, so it must use the migration execution context and resolved deployment identity, take
  part in the migration transaction, be idempotent, and have its side effects and caches assessed. The
  [migration-service-operation](../migration-service-operation/SKILL.md) skill defines these. Such an operation also needs a PostgreSQL integration test that proves rollback on
  failure and completed progress on success. Decide this in the plan, not during coding.
- **Missing provider behaviour must be deliberate.** The 2.0.0 sources treat a nil provider as "no
  declarative data" so tools and tests can omit it. If a missing provider could make the operation
  convert data incorrectly, return a clear error instead.
- The lazy source caches its result for the process. The CLI is one shot, so that is fine, but do not
  rely on the cache to observe later changes.
- Using a business service brings its initialisation graph, caches and transactions along. Inspect
  them. Do not copy an existing provider mechanically.
- Keep the migration deterministic. The same inputs must give the same result.

The dedicated skill for this seam is [migration-service-operation](../migration-service-operation/SKILL.md).
It is self contained. Read it before writing code that uses a provider.

## Checklist

- [ ] Only the declared stores are read, only one written.
- [ ] Ordering is expressed in readiness, not assumed.
- [ ] Re-reads each batch, tolerant of concurrent change.
- [ ] Provider interface is minimal and lazy. Read only unless the plan explicitly approved a writing service.
- [ ] Missing provider behaviour is a decision, not an accident.
- [ ] Both stores' verifiers can tell their own state without the other operation having run.
