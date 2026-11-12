---
name: migration-service-operation
description: Implement or review a ThunderID migration operation that requires business-service dependency injection. Use when a release migration needs application, entity, organization, or another ThunderID service rather than SQL access alone.
---

# Migration Service Operation

This skill is self contained. It covers only the **service dependency seam**: how a migration
operation receives information or behaviour from a ThunderID service. Everything else about operations
lives in the migration skills:

- plan the change with `migration-plan` first (phase, kind, mode, rollback);
- implement the operation with `migration-operation`;
- test it with `migration-testing`;
- review it with `migration-review`. The hub is `migration-system`.

## Decision

The migration engine does not know about ThunderID business services. It runs operations through the
existing `engine.Operation`, `engine.AtomicOperation` and `engine.ResumableOperation` contracts.

Service injection happens while a release builds its plan. An operation declares the smallest
interface it needs, receives it through its constructor, and is returned to the engine as an ordinary
operation. The release keeps a release specific `Dependencies` struct, and the forwarding path runs
through `current` and `cmd/server`.

Do not add speculative dependency fields, a global service registry or a separate "programmatic
operation" executor before a real migration requires them.

## The seam

```text
cmd/server composition
        | constructs concrete services when required (lazily)
        v
current release selector           (alias of the release Dependencies, forwards them)
        | forwards the selected release dependencies
        v
release plan construction          (migrationPlan in release.go)
        | injects narrow interfaces into operation constructors
        v
migration operation                (an ordinary engine operation)
        | implements an existing engine contract
        v
migration engine                   (never imports business packages)
```

| Layer | Owns |
|---|---|
| Operation file in the release package | The narrow interface, the logic using it |
| `release.go` | The `Dependencies` struct, constructing sources, operation order and phase membership |
| `current/current.go` | Release selection. Forwards types with aliases. No initialisation, no logic |
| `cmd/server/migration_*.go` | Concrete provider construction from real services and configuration |
| Engine | Dispatching atomic, online or resumable operations. Nothing service specific |

## Adding a service backed operation

### 1. Define a narrow interface next to the operation

Declare only the methods the migration uses. Do not depend on a large service interface because the
concrete implementation happens to provide it. The interface belongs in the release package, in the
operation file when only that operation uses it.

```go
// DeclarativeEntityTypes lists the entity types defined in declarative resource files.
type DeclarativeEntityTypes interface {
	ListDeclarativeEntityTypes(ctx context.Context) ([]DeclarativeEntityType, error)
}
```

Export the small data types the interface returns (here `DeclarativeEntityType`) so `current` and
`cmd/server` can alias and fill them.

### 2. Wrap it in a lazy source

A provider is a function, so phases and commands that do not need it never build the service graph
behind it. A small source type resolves it on first use and caches the result for the process.

```go
type declarativeEntityTypeSource struct {
	provider func() (DeclarativeEntityTypes, error)
	once     sync.Once
	types    []DeclarativeEntityType
	err      error
}

func (s *declarativeEntityTypeSource) list(ctx context.Context) ([]DeclarativeEntityType, error) {
	if s == nil || s.provider == nil {
		return nil, nil // no declarative data
	}
	s.once.Do(func() {
		lister, err := s.provider()
		if err != nil {
			s.err = err
			return
		}
		s.types, s.err = lister.ListDeclarativeEntityTypes(ctx)
	})
	return s.types, s.err
}
```

The CLI is a one shot process, so process wide caching is fine. Do not rely on it to observe later
changes.

### 3. Declare the release dependency

Add a narrowly typed, lazy field to the existing release `Dependencies` struct, only when an
operation consumes it. Document what it provides and what happens when it is nil.

```go
type Dependencies struct {
	// DeclarativeEntityTypes lazily provides the read-only entity types defined in declarative resource
	// files, which have no ENTITY_TYPES row. When nil, the operations map entity types from the
	// database alone.
	DeclarativeEntityTypes func() (DeclarativeEntityTypes, error)
}
```

Build the source once in `migrationPlan` and pass it to every operation that needs it, forward and
rollback alike, so both directions see the same data:

```go
declaredEntityTypes := newDeclarativeEntityTypeSource(dependencies.DeclarativeEntityTypes)
backfill := newBackfillEntityTypeHandleOperation(declaredEntityTypes)
```

### 4. Forward through `current`

Use type aliases when no adaptation is needed. `current` never initialises services.

```go
type Dependencies = release.Dependencies
type DeclarativeEntityTypes = release.DeclarativeEntityTypes
```

### 5. Construct the service in `cmd/server`

The composition layer owns configuration dependent initialisation. It builds only the service graph the
release needs, lazily, and adapts the real service to the narrow interface.

```go
releaseDefinition, err := current.DefinitionWithDependencies(current.Dependencies{
	DeclarativeEntityTypes: newDeclarativeEntityTypesProvider(cfg),
	DeclarativeConnections: newDeclarativeConnectionsProvider(),
})
```

Do not make the engine import application packages. Do not start HTTP routes, MCP tools, background
jobs or the full serving lifecycle to obtain a service. Close any resources the migration owns when the
command ends.

## Worked examples in release 2.0.0

### Declarative entity types

Entity types defined in declarative resource files have no `ENTITY_TYPES` row. Operations that map
stored names to handles, and the handle backfill, must still know them.

- `cmd/server/migration_declarative_entity_types.go`: `newDeclarativeEntityTypesProvider(cfg)` lazily
  builds the system authorization service, the cache manager, the organization unit service and the
  entity type service **over the declarative store only** (no routes, no MCP, no database reads), then
  adapts `GetEntityTypeList` for the user and agent categories to the narrow interface.
- Only the declarative half is injected. Database entity types are still read by the operations' own SQL.
- Declarative types are not bound to a deployment (the file store has no such concept), so they apply to
  every deployment being migrated. Merge them by category and value, not by deployment.
- Rules in the mapping: database mapping first; a value that is already a handle is accepted unchanged
  so re-runs are safe; a name defined differently in the database and in the files is an **error naming
  both handles**. The migration never guesses.
- The handle backfill reserves declarative handles when generating new ones, and its verifier reports a
  database row that clashes with one as `invalid`, because the server refuses to start when a database
  entity type shares a handle with a declarative one in the same category.

Pitfalls found while building it:

- The list call sets `IsReadOnly` only in the composite store. The declarative only store leaves it
  false, so the adapter must not filter on it.
- Calling the service with `security.WithRuntimeContext` makes access checks allow everything. A
  migration has no user, so it needs that runtime context.
- A nil provider is treated as "no declarative types" so tools and tests that omit it keep working.
  `cmd/server` always supplies it. See "Missing provider behaviour" below.

### Declarative connections

`cmd/server/migration_declarative_connections.go` reads every connection document the declarative
loader would read, in the same order of precedence, with the resource loader, and keeps only the
identifier and vendor type of each. Operations use it to find which connection issued a legacy value.
The provider holds no services at all, which keeps it cheap and read only.

## Required design

- Keep the migration engine independent of ThunderID business packages.
- Implement the operation through the existing atomic or resumable contract (online is for SQL only
  schema changes). Do not introduce a separate programmatic operation executor.
- Define the smallest service interface needed in the release package, preferably next to the operation.
- Inject it through the operation constructor.
- Add narrowly typed fields to the existing release dependency seam only when a concrete operation
  consumes them. No universal registries, no speculative providers.
- Let the release construct operations, `current` select and forward, and `cmd/server` initialise
  concrete services.
- Prefer lazy providers so migrations that do not need a service avoid initialising it.

## Preserve the migration guarantees

A service backed operation keeps every guarantee of a SQL operation:

- **Context and deployment.** The service uses the migration execution context and the resolved
  deployment identity when applicable.
- **Transaction.** Database writes and the operation's progress participate in the migration
  transaction when atomic completion is required. Prefer **read only providers** and write with the
  operation's own execution access. Then the engine's write fence, verifiers and transaction boundary
  all apply. A service that writes bypasses the fence and must be proven to join the transaction.
- **Checkpoint.** A resumable operation persists enough in its checkpoint to restart safely.
- **Side effects.** The developer assesses external side effects, caches, idempotency and retry
  behaviour. A repeated call must be harmless.
- **Phase placement** stays in the release plan. Using a service does not decide expand, migrate or
  contract.
- **Compatibility** with concurrently serving binaries is assessed like any other operation.

### Missing provider behaviour

Choose deliberately. Release 2.0.0 treats a nil provider as "no declarative data" so existing callers
keep working. If a missing provider could cause wrong conversions, return a clear error from the
constructor or the plan builder instead. Return an error when the selected phase contains an operation
whose required provider is absent.

## Implementation checks

1. Identify whether the operation is atomic or resumable from its transaction and checkpoint needs, not
   from whether it uses SQL or a service.
2. Verify the injected service uses the migration context and resolved deployment identity where
   applicable.
3. Verify database writes and migration progress have the required transaction boundary.
4. Review idempotency, retries, caches, external side effects, and compatibility with concurrently
   serving binaries.
5. Keep phase assignment in the release plan.
6. Add focused operation tests and release construction tests.
7. Add PostgreSQL integration coverage when real service initialisation, transaction participation, or
   persistence behaviour must be proven.

Do not copy an existing provider mechanically. Inspect the real service's initialisation graph and
transaction behaviour, then wire only what the concrete operation requires.

## Testing requirements

Test in layers proportional to the operation:

1. **Operation with a narrow fake.** Verify service calls, transformation behaviour and error
   propagation, including a conflict between database and declarative data.
2. **Release construction.** With the required dependency, with it nil, and with a failing provider.
3. **Standalone command with the real service graph** when initialisation or transaction participation
   is material. The `cmd/server` tests for the providers show the pattern.
4. **PostgreSQL integration** for any operation whose service writes: prove rollback on failure and
   completed migration progress on success. For read only providers, assert the migrated data in the
   Docker suite using the declarative resource files in `backend/tests/migration/resources`.

An integration test that only shows a service method can be called is not enough. Assert the migrated
data and the durable migration state.

## What remains generic

- The engine dispatches atomic, online or resumable operations without knowing how they are
  implemented.
- SQL and service access may be used by any execution style.
- The release owns operation order and phase membership.
- `current` owns release selection.
- `cmd/server` owns concrete dependency construction.

Only release specific operation code should know which ThunderID services it needs.
