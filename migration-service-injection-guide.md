# Service Injection for Migration Operations

## Decision

The migration engine does not know about ThunderID business services. It executes operations through the existing `engine.Operation`, `engine.AtomicOperation`, and `engine.ResumableOperation` contracts.

Service injection is performed while a release builds its migration plan. A release operation declares the smallest interface it needs, receives that interface through its constructor, and is returned to the engine as an ordinary operation.

The release retains an empty, release-specific `Dependencies` structure and the forwarding path through `current` and `cmd/server`. This keeps the construction seam executable without naming a business service. Do not add speculative dependency fields, a global service registry, or a `ProgrammaticOperation` executor before a real migration requires them.

The feasibility proof is preserved on the local `migration-service-injection-poc` branch at commit `6c12842aa`.

## First service-backed dependency

`Dependencies.DeclarativeEntityTypes` in `backend/internal/system/migration/releases/v2_0_0/release.go`
is the first real use of this seam. It gives the entity-type reference operations the read-only entity
types defined in declarative resource files, which have no `ENTITY_TYPES` row. `cmd/server` builds it
lazily from the real entity type service over the declarative store (no routes, no database reads) and
adapts it to a narrow interface. See `MIGRATION-TEST-CHANGES.md`, section 1.3, for the design and the
pitfalls found (for example `IsReadOnly` is only set by the composite store).

## Stable extension seam

The default construction path is:

```text
cmd/server composition
        |
        | constructs concrete services when required
        v
current release selector
        |
        | forwards the selected release dependencies
        v
release plan construction
        |
        | injects narrow interfaces into operation constructors
        v
migration operation
        |
        | implements an existing engine operation contract
        v
migration engine
```

The stable seam present in the codebase is `DefinitionWithDependencies` forwarding a release-specific dependency value into the release plan that constructs operations. No engine change is required to support a service-backed operation.

## Adding a service-backed operation

### 1. Define a narrow interface next to the operation

Declare only the methods used by the migration. Do not depend on a large service interface merely because the concrete implementation provides it.

```go
type applicationService interface {
	GetApplication(context.Context, string) (*model.Application, error)
	UpdateApplication(context.Context, string, *model.Application) error
}
```

The interface belongs in the release package, preferably in the operation file when only one operation uses it.

### 2. Inject the interface through the operation constructor

```go
func newBackfillApplicationPropertyOperation(
	service applicationService,
) engine.ResumableOperation {
	return &backfillApplicationPropertyOperation{service: service}
}
```

Whether the operation is atomic or resumable depends on its execution characteristics, not on whether it uses SQL or a business service.

### 3. Declare the release dependencies

Once the first real service-backed operation exists, add its narrowly typed provider to the existing release-specific dependency structure. Prefer lazy provider functions so SQL-only commands and phases do not initialize unused services.

```go
type Dependencies struct {
	ApplicationService func() (applicationService, error)
}
```

Use the dependency while constructing the phase plan. Return a clear error if the selected phase contains an operation whose required provider is missing.

### 4. Forward dependencies through `current`

The `current` package selects the release compiled into the binary. It forwards dependencies but does not initialize services or contain migration business logic.

Prefer a type alias when no adaptation is required:

```go
type Dependencies = release.Dependencies
```

### 5. Construct services in `cmd/server`

The executable composition layer owns configuration-dependent initialization. Initialize only the service graph required by the release operations, preferably on first use. Close any resources owned by the migration service runtime when the command ends.

Do not make the migration engine import application packages. Do not initialize HTTP routes, MCP tools, background jobs, or the full serving lifecycle solely to obtain a business service.

### 6. Preserve migration execution guarantees

A service-backed operation must preserve the same guarantees as a SQL operation:

- The service must use the migration execution context, including the resolved deployment identity when applicable.
- Database writes and the operation checkpoint must participate in the migration transaction when atomic completion is required.
- A resumable operation must persist enough checkpoint information to restart safely.
- The operation developer must assess external side effects, cache behavior, idempotency, retry behavior, and source/target compatibility.
- Phase placement remains in the release plan. Service injection does not determine whether an operation belongs to expand, migrate, or contract.

## Testing requirements

Add tests in layers proportional to the operation:

1. Test the operation with a narrow fake to verify service calls, transformation behavior, and error propagation.
2. Test release construction with required and missing dependencies.
3. Test the standalone migration command with the real service graph when service initialization or transaction participation is material.
4. For a database-writing service operation, add PostgreSQL integration coverage proving both rollback on failure and migration-progress completion on success.

An integration test that merely proves a service method can be called is not sufficient for a real migration. Assert the migrated data and durable migration state.

## What remains generic

- The engine dispatches atomic or resumable operations without knowing how they are implemented.
- SQL and business-service access may be used by either execution style.
- The release owns operation ordering and phase membership.
- `current` owns release selection.
- `cmd/server` owns concrete dependency construction.

Only release-specific operation code should know which ThunderID business services it needs.
