# Migration design decisions

This is the working record of design decisions agreed during the migration discussion. It is kept
separate from the requirements so that requirements describe required outcomes while this document
describes the chosen implementation model. The final design will be rewritten into the ThunderID
feature documentation structure after the exploratory discussion is complete.

## Conversation handoff

Design work is parked after the fundamentals below were agreed:

- embedded release requirements, independent per-store state and a read-only terminating startup gate;
- adjacent published-release migrations, with each target artifact owning one transition;
- one trust-based `initialize` command, fixed `1.0.0` registration for absent metadata tables and
  manifest-target registration for missing rows in existing current-script tables;
- compiled SQL and programmatic operations, scopes, readiness and exact pre/post-verification;
- engine-owned ordering, reservations, transactions, metadata, batch/checkpoint orchestration,
  independent store finalization, reporting and verified resume;
- one executable with isolated modes, SQLite/PostgreSQL first and Redis deferred; and
- manual administrator handling for configuration and declarative-file changes, guided per release.

The section [Remaining design work](#remaining-design-work) is the authoritative continuation list.
Later refinement sections supersede any older text that they explicitly identify. The summary is the
short meeting artifact; the overview contains diagrams and examples; this file retains full reasoning.

## Decision 1: Embedded release manifest

**Status:** Agreed, including the embedding model, manifest structure and validation rules below.

- Every release has one canonical JSON release manifest.
- The Go build embeds the canonical manifest into every migration-aware executable by using the Go
  standard library `embed` package.
- Startup compatibility checks and migration decisions use only the embedded manifest. They never
  trust the readable manifest shipped beside the executable.
- The build copies the same canonical source file into the distribution for human inspection. The
  readable copy is informational and changing or removing it cannot alter runtime compatibility
  decisions.
- Release validation fails the build when the manifest is malformed or internally inconsistent,
  differs from `version.txt`, omits or duplicates a required logical store, or disagrees with the
  migration definitions shipped by the release.
- ThunderID provides a command that prints the manifest embedded in the invoked executable, so an
  administrator can inspect the executable's authoritative requirements.
- The manifest format and validation use Go standard-library facilities and do not require a
  third-party dependency.

### Source and distribution model

```text
Canonical release-manifest.json
            |
            +--> Embedded into the executable and used at runtime
            |
            +--> Copied unchanged into the distribution for people to read
```

The external file is not an input to startup compatibility checking or migration execution.

### Manifest responsibility

The manifest is the release compatibility contract. It declares what the release requires and the
immediately preceding release it accepts. It does not encode executable migration logic.
Developer-authored pre-verification, execution and post-verification functions belong to the
compiled migration registry. Release validation cross-checks that registry against the manifest.

### Manifest structure

```json
{
  "manifestVersion": 1,
  "releaseVersion": "1.2.1",
  "acceptedSourceVersion": "1.2.0",
  "migrationMetadataVersion": 1,
  "migrationDocumentation": "https://thunderid.dev/docs/migrations/1.2.0-to-1.2.1",
  "stores": [
    {
      "id": "config",
      "acceptedSourceLevel": "1.1.0",
      "requiredLevel": "1.1.0",
      "changed": false
    },
    {
      "id": "entity",
      "acceptedSourceLevel": "1.2.0",
      "requiredLevel": "1.2.1",
      "changed": true
    },
    {
      "id": "runtime_transient",
      "acceptedSourceLevel": "1.2.0",
      "requiredLevel": "1.2.1",
      "changed": true
    },
    {
      "id": "runtime_persistent",
      "acceptedSourceLevel": "1.1.4",
      "requiredLevel": "1.1.4",
      "changed": false
    }
  ]
}
```

| Field | Required | Meaning |
|---|---:|---|
| `manifestVersion` | Yes | Version of this JSON contract. It allows future manifest evolution without guessing its shape. |
| `releaseVersion` | Yes | ThunderID version containing this manifest. It must equal the normalized version from `version.txt`. |
| `acceptedSourceVersion` | Yes | Immediately preceding published ThunderID version from which this release accepts an upgrade. |
| `migrationMetadataVersion` | Yes | Migration-table format that the release creates and understands. It is compared with `MIGRATION_STATE.METADATA_VERSION`. |
| `migrationDocumentation` | Yes | Release-specific documentation used in preflight output and startup refusal messages. |
| `stores` | Yes | Complete list of logical stores supported by this release. The array is used so duplicate identifiers can be rejected explicitly. |
| `stores[].id` | Yes | Stable store identifier shared by configuration, database state, migration definitions and diagnostics. |
| `stores[].acceptedSourceLevel` | Yes | Exact completed level this target accepts for that store. It is the level required by the immediately preceding release. |
| `stores[].requiredLevel` | Yes | Exact completed level the store must have before this release can serve. |
| `stores[].changed` | Yes | Explicit administrator-facing declaration of whether this release changes the store. |

Versions are normalized semantic versions without a leading `v` inside the manifest and database
state. The build normalizes the current `version.txt` value before comparing it with
`releaseVersion`.

`changed` is deliberately explicit even though it can be checked against the two levels. This
satisfies the requirement that every release declare its migration impact. Release validation must
reject the manifest when `changed` is inconsistent with the levels or compiled migration registry.

The manifest does not contain operation ordering, SQL, checkpoint formats or implementation
function names. Those belong to the compiled migration plan, which is a separate design decision.
This manifest remains sufficient for startup compatibility checking, fresh initialization,
adjacent-release source validation, operator diagnostics and release tooling.

### Manifest validation

The release build and tests reject a manifest when:

- an unknown or duplicate JSON field is present;
- a required field or supported store is missing;
- a store identifier is duplicated or unknown;
- a version is not in canonical normalized semantic-version form;
- `releaseVersion` differs from the normalized `version.txt` value;
- `acceptedSourceVersion` is not the immediately preceding published release;
- a store's `acceptedSourceLevel` differs from the level required by that preceding release;
- an unchanged store changes its required level;
- a changed store does not advance to the current `releaseVersion`;
- a changed store has no compiled migration operations;
- an unchanged store has compiled migration operations; or
- `migrationDocumentation` is missing or does not identify this source-to-target transition.

## Decision 2: Database-side migration state

**Status:** Agreed, including the detailed columns, constraints and lifecycle below.

Each SQL database namespace that contains one or more ThunderID logical stores contains two system
metadata tables. A SQL database namespace means one SQLite database file, or one PostgreSQL database
and schema combination. Logical stores configured against the same namespace share the tables.
Stores in different files, PostgreSQL databases or PostgreSQL schemas have separate copies.

1. `MIGRATION_STATE`, the authoritative compatibility summary for each logical store.
2. `MIGRATION_PROGRESS`, the durable operation and checkpoint state needed to resume unfinished
   work.

`MIGRATION_STATE` is the only table normal serving startup needs to read. `MIGRATION_PROGRESS` is
owned by the migration process and is used for recovery and diagnostics.

These are store-scoped system tables. They are deliberately not deployment-owned application
tables and therefore do not contain the normally mandatory `DEPLOYMENT_ID` column. A deployment
identifier can appear as the value of a progress scope when an operation processes
deployment-scoped content. It never changes the completed level of the logical store into a
per-deployment level.

This decision applies to relational stores backed by PostgreSQL or SQLite. Redis migration is
deferred to a separate delivery phase because relational tables cannot be assumed there.

The structures below are logical schemas. The implementation design will translate them into
equivalent quoted, uppercase PostgreSQL and SQLite DDL.

### Table 1: `MIGRATION_STATE`

There is exactly one row for each logical store represented in the physical SQL database.

| Column | Logical type | Null | Meaning |
|---|---|---:|---|
| `ID` | `VARCHAR(36)` | No | UUID v7 primary key for the state row. |
| `STORE_ID` | `VARCHAR(64)` | No | Canonical logical-store identifier. It must match a store identifier in the embedded release manifest and is unique within the physical database. |
| `METADATA_VERSION` | `INTEGER` | No | Version of the migration-metadata format understood by the code. This is separate from the product and store migration levels. |
| `COMPLETED_LEVEL` | `VARCHAR(50)` | No | Latest store level whose operations and post-verification completed successfully. It is never advanced at the start of a migration. |
| `TARGET_LEVEL` | `VARCHAR(50)` | Yes | Level currently being attempted. It is null only when the row is clean. |
| `STATUS` | `VARCHAR(20)` | No | One of `clean`, `in_progress` or `failed`. Normal startup accepts only `clean`. |
| `ACTIVE_RUN_ID` | `VARCHAR(36)` | Yes | UUID v7 identifying the active or failed migration run. The orchestrator uses the same run identifier in every affected database for correlation. |
| `CURRENT_OPERATION_ID` | `VARCHAR(255)` | Yes | Stable identifier of the operation currently executing or most recently failed. It is null before the first operation and after successful completion. |
| `LAST_FAILURE_CODE` | `VARCHAR(64)` | Yes | Stable machine-readable category of the most recent failure affecting this migration attempt. |
| `LAST_FAILURE_SUMMARY` | `VARCHAR(1024)` | Yes | Bounded, sanitized administrator-facing explanation of the most recent failure. |
| `LAST_FAILED_AT` | UTC timestamp | Yes | Time at which the most recent failure was recorded. |
| `LEVEL_COMPLETED_AT` | UTC timestamp | No | Time at which `COMPLETED_LEVEL` was last established by fresh initialization, baseline adoption or migration. |
| `STARTED_AT` | UTC timestamp | Yes | Time at which the current migration attempt began. It is null when the row is clean. |
| `UPDATED_AT` | UTC timestamp | No | Time at which this state row was last changed. |

Required constraints:

- `ID` is the primary key.
- `STORE_ID` is unique.
- `STATUS` accepts only `clean`, `in_progress` or `failed`.
- A `clean` row has null `TARGET_LEVEL`, `ACTIVE_RUN_ID`, `CURRENT_OPERATION_ID` and `STARTED_AT`.
- An `in_progress` or `failed` row has non-null `TARGET_LEVEL`, `ACTIVE_RUN_ID` and `STARTED_AT`.
- `LAST_FAILURE_CODE`, `LAST_FAILURE_SUMMARY` and `LAST_FAILED_AT` are either all null or all
  present.
- `TARGET_LEVEL` must differ from `COMPLETED_LEVEL` when it is present.
- Versions are stored in the canonical normalized format selected for the manifest. The same parser
  and comparison rules are used for both sources.

`CURRENT_OPERATION_ID` may be null briefly after a run is opened but before its first operation is
selected. It is retained on failure to make the startup and migration diagnostics actionable.

Failure details are stored only in `MIGRATION_STATE`. A failed `MIGRATION_PROGRESS` row already
identifies the operation and scope, while the state row records the one causal failure that stopped
the sequential attempt. This also covers failures outside a scope, such as reservation
revalidation or store finalization. The summary must not contain credentials, secrets, raw identity
records, SQL text, connection details or an unbounded provider error.

The last failure fields remain available during recovery. They may temporarily remain on a clean
row after a successful resume until its report is delivered. They are cleared after successful
report delivery, not at store finalization. A later failed attempt may replace them after the
earlier attempt's report has been delivered.

The physical type mapping is:

| Logical type | PostgreSQL | SQLite |
|---|---|---|
| Bounded string | `VARCHAR(n)` | `TEXT` with the required length validation |
| Integer | `INTEGER` | `INTEGER` |
| UTC timestamp | `TIMESTAMPTZ` | `TEXT` containing a normalized UTC timestamp |

The application supplies normalized UTC timestamps consistently rather than relying on different
database-specific current-time expressions.

### Table 2: `MIGRATION_PROGRESS`

This table contains bounded, durable progress for the active target level. A row identifies one
operation scope. Atomic operations normally use one store-scoped row. Resumable operations may use
one row per deployment or stable batch partition.

| Column | Logical type | Null | Meaning |
|---|---|---:|---|
| `ID` | `VARCHAR(36)` | No | UUID v7 primary key for the progress row. |
| `MIGRATION_STATE_ID` | `VARCHAR(36)` | No | Foreign key to the owning `MIGRATION_STATE.ID`. |
| `RUN_ID` | `VARCHAR(36)` | No | Migration-run identifier. It must match the owning state's `ACTIVE_RUN_ID` while that run is active or failed. |
| `TARGET_LEVEL` | `VARCHAR(50)` | No | Store level being applied by this operation. |
| `OPERATION_ID` | `VARCHAR(255)` | No | Stable developer-defined identifier for the migration operation. |
| `OPERATION_ORDER` | `INTEGER` | No | Position of the operation within the target release's ordered migration plan. |
| `SCOPE_TYPE` | `VARCHAR(20)` | No | One of `store`, `deployment` or `batch`. |
| `SCOPE_ID` | `VARCHAR(255)` | No | Stable identity of this unit. For `store`, use the logical store identifier. For `deployment`, use the deployment identifier. For `batch`, use an operation-defined stable partition identifier. |
| `STATUS` | `VARCHAR(20)` | No | One of `in_progress`, `completed` or `failed`. |
| `CHECKPOINT_VERSION` | `INTEGER` | Yes | Version of the operation-specific checkpoint representation. It is required when `CHECKPOINT_DATA` is present. |
| `CHECKPOINT_DATA` | `TEXT` | Yes | Bounded JSON checkpoint owned and validated by the operation, such as the last committed cursor. It must not contain secrets or copied application records. |
| `STARTED_AT` | UTC timestamp | No | Time at which work on this scope began. |
| `UPDATED_AT` | UTC timestamp | No | Time at which this scope or its checkpoint last changed. |
| `COMPLETED_AT` | UTC timestamp | Yes | Time at which post-verification completed for this scope. It is present only for a completed row. |

Required constraints:

- `ID` is the primary key.
- `MIGRATION_STATE_ID` references `MIGRATION_STATE.ID` and deletes progress rows when the owning
  state row is deleted.
- The combination of `MIGRATION_STATE_ID`, `TARGET_LEVEL`, `OPERATION_ID`, `SCOPE_TYPE` and
  `SCOPE_ID` is unique.
- `STATUS` accepts only `in_progress`, `completed` or `failed`.
- `SCOPE_TYPE` accepts only `store`, `deployment` or `batch`.
- `COMPLETED_AT` is present only when `STATUS` is `completed`.
- `CHECKPOINT_VERSION` and `CHECKPOINT_DATA` are either both null or both present.
- Checkpoint data is bounded by an implementation-defined maximum size and is validated before it
  is stored or consumed.
- The unique constraint beginning with `MIGRATION_STATE_ID` supports operation and scope lookup.
  An additional index on `RUN_ID` and `STATUS` supports recovery of the active run.

Progress rows are retained throughout the migration run, including temporarily after an individual
store reaches `clean`, so a multi-store report can describe the complete run. After every operation
and scope required for a store has passed post-verification, one database transaction advances that
store's `COMPLETED_LEVEL`, clears its active fields and changes its state to `clean`; this transaction
does not yet delete the completed progress rows.

After the run reaches its final success or failure outcome, the migration process reads the retained
progress, builds the complete human-readable report and delivers it to the chosen output. If report
construction or delivery fails, completed progress remains available so report generation can be
retried without repeating migration operations.

Only after the complete multi-store run succeeds and its final report is delivered does the engine
delete that run's progress rows. A failed or incomplete run retains all progress, including completed
scopes and progress belonging to stores already clean at target. This preserves one coherent run for
resume, verification and reporting. Progress rows are not permanent history. The delivered final
report is the human-facing record, while each clean `COMPLETED_LEVEL` remains the database's
authoritative compatibility evidence.

### State transitions

```text
Fresh or legacy initialization
                    |
                    v
                 clean
                    |
             begin migration
                    v
              in_progress
               /       \
      verified failure  all post-verification passes
             v                         |
           failed                      v
             |                       clean
       verified resume          completed level advances
             |
             v
        in_progress
```

If the process terminates unexpectedly, the row remains `in_progress`. It is treated as incomplete,
not silently converted to `failed` or `clean`. Normal startup terminates for both `in_progress` and
`failed`. Only the explicit migration process can verify and resume the work.

### Example: clean store

The `config` store has completed level `1.1.0` and is safe for a binary that requires `1.1.0`. The
example shows the columns relevant to compatibility; its required timestamp columns are omitted for
readability.

| ID | STORE_ID | METADATA_VERSION | COMPLETED_LEVEL | TARGET_LEVEL | STATUS | ACTIVE_RUN_ID | CURRENT_OPERATION_ID |
|---|---|---:|---|---|---|---|---|
| `01991f6e-84b7-7e19-a449-1d73058586a1` | `config` | `1` | `1.1.0` | null | `clean` | null | null |

There are no `MIGRATION_PROGRESS` rows for this completed target.

### Example: interrupted deployment-scoped data migration

The `runtime_persistent` store completed `1.0.0` but was interrupted while moving to `1.1.0`. The
example shows the state and checkpoint columns relevant to recovery; required identifiers,
operation order and timestamps are omitted where they do not clarify the scenario.

`MIGRATION_STATE`:

| ID | STORE_ID | COMPLETED_LEVEL | TARGET_LEVEL | STATUS | ACTIVE_RUN_ID | CURRENT_OPERATION_ID |
|---|---|---|---|---|---|---|
| `01991f70-33cf-76d9-9628-c89349c73bb9` | `runtime_persistent` | `1.0.0` | `1.1.0` | `in_progress` | `01991f72-f8cf-7b48-9dd8-0b50beae25d1` | `1.1.0/backfill-session-subject` |

`MIGRATION_PROGRESS`:

| OPERATION_ID | SCOPE_TYPE | SCOPE_ID | STATUS | CHECKPOINT_VERSION | CHECKPOINT_DATA |
|---|---|---|---|---:|---|
| `1.1.0/backfill-session-subject` | `deployment` | `production` | `completed` | null | null |
| `1.1.0/backfill-session-subject` | `deployment` | `partners` | `in_progress` | `1` | `{"lastId":"01991f74-83f0-75c6-9228-fce4225cb7a2"}` |

The database still reports `1.0.0` as its last completed level. Startup terminates because the state
is not clean. A migration resume verifies the completed `production` scope, verifies and continues
the `partners` scope from its last committed checkpoint, and advances the store to `1.1.0` only
after store-wide post-verification passes.

### Scenario A: one physical database contains every logical store

The metadata tables are created once in that SQL database. `MIGRATION_STATE` contains independent
rows for the canonical stores represented there:

| STORE_ID | COMPLETED_LEVEL | STATUS |
|---|---|---|
| `config` | `1.1.0` | `clean` |
| `entity` | `1.0.0` | `clean` |
| `runtime_transient` | `1.1.0` | `clean` |
| `runtime_persistent` | `1.0.0` | `in_progress` |

```text
One physical SQL database
├── MIGRATION_STATE
│   ├── config row
│   ├── entity row
│   ├── runtime_transient row
│   └── runtime_persistent row
└── MIGRATION_PROGRESS
    └── rows belonging to any unfinished store operation
```

The tables are not duplicated or prefixed for each logical store. Startup compares each row
independently with the corresponding store requirement in the embedded manifest. One incompatible
row terminates the whole ThunderID startup.

### Scenario B: each logical store uses a separate physical database

Each physical SQL database has the same metadata-table definitions but contains only the state rows
for logical stores located there. With four separate databases, the common case is one state row per
database:

```text
Config database
├── MIGRATION_STATE: config row
└── MIGRATION_PROGRESS: config progress only

Entity database
├── MIGRATION_STATE: entity row
└── MIGRATION_PROGRESS: entity progress only

Runtime-transient database
├── MIGRATION_STATE: runtime_transient row
└── MIGRATION_PROGRESS: runtime_transient progress only

Runtime-persistent database
├── MIGRATION_STATE: runtime_persistent row
└── MIGRATION_PROGRESS: runtime_persistent progress only
```

The migration run identifier may be repeated across these databases to correlate one orchestrated
run, but no database's state is inferred from another database. A backup or restore carries the
state of only the logical stores contained in that database.

### Scenario C: hybrid physical layout

The same rules cover a mixed configuration. If `config` and `entity` share one database while the
two runtime stores are separate, the shared database has two `MIGRATION_STATE` rows and each runtime
database has one. No separate migration database is introduced.

A PostgreSQL server containing separate database catalogs is treated as multiple physical databases
for this purpose because tables and transactions are isolated by catalog. Logical stores sharing the
same SQL catalog and schema share one pair of metadata tables.

### Startup comparison

For every store required by the embedded manifest, startup performs the following read-only checks:

1. Locate the configured physical store.
2. Read the `MIGRATION_STATE` row whose `STORE_ID` matches the manifest store identifier.
3. Verify that the metadata format is supported.
4. Verify that `STATUS` is `clean`.
5. Compare `COMPLETED_LEVEL` with the manifest's required level using exact normalized equality.

A missing row, unsupported metadata version, unreadable state, non-clean status or level mismatch
terminates startup before ThunderID activates serving or background work.

## Decision 3: Startup compatibility gate

**Status:** Agreed

The compatibility gate is a mandatory, read-only boundary between loading enough static deployment
configuration to locate persistent stores and initializing application behavior that depends on
those stores. It uses the embedded release manifest as the required state and the authoritative
state held with each store as the achieved state. For SQL-backed stores that authority is the
`MIGRATION_STATE` row. The initial relational phase does not apply this gate to Redis-backed data.
The later Redis migration phase must define its compatibility evidence without assuming SQL tables.

### Modes governed by the gate

The gate applies to:

- normal serving startup;
- bootstrap, because it writes built-in resources through application services; and
- any future command that reads or writes application data using the normal ThunderID service
  layer.

The serving compatibility gate does not block these dedicated migration-management modes:

- preflight check;
- migration apply or resume;
- migration status;
- fresh initialization; and
- baseline registration through initialize.

These modes are not unverified. Each performs its own mode-specific source, structure, state,
permission and reservation checks. They are allowed to inspect an incompatible store because assessing
or bringing that store to a compatible state is their purpose.

### Gate sequence

```text
Determine invocation mode
          |
          v
Load and validate embedded release manifest
          |
          v
Load enough deployment configuration to locate every store
          |
          v
Open minimal persistent-store connections
          |
          v
Read and evaluate authoritative state for every required store
          |
          v
Collect every compatibility failure
          |
     all compatible?
       /       \
     yes        no
      |          |
Initialize      Report all failures,
application     close resources and
behavior        terminate non-zero
```

The gate runs before request listeners, background workers, schedulers, bootstrap imports, caches
that load application state, or normal service registration can become active. Implementations may
create only the minimal configuration and database-provider facilities needed to perform the
read-only check.

### Per-store evaluation

For every store in the embedded manifest, the gate:

1. Resolves the store's configured storage location and authoritative migration-state source.
2. Reads the authoritative state matching the manifest's store identifier. For a SQL-backed store,
   this is its `MIGRATION_STATE` row.
3. Validates the migration metadata format.
4. Requires `STATUS` to be `clean`.
5. Compares `COMPLETED_LEVEL` with the manifest's `requiredLevel` using canonical semantic-version
   parsing and exact normalized equality.

SQL-backed stores sharing one SQL database namespace are evaluated independently even when their
rows are obtained by one query. Failure of one required store fails the whole process. Stores in
separate namespaces or non-SQL state locations remain independent evidence and are never represented
by an aggregate lowest level.

The gate evaluates every required store that it can reach and reports all discovered failures in one
run. A failure to connect to one storage location does not prevent checks against other reachable
locations.

### Outcomes

| Observed state | Gate result | Required diagnostic and action |
|---|---|---|
| Row exists, metadata format is supported, status is `clean`, and completed level equals required level | Pass for that store | Report as compatible when verbose or status output is requested. |
| Completed level is lower than required | Fail as behind | Show recorded and required levels and direct the administrator to the migration command and release-specific documentation. |
| Completed level is higher than required | Fail as ahead | State that the binary is older than the data and that the matching newer binary is required. Do not suggest a down migration. |
| Status is `in_progress` | Fail as interrupted or still running | Show target level, run identifier and current operation when recorded. Direct the administrator to inspect or resume the run. |
| Status is `failed` | Fail as incomplete | Show target level, run identifier and failed operation when recorded. Direct the administrator to the recovery guidance. |
| Metadata table or store row is missing | Fail as unestablished | Direct the administrator to migration initialize. Startup never creates or stamps the row. |
| Metadata format is unsupported | Fail as unsupported metadata | Show recorded and supported metadata versions and direct the administrator to the required intermediate release or recovery guidance. |
| Store is unreachable or state cannot be read | Fail as unverifiable | Identify the configured store without printing credentials or a sensitive connection string. |
| Manifest itself is invalid | Fail as invalid release artifact | Terminate immediately and identify the embedded-manifest defect. This should already have been prevented by build validation. |

The process exits with a non-zero status if any required store fails. It does not continue in a
partial-serving or degraded mode.

### Read-only and verification boundaries

The gate never:

- creates migration tables or rows;
- infers a level from the apparent database structure;
- stamps, repairs or resumes state;
- executes migration pre-verification or post-verification operations;
- validates every application table or content record; or
- acquires credentials capable of altering schema solely for startup checking.

Initialization stamps trusted levels without structural/content verification. During migration,
structural and content correctness is established by
migration post-verification before `COMPLETED_LEVEL` advances. Repeating those expensive checks on
every startup would duplicate the migration process. Startup trusts a clean authoritative state row
only after validating its metadata format and exact required level.

### Configuration and initialization relationship

Static deployment configuration is loaded before the gate because it identifies the configured
stores. A configuration error may therefore terminate the process before database compatibility is
evaluated. Dynamic configuration and application data must not be loaded through normal services
until the gate passes.

A fresh installation registers manifest target levels using the trusted creation-script path first. Bootstrap then passes the gate and
creates the required built-in resources. A migration-management mode can use migration-only
components before the gate, but it cannot activate serving or normal background behavior.

### Concurrency boundary

The gate proves compatibility at the time it reads the state. It does not hold a serving-process
lock for the lifetime of ThunderID and cannot guarantee that an administrator will not start a
migration afterward. Ensuring affected instances are stopped remains a documented administrator
responsibility, reinforced by command-output reminders rather than a mandatory acknowledgement.

## Decision 4: Compiled migration plan and operation model

**Status:** Agreed, including operation categories, plan structure, repository persistence, release
selection, binary packaging and detailed operation contracts.

### Agreed operation categories

The migration engine supports two execution categories under one operation lifecycle:

1. `SQLOperation` executes explicitly registered database statements. It is preferred for schema,
   set-based data and content changes that can be expressed safely and verified for every supported
   database type.
2. `ProgrammaticOperation` executes compiled migration-specific Go behavior. It is used for complex
   transformations, controlled batching, cryptographic or domain logic, and operations that require
   selected ThunderID components.

`ProgrammaticOperation` does not mean starting or freely using the normal runtime service graph. It
receives only migration-scoped dependencies and must not activate listeners, background workers,
caches with application side effects, or other serving behavior.

Both categories have the same required lifecycle:

```text
Developer-authored pre-verification
                 |
                 v
       SQL or programmatic execution
                 |
                 v
Developer-authored post-verification
```

Every operation also declares a stable identifier, one writable logical store, interruption mode
and progress scope. An operation may read another store when necessary, but it writes and records
completion against exactly one logical store. A change that writes several stores is decomposed into
globally ordered operations so that each store retains independent completion and recovery state.

### Agreed SQL-operation rules

- Migration SQL is stored only in typed, source-controlled Go definitions and is compiled into the
  migration-aware executable. The engine does not load executable migration SQL from a distribution
  file.
- A SQL operation contains explicitly ordered statements. The engine does not implement a general
  SQL-script parser or split arbitrary scripts on semicolons.
- Every statement has a stable query identifier and an explicitly validated implementation for
  every supported SQL database type.
- A generic SQL fallback must not allow a newly supported database type to execute a statement that
  was never reviewed and tested for it.
- Being an SQL operation does not imply atomicity. The developer declares and tests its interruption
  behavior.

### Agreed programmatic-operation rules

- Programmatic operations are compiled into the target release and use migration-scoped
  dependencies.
- They may use selected ThunderID domain behavior where required, but must not depend on a running
  server or silently inherit the process-wide configured deployment as their only scope.
- They must participate in the engine's transaction and progress contracts rather than open hidden
  independent transactions that the engine cannot track.
- Being a programmatic operation does not imply resumability. The developer declares and tests its
  interruption behavior.

### Release-plan structure

Each target release compiles one explicit plan for the immediately preceding release it accepts:

```go
type MigrationPlan struct {
    SourceVersion string
    TargetVersion string
    Operations    []Operation
}
```

The `Operations` slice is the authoritative global order. A separate sequence field is not stored in
each operation because that would duplicate the slice order and create two values that could
disagree. The engine derives `OPERATION_ORDER` for progress records from the operation's position in
the validated slice.

The plan is constructed explicitly:

```go
func BuildMigrationPlan() MigrationPlan {
    return MigrationPlan{
        SourceVersion: "1.2.0",
        TargetVersion: "1.2.1",
        Operations: []Operation{
            addClientTypeColumn(),
            backfillUserType(),
            updatePersistentSessions(),
        },
    }
}
```

Automatic package initialization, reflection and filesystem discovery are not used to register
operations. Explicit construction makes the complete order visible in review and deterministic in
tests.

The plan has no separate identifier. The normalized source-to-target version pair is its identity.
An empty operation list is valid only when the embedded manifest marks every store unchanged.

### Plan validation

Before preflight or execution, build tests and the runtime loader validate that:

- source and target versions exactly match the embedded manifest;
- the target is the release containing the plan and the source is its accepted adjacent release;
- operation identifiers are non-empty and globally unique within the plan;
- every operation targets a store declared by the manifest;
- every store marked `changed` has at least one operation;
- every store marked unchanged has no operation;
- every operation's provider coverage, verification functions, interruption mode and scope are
  complete; and
- the operation slice is deterministic and has not been altered by runtime configuration.

### Repository persistence

Released migration operations remain in version-specific source packages. A new target release adds
a package instead of modifying the package belonging to an already published release.

```text
backend/internal/system/migration/
├── contract/
│   └── shared plan, operation and verification types
├── engine/
│   └── version-neutral orchestration
├── current/
│   └── explicit binding to the release being built
└── releases/
    ├── v1_1_0/
    │   ├── release-manifest.json
    │   ├── release.go
    │   ├── plan.go
    │   └── config_add_client_type.go
    └── v1_2_0/
        ├── release-manifest.json
        ├── release.go
        ├── plan.go
        └── ...
```

The directory name uses a Go-compatible normalized release version. `release-manifest.json` remains
the canonical manifest for that release. `release.go` embeds it and exposes the release definition,
while `plan.go` explicitly constructs the globally ordered `MigrationPlan`.

All migration SQL, including longer statements, uses typed Go definitions and raw string literals.
Each `SQLOperation` provides one explicit `SQLImplementation` for every supported SQL database type,
and each implementation contains its own ordered `[]SQLStatement`. The engine neither discovers
files nor infers execution order from filenames.

The existing `.sql` files under `backend/dbscripts` remain the fresh-database creation path. They are
not migration-operation definitions: creation scripts construct the current schema from nothing,
while compiled migration statements transform an existing store across one accepted adjacent
release transition.

Migration-specific transformation behavior stays in its release package. Version-neutral execution,
state and verification contracts stay in the shared packages. Released operations are not edited to
correct later behavior; a subsequent release adds a corrective operation.

### Current-release binding

The `current` package contains an explicit import and binding to exactly one release package:

```go
package current

import (
    "github.com/thunder-id/thunderid/internal/system/migration/contract"
    release "github.com/thunder-id/thunderid/internal/system/migration/releases/v1_2_0"
)

func Definition() contract.ReleaseDefinition {
    return release.Definition()
}
```

Creating a release therefore requires two deliberate changes:

1. Add its immutable version-specific release package.
2. Change the `current` binding to that package.

The binding is not selected from administrator configuration, an environment variable, runtime
filesystem discovery or the readable distribution manifest.

### Binary packaging

Every executable that performs compatibility checking or migration imports the `current` package.
That package imports only the selected release package. Go therefore compiles the selected manifest,
plan, typed SQL definitions and programmatic operations into the executable. Historical release
packages remain in the repository for traceability and tests but are not imported into, and
therefore are not packed into, the current distribution binary.

This is especially important for the adjacent-release rule. For example, the `1.2.0` executable
contains the `1.1.4` to `1.2.0` transition it owns when `1.1.4` is the immediately preceding
published release. It does not become a universal client containing every
historical migration. An older installation is advanced by using each target release's own artifact
in order.

The packaging step copies the selected release package's canonical `release-manifest.json` into the
distribution for human inspection. Build validation compares its bytes or digest with the embedded
manifest before packaging completes.

### Persistence and packaging validation

Build and release checks verify that:

- the `current` package binds exactly one release definition;
- the selected release version equals the normalized `version.txt` value;
- the embedded manifest is byte-for-byte identical to the canonical manifest copied into the
  distribution;
- the compiled plan passes the manifest and operation validations;
- every `SQLOperation` provides an explicitly validated `SQLImplementation` for every supported SQL
  database type;
- no operation depends on executable migration SQL outside the binary; and
- compatibility-checking and migration entry points expose the same embedded release definition.

### Common operation contract

The engine consumes one common `Operation` interface regardless of execution category:

```go
type Operation interface {
    Definition() OperationDefinition
    CheckReadiness(ctx context.Context, access ReadAccess, plan PlanContext) (ReadinessResult, error)
    ResolveScopes(ctx context.Context, access ReadAccess) ([]Scope, error)
    PreVerify(ctx context.Context, access ReadAccess, scope Scope, checkpoint *Checkpoint) (VerificationResult, error)
    Execute(ctx context.Context, execution OperationExecution) error
    PostVerify(ctx context.Context, access ReadAccess, scope Scope) (VerificationResult, error)
}
```

`OperationExecution` is an engine-owned execution envelope, not migration progress data. For an
atomic operation it supplies transaction-bound `ExecutionAccess`. The `Execute` shape shown above is
not final for resumable operations: the later engine-driven batch refinement replaces an
operation-owned loop with selection, transformation and checkpoint-validation callbacks. The final
common interfaces must represent these modes explicitly without asking developers to construct or
inspect engine orchestration state.

The release package does not implement arbitrary engine orchestration. It constructs an
`SQLOperation` or `ProgrammaticOperation` through migration-contract constructors. Those concrete
types implement the common interface and keep lifecycle behavior consistent.

The engine alone decides when to acquire locks, create transactions, write `MIGRATION_STATE`, write
or delete `MIGRATION_PROGRESS`, stop after a failure, and advance a completed level. An operation
cannot report itself complete or alter the store-level migration record.

### Operation definition

```go
type OperationDefinition struct {
    ID               string
    Description      string
    StoreID          StoreID
    ReadStoreIDs     []StoreID
    InterruptionMode InterruptionMode
    ScopeType        ScopeType
}
```

| Field | Meaning |
|---|---|
| `ID` | Stable, globally unique identifier within the release plan. It includes the target version and owning store for readable diagnostics. |
| `Description` | Short human-readable explanation used in reports. It is not an identifier. |
| `StoreID` | The one logical store the operation may modify and against which completion is recorded. |
| `ReadStoreIDs` | Other logical stores the operation is allowed to read. The writable store does not need to be repeated here. |
| `InterruptionMode` | Either `atomic` or `resumable`. It governs transaction and checkpoint behavior, not whether the implementation is SQL or programmatic. |
| `ScopeType` | Either `store`, `deployment` or `batch`, matching the progress-record contract. |

The execution category is derived from the concrete operation type. It is not repeated in
`OperationDefinition`, avoiding a value that could disagree with the actual implementation.

### Scope contract

```go
type Scope struct {
    Type ScopeType
    ID   string
}
```

`ResolveScopes` is read-only and returns the complete stable scopes the operation must process:

- A store-scoped operation uses one scope whose ID is the owning `StoreID`.
- A deployment-scoped operation discovers every affected deployment identifier from the stored data.
- A batch-scoped operation returns stable partitions, not one scope for every application record.

The engine rejects empty, duplicate or mismatched scopes. It records each scope through
`MIGRATION_PROGRESS`. Applying migration resolves scopes again after establishing the store
reservations, so it does not rely only on an earlier optional preflight result.

Batching inside a deployment remains an operation-owned checkpoint detail. A developer should not
create both deployment and batch scope rows for the same logical work merely to represent nested
loops.

### Verification contract

```go
type VerificationState string

const (
    VerificationSource  VerificationState = "source"
    VerificationTarget  VerificationState = "target"
    VerificationInvalid VerificationState = "invalid"
)

type VerificationResult struct {
    State    VerificationState
    Summary  string
    Findings []Finding
}
```

Pre-verification classifies each scope:

- `source` means execution may begin for that scope.
- `target` means the scope already has the exact required result and may be skipped only after that
  result has been verified.
- `invalid` means the observed state is neither a valid source nor the exact target, so migration
  stops without executing the scope.

An error means verification could not be completed, such as an unavailable store or failed query.
It is distinct from a successful verification that returns `invalid`.

Post-verification must return `target`. Returning `source`, `invalid`, or an error fails the
operation and prevents the owning store's completed level from advancing.

`Summary` and `Findings` contain safe diagnostic information and must not include credentials,
secrets or copied identity data.

### Access boundaries

`ReadAccess` exposes query-only clients for the writable store and every declared `ReadStoreID`.
It has no execution or transaction method. This makes pre-verification, post-verification and scope
discovery read-only by construction.

`ExecutionAccess` exposes:

- write access only to the operation's `StoreID`;
- read-only access to declared dependent stores;
- the selected database or store-provider type;
- migration-scoped dependencies explicitly required by a programmatic operation; and
- the engine-owned context, including any transaction opened for an atomic operation.

It does not expose an unrestricted global database provider or runtime service locator. A request
for an undeclared store fails rather than returning a client.

Resumable operations receive only the bounded access required by the engine-driven callback currently
being invoked. They do not receive a developer-facing `BatchCommitter`, unrestricted writable access
or transaction control. The final callback/access types remain an open contract-design item.

### SQL execution contract

```go
type SQLStatement struct {
    ID    string
    Query string
    Args  SQLArgsFunc
}

type SQLImplementation struct {
    Statements []SQLStatement
}
```

- Each supported SQL database type has one explicit `SQLImplementation`; there is no generic
  fallback.
- A provider implementation may contain a different number of statements from another provider's
  implementation. For example, PostgreSQL may use one `ALTER TABLE` while SQLite uses an ordered
  table-reconstruction sequence.
- A statement `ID` is stable and unique within its provider implementation and operation.
- `Query` contains the compiled SQL for that provider.
- `Args` derives bound parameters from the current scope. It never interpolates a scope identifier
  into SQL text. It is nil when the statement has no parameters.
- `Statements` slice position defines execution order. Provider-map iteration never defines
  execution order.
- The engine passes each selected statement through the existing database execution and transaction
  facilities rather than creating a second SQL driver layer.

Pre-verification and post-verification remain functions even for an `SQLOperation`. They can issue
one or more read-only queries and evaluate database-specific metadata into the common
`VerificationResult`. This is more precise than requiring every database to express complex schema
classification as one boolean SQL query.

### Concrete operation structures

```go
type SQLOperation struct {
    DefinitionValue  OperationDefinition
    ReadinessChecker ReadinessFunc
    ScopeResolver    ScopeResolver
    PreVerifier      VerificationFunc
    Implementations  map[DatabaseType]SQLImplementation
    PostVerifier     VerificationFunc
}

type ProgrammaticOperation struct {
    DefinitionValue  OperationDefinition
    ReadinessChecker ReadinessFunc
    ScopeResolver    ScopeResolver
    PreVerifier      VerificationFunc
    Executor         ProgrammaticExecutionFunc
    PostVerifier     VerificationFunc
}
```

Contract constructors validate all required functions and values. A store-scoped helper supplies the
standard single-store scope resolver, but pre-verification and post-verification are never omitted or
replaced with unconditional success helpers.

An `SQLOperation` selects the implementation matching the configured database type and executes that
implementation's statements sequentially through the engine. A `ProgrammaticOperation` invokes its
executor once per resolved scope. Neither concrete type controls its own outer lifecycle or
store-level completion state.

## Decision 5: Migration engine execution lifecycle

**Status:** Agreed across initial validation, store reservation, ordered execution, resume, store
finalization, reporting, cleanup and explicit recovery.

### Stage 1: Initial validation and preflight

**Status:** Agreed.

The engine distinguishes a plan-wide readiness check from an operation's immediate source-state
verification.

This distinction is necessary when operations depend on earlier operations. Consider a plan that
first adds a column and then backfills it. Before the first operation runs, the backfill operation
cannot observe its immediate source state because the column does not exist yet. Treating that as an
invalid migration would make a correct plan fail preflight. Treating it as immediately executable
would make pre-verification imprecise.

The checks are therefore:

1. `CheckReadiness` is developer-authored and read-only. It determines whether the release source,
   declared preceding operations and available database capabilities make the operation viable as
   part of the complete plan.
2. `PreVerify` is developer-authored and read-only. It runs immediately before a resolved scope is
   executed and requires the scope to be exactly at the operation's source state or already at its
   exact target state.
3. `PostVerify` is developer-authored and read-only. It runs after execution and requires the exact
   target state.

For an independent operation, `CheckReadiness` and `PreVerify` may share validation helpers. They
remain separate engine phases so a dependent operation is never forced to pretend that an earlier
operation has already executed.

The common operation contract in Decision 4 includes:

```go
CheckReadiness(
    ctx context.Context,
    access ReadAccess,
    plan PlanContext,
) (ReadinessResult, error)
```

`PlanContext` exposes only the validated source and target versions, the operation's position and
the identifiers of preceding operations. It does not allow the readiness check to execute or
simulate those operations.

The engine constructs an immutable context for each operation:

```go
type PlanContext struct {
    sourceVersion         string
    targetVersion         string
    operationIndex        int
    precedingOperationIDs []OperationID
}
```

Read-only methods expose `SourceVersion`, `TargetVersion`, `OperationIndex`, a copied list of
`PrecedingOperationIDs` and a `Precedes(OperationID)` lookup. `OperationID` is a distinct string
type. The context contains plan structure only. It does not contain database clients, transactions,
migration progress, execution functions, runtime services or assumed results of preceding
operations. Live database observations remain available through `ReadAccess`.

### Readiness result contract

`CheckReadiness` is required for every operation, but it is not required to claim that every exact
condition can already be verified. It returns one of three explicit states:

```go
type ReadinessState string

const (
    ReadinessReady    ReadinessState = "ready"
    ReadinessDeferred ReadinessState = "deferred"
    ReadinessBlocked  ReadinessState = "blocked"
)

type ReadinessResult struct {
    State         ReadinessState
    Summary       string
    Findings      []Finding
    DeferredUntil []string
}
```

- `ready` means every condition that must exist before the plan begins was checked successfully.
- `deferred` means an exact condition cannot exist or be observed until the listed preceding
  operations complete. It is disclosed in the preflight report and is not treated as successful
  pre-verification.
- `blocked` means a known incompatibility prevents the operation or the developer cannot safely
  determine readiness for a reason explained in the findings.

An error means the readiness check itself could not be completed and blocks migration. A deferred
result is accepted only when every identifier in `DeferredUntil` exists earlier in the validated
plan and the result explains what must be verified later. Deferral without a valid preceding
operation is rejected.

The engine may continue from Stage 1 when every operation is either `ready` or validly `deferred`.
When a deferred operation is eventually reached during apply, its exact `PreVerify` remains
mandatory. Failure at that point stops the migration before the operation executes.

This makes readiness explicit rather than optional. Omitting `CheckReadiness` would make it
impossible to distinguish a deliberately deferred check from a developer accidentally forgetting
to implement it. Even when exact verification is deferred, the check can still validate provider
support, accessible input stores and other conditions that are observable at the release source.

### Standalone preflight sequence

The optional preflight command performs no writes and does not reserve any store:

```text
Load and validate embedded manifest and compiled plan
                       |
Load and validate target deployment configuration
                       |
Resolve every required store and provider
                       |
Read and classify migration state for every store
                       |
Validate plan applicability and resume consistency
                       |
Check every permission that can be established read-only
                       |
Run every operation's plan-wide readiness check
                       |
Validate the requested report destination
                       |
Return one aggregate readiness report
```

The command continues checking independent stores and operations after a failure so the
administrator receives all discoverable blockers in one run. A failed dependency is reported
against the dependent operation without running checks that would produce misleading results.

Exact scope discovery and `PreVerify` are not required during this unlocked whole-plan preflight.
Some scopes or source-state structures may be created by preceding operations. They are resolved
and verified immediately before their operation during apply.

Because the command holds no locks and changes nothing, its success is not a guarantee that apply
will succeed. State, connectivity or permissions may change afterward.

### Initial apply validation

Running the standalone preflight command is optional, but `apply` always performs the same initial
read-only validation before making any change. This prevents a direct
apply invocation from skipping safety checks.

Apply displays reminders that the administrator must stop affected instances and maintain a
recoverable backup. It requires no offline or backup acknowledgement flags and never waits for
interactive input. After initial validation succeeds, apply proceeds to the next lifecycle stage,
deterministic store reservation. After every required
store is reserved, the engine rereads authoritative state and repeats the readiness checks because
the result obtained before reservation may already be stale.

### Initial store-state classification

For each store, initial validation classifies the authoritative record against the embedded
manifest:

| Store state | Initial result |
|---|---|
| Clean at `acceptedSourceLevel` and the store is changed by this release | Pending and eligible for migration. |
| Clean at `requiredLevel` | Already complete. Do not execute its operations, but include it in the report. |
| Clean at any other level | Refuse before reserving a store or changing anything. |
| `in_progress` or `failed` toward this release's `requiredLevel`, with a run and operation recognized by the compiled plan | Eligible only for verified resume. |
| `in_progress` or `failed` toward another target, or referencing an unknown operation | Refuse as inconsistent state. |
| Missing or unsupported metadata | Refuse and direct the administrator to initialization, an intermediate release or recovery as applicable. |
| Unreachable or unreadable | Refuse as unverifiable. |

A mixture of stores already at `requiredLevel` and stores at `acceptedSourceLevel` is permitted. It
is the expected recoverable state after an earlier multi-store attempt completed some stores before
failing elsewhere. Each store is still evaluated independently; no aggregate migration level is
invented.

### Stage 1 outcome

Stage 1 returns a structured result containing:

- source, target and status for every store;
- whether the run is a new application, verified resume or no-op;
- every operation considered;
- all readiness and permission findings;
- administrator responsibility reminders; and
- whether store reservation may begin.

No migration table, progress row, schema, application content or configuration is modified during
this stage.

### Stage 2: Store reservation and authoritative revalidation

**Status:** Agreed.

Before executing operations, a migration run reserves every affected logical store by atomically
recording its run identifier and target level in `MIGRATION_STATE`. The existing `STATUS`,
`ACTIVE_RUN_ID`, `TARGET_LEVEL` and timing fields hold the reservation with the data being
protected. This is a migration-engine reservation rather than a long-running database transaction
or an expiring lease.

This keeps the behavior consistent across supported database types and avoids claiming that an
expired lease proves its previous owner has stopped. Automatic lease takeover is unsafe because a
paused or partitioned process could resume after another process has begun writing.

#### New-run reservation sequence

After Stage 1 succeeds, the engine:

1. Generates one UUID v7 run identifier for the complete multi-store run.
2. Visits pending logical stores in canonical manifest order.
3. For each store, performs a short transaction containing a conditional update from the exact
   clean accepted source state to `in_progress`, with the target level and run identifier.
4. Treats a zero-row update as a concurrent change and stops store reservation.
5. After every required reservation succeeds, rereads every store and reruns plan-wide readiness
   before the first operation is selected.

The conditional update checks the metadata version, completed level, clean status and empty active
fields. It never overwrites an unexpected state. Stores already verified at the required level are
reported as complete and are not reserved.

If reservation fails before any migration operation begins, the engine attempts to cancel only the
reservations created by its run. Cancellation also uses a conditional update and succeeds only when
the run identifier still matches, no current operation is recorded and no progress row exists.
Failure to cancel a reservation is reported as recovery work rather than being hidden.

#### Resume reservation sequence

An orderly operation failure changes the affected state from `in_progress` to `failed` while
retaining its run identifier, target, current operation and progress. Resume first verifies that all
non-clean stores refer to the same recognized run and target. It then conditionally changes the
eligible `failed` rows back to `in_progress`. A clean store already completed by that run remains
untouched.

An existing `in_progress` row is never taken over automatically, even when its timestamp appears
old. A second normal apply or resume attempt refuses it as a possible concurrent executor.

If a process terminates without changing `in_progress` to `failed`, an explicit recovery action is
required. The administrator must first ensure the former process is no longer running, then identify
the expected run before the recovery action conditionally marks it failed. Normal verified resume
can follow. The detailed recovery command belongs to a later lifecycle stage.

#### What the reservation does and does not protect

The reservation prevents two migration engines from legitimately operating on the same logical
store. It also leaves visible evidence after a process crash. It does not lock the database or stop
a normal ThunderID process that was already running before the reservation, or an external database
administrator, from writing. Quiescing those writers remains the administrator's responsibility under
the documented deployment procedure; a migration reservation does not prove that they stopped.

Stage 2 makes no schema or application-content changes. Its only writes are the conditional
migration-state transitions needed to reserve the stores for one migration run.

### Stage 3: Ordered operation execution and resume

**Status:** Agreed.

After every store reservation and the reserved-state revalidation succeed, the engine walks the
compiled `Operations` slice in its declared global order. It does not reorder operations by store,
execution category or database provider. Resume also walks the plan from the beginning and
reconciles durable progress instead of trusting a stored operation identifier as an instruction to
jump forward.

For each pending operation, the engine performs this outer lifecycle:

```text
Confirm owning store is reserved by this run
                    |
Record CURRENT_OPERATION_ID
                    |
Resolve and deterministically order scopes
                    |
Reconcile scopes with retained progress
                    |
PreVerify each scope using its progress snapshot
                    |
Execute, resume, or skip according to verified state
                    |
PostVerify every completed scope
                    |
Continue to the next globally ordered operation
```

An operation cannot advance its store level. After the last operation owned by a store completes,
the engine performs a separate store-finalization check. It advances that store only after every
operation and scope owned by the store has exact target verification. Deployment-scoped work must
therefore complete for every discovered deployment before the store becomes clean.

#### Verification of partially completed scopes

**Status:** Agreed.

The existing three-state verification model needs one refinement for resumable operations. A scope
with committed batches may be neither at its pristine source nor at its final target while still
being valid to resume. Calling this state `source` would hide an important distinction.

Stage 3 therefore proposes a fourth verification state:

```go
const VerificationCheckpoint VerificationState = "checkpoint"
```

`checkpoint` means the observed data is exactly consistent with a retained, validated checkpoint
and execution may resume after that checkpoint. It is valid only for a resumable operation with a
matching progress record. An atomic operation, or a scope without retained progress, cannot return
it.

`PreVerify` consequently receives only an optional read-only checkpoint:

```go
type Checkpoint struct {
    Version int
    Data    []byte
}

PreVerify(
    ctx context.Context,
    access ReadAccess,
    scope Scope,
    checkpoint *Checkpoint,
) (VerificationResult, error)
```

A nil checkpoint means there is no durable resume position for that scope. Checkpoint data is
copied, bounded and validated before it reaches the operation. The operation cannot modify the
stored value. Progress-row status, run identifiers and timestamps remain private engine state.

The write-side contract is separate:

```go
// The final engine-driven SelectNext, ExecuteBatch and checkpoint-validation
// callback types remain to be designed. Operations do not own the batch loop.
```

The engine opens each batch transaction, invokes the operation's bounded selection/transformation
callbacks, persists the next checkpoint and commits both data and checkpoint together. Any internal
committer is an engine implementation detail. The older developer-facing `BatchCommitter` and
`ProgressRecorder` concepts are superseded and do not coexist with the final callback contract.

#### Pre-verification outcomes

| Durable progress | Verified state | Engine action |
|---|---|---|
| None | `source` | Create progress and execute from the beginning. |
| None | `target` | Record verified completion without executing. |
| None | `checkpoint` | Refuse because no durable checkpoint supports the partial state. |
| Incomplete without a checkpoint | `source` | Retry from the beginning according to the operation's interruption contract. |
| Incomplete with a checkpoint | `checkpoint` | Resume after the validated checkpoint. |
| Incomplete | `target` | Treat prior execution as committed, post-verify and record completion without repeating it. |
| Incomplete with a checkpoint | `source` | Refuse because data and recorded progress disagree. |
| Completed | `target` | Retain completion and skip execution. |
| Completed | Any state other than `target` | Refuse because durable completion and observed data disagree. |
| Any | `invalid` or verification error | Stop before executing the scope. |

The `target` cases handle interruption after data committed but before the progress status was
updated. They do not rely on blind operation-level idempotence because the exact target is verified
first.

#### Atomic operation boundary

**Status:** Agreed.

For an `atomic` operation scope, the engine uses one provider transaction for pre-verification,
execution, post-verification and progress completion. Reads used by verification must observe that
same transaction. If any statement, programmatic action, verification or progress update fails, the
transaction rolls back and no partial application change or completed progress survives.

The operation contract is rejected for a provider that cannot provide the transactional behavior
the operation declares. Atomicity covers one scope in one writable store. It does not promise one
transaction across multiple scopes, logical stores or physical databases.

#### Resumable operation boundary

**Status:** Agreed.

A `resumable` operation may commit bounded units of work. Every mutation and the checkpoint that
acknowledges it must commit atomically together whenever the target store supports transactions.
The engine owns that transaction and does not permit an independent application-data commit. It
loads and validates the retained checkpoint, requests the next batch through the operation callback,
executes the transformation with transaction-bound access, and advances the checkpoint only with the
same commit. The operation never owns the loop or receives writable access outside the current batch.

After the executor reports that a scope has no remaining work, exact post-verification must return
`target` before the progress row becomes `completed`. A checkpoint is recovery information, not
evidence that the scope completed.

Provider types that cannot transactionally couple a mutation with its checkpoint require a
provider-specific interruption contract. That contract must be designed before such an operation
is accepted; the engine must not silently apply SQL transaction assumptions to a future
non-transactional store.

The initial framework does not permit a resumable operation to perform an irreversible external
side effect. An external system cannot participate in the transaction that couples the target-store
mutation with its checkpoint. Supporting such an effect would require a separately reviewed
idempotency and reconciliation contract.

#### Scope reconciliation during resume

**Status:** Agreed.

Scopes are resolved again after reservations are established. The engine sorts them by stable scope type and ID,
then compares them with retained progress:

- duplicate or malformed scopes are rejected;
- every retained progress scope must still be recognized by the resolver;
- completed scopes must still verify as exact target;
- incomplete scopes follow the pre-verification table above; and
- a newly discovered scope is allowed only when it verifies as source or exact target.

This prevents a resumed run from silently losing previously recorded work or trusting a changed
deployment inventory.

`ResolveScopes` returns the complete scope set, including scopes whose data is already at target. It
must not filter the result down to apparently pending scopes. Before operation completion, the engine
resolves the set again. A changed, missing, duplicate or malformed scope prevents completion. The
operation's store-wide post-verification remains the final proof that no required deployment or
batch was omitted.

#### Failure boundary

**Status:** Agreed.

The first operation or scope failure stops the global plan. The engine does not continue with an
independent-looking later operation because the declared global order may encode cross-store
dependencies.

After an orderly failure, the engine records the error for reporting and conditionally changes every
reserved store that has not already reached clean target state to `failed`. The owning store retains
the failing `CURRENT_OPERATION_ID`; stores that had not begun may keep it null. If a failure-state
write cannot be completed, its store remains visibly `in_progress` and requires the explicit crash
recovery path from Stage 2.

### Stage 4: Store finalization

**Status:** Agreed.

A logical store reaches its required migration level only after every operation assigned to it has
been reached in global plan order, every required scope and deployment has completed, completed
scopes still pass exact post-verification, final scope resolution finds no change, and no failed or
incomplete progress remains for the store.

The engine then performs one conditional store-finalization transaction. It verifies that the
reservation belongs to the active run, writes the required `COMPLETED_LEVEL` and
`LEVEL_COMPLETED_AT`, changes `STATUS` to `clean`, and clears `TARGET_LEVEL`, `ACTIVE_RUN_ID`,
`CURRENT_OPERATION_ID` and `STARTED_AT`.

`MIGRATION_PROGRESS` is not deleted by this transaction. It remains temporarily available for the
complete migration report.

Store finalization does not clear the last failure fields. A successfully resumed run retains them
until its report has been delivered so that report construction can describe the recovered failure.

Stores are finalized independently because there is no atomic commit across physical databases. A
later failure can therefore leave some stores clean at target and other stores failed before target.
This does not permit serving startup because the startup gate verifies every required configured
store. Resume leaves already-finalized stores unchanged and continues through the global plan for
the incomplete stores.

If application data reached the exact target but the finalization transaction failed, the store
remains `in_progress` or `failed`. Verified resume recognizes the target state and retries
finalization without repeating completed migration work.

### Stage 5: Report delivery and metadata cleanup

**Status:** Agreed.

**Failure record placement:** Agreed. The last failure details are stored only in
`MIGRATION_STATE`; progress rows remain limited to operation scope, execution status and checkpoint
data.

After the run reaches an orderly success or failure outcome, the engine reads the retained state and
progress into a structured report model before deleting anything. It then renders and delivers the
requested human-readable report. Terminal output is the default; a requested file destination is
validated during Stage 1.

Report delivery and migration completion are separate outcomes. For example, the stores may be
successfully migrated even when writing the requested report file fails. The command must state
both outcomes and return a non-zero status for the report-delivery failure without changing clean
stores back to failed or repeating migration operations.

Cleanup begins only after the complete multi-store run succeeds and its final report is delivered:

- the engine deletes all progress rows belonging to the successful run and clears retained last
  failure details;
- a failed or incomplete run retains all progress, including completed scopes and rows belonging to
  stores already clean at target, even after its failure report is delivered;
- cleanup uses the run identifier and expected state in conditional, idempotent writes; and
- cleanup failure is reported as incomplete housekeeping and can be retried without rerunning
  migration operations.

If report construction or delivery fails, no report-related cleanup occurs. A report-only command
can reconstruct and redeliver the report from the retained metadata without executing migration
operations. Normal startup continues to depend only on compatible clean `MIGRATION_STATE` rows, so
retained progress after a successfully completed migration does not block serving.

An explicitly abandoned run or a backup restoration removes retained progress only through a
separate verified recovery action. Ordinary failure-report delivery never abandons resumable work.

### Stage 6: Explicit recovery control

**Status:** Agreed.

Recovery never infers from age alone that an `in_progress` executor has stopped. The administrator
first ensures the former migration process is no longer running, then supplies the expected run
identifier. Stopping the former executor is a documented prerequisite, not a confirmation flag.
A conditional recovery transition changes only matching
`in_progress` rows to `failed`; it does not execute operations, change completed levels or delete
progress. Normal verified resume can then reserve the failed stores again.

An ordinary `failed` run does not need that recovery transition. After the cause is corrected, resume
validates the same target release, embedded plan, run identifier, store states, progress and
checkpoints before changing failed stores back to `in_progress`. Resume repeats the offline and
backup responsibility reminders without requiring acknowledgements. The administrator must ensure
that the intended recovery backup remains suitable. A backup of partially migrated data does not
replace the original recovery point.

Recovery and resume use the release artifact that owns the failed source-to-target transition. A
newer release cannot reinterpret or skip an unfinished older plan.

Abandoning progress is different from resuming. It is permitted only after a restore or corrective
action has established a complete recognized state across every affected store. The engine verifies
the restored levels and the operation-defined source or target invariants before conditionally
clearing the matching run metadata. It never provides an unchecked force-level or delete-progress
action.

A graceful cancellation rolls back the current transaction or batch and makes a best effort to
record the run as failed. An abrupt termination can leave `in_progress`; the next inspection reports
the interruption from durable state and directs the administrator to the explicit recovery action.

The eventual command names are a CLI design concern. The engine behaviors remain distinct:

- inspect and reconstruct reports without writes;
- acknowledge a stranded executor and transition it to failed;
- resume a verified failed run; and
- abandon a run only after verified restoration or correction.

## Decision 6: Redis migration scope

**Status:** Deferred to a separate delivery phase. The first migration delivery supports relational
stores backed by PostgreSQL and SQLite.

### Current Redis scope

The current configuration permits Redis as the provider for `runtime_transient`. That store contains
short-lived OAuth and authentication-flow state, and its Redis keys are namespaced by configured key
prefix, deployment identifier and runtime namespace. The current runtime-store API supports TTLs,
although it does not enforce a positive TTL for every value.

Redis used by the separate cache subsystem is derived cache data rather than a logical database
store. It does not receive a migration level or participate in the multi-store migration plan. Cache
format isolation and invalidation remain a cache-versioning concern.

The current product does not use Redis for `config`, `entity` or `runtime_persistent`. The initial
migration engine does not record levels, reserve, transform, verify or retain progress for
Redis-backed `runtime_transient`, and normal startup does not treat the absence of Redis migration
metadata as a relational compatibility failure.

No Redis migration strategy is selected by this design. Namespace isolation, bounded key
transformation and safe recreation remain candidates for the later phase. That phase must define
provider-appropriate compatibility evidence, interruption behavior and cleanup without using
database-wide destructive commands or assuming SQL transaction guarantees.

Until that phase is delivered, a release that changes the representation used by Redis-backed
`runtime_transient` must disclose the limitation and any administrator action in its release notes.
Redis used by the separate cache subsystem remains outside the migration feature.

## Decision 7: Administrator command surface

**Status:** One executable with strictly isolated modes agreed. Command behavior is under discussion;
exact names and flags follow after the behaviors are accepted.

### First proposal: One versioned executable with isolated modes

The distribution should use the target release's existing `thunderid` executable for serving,
bootstrap, compatibility inspection and migration-management modes. Migration separation is a
process-lifecycle boundary, not a requirement for a second compiled executable.

The executable contains one embedded current-release manifest and migration plan, so every mode
uses the same release definition. A separate migration binary would duplicate packaging, create a
new binary-version matching concern and provide no stronger isolation if it imports the same
providers and migration operations.

Command routing must occur after minimal argument and configuration-path parsing but before normal
runtime initialization. In particular, a migration or inspection invocation must not initialize the
HTTP server, cache manager, background workers or the normal service graph merely to discover that
the database is incompatible. Programmatic operations receive only their explicitly declared
migration-scoped components.

The lifecycle boundaries are:

```text
thunderid serving mode
    -> load configuration needed to locate stores
    -> run the read-only compatibility gate
    -> initialize normal services only when compatible

thunderid migration mode
    -> load configuration needed for migration
    -> initialize only migration-scoped providers and components
    -> inspect, apply or recover
    -> exit without serving

thunderid bootstrap mode
    -> require compatible initialized stores
    -> create built-in resources
    -> exit without serving
```

The distribution may provide `migrate.sh` and `migrate.ps1` as thin discoverability wrappers around
the executable. They contain no migration logic and do not select a different manifest or plan.
Direct binary invocation remains available for containers and automation.

`start.sh` and `start.ps1` never apply migration. They start the normal serving mode, which performs
only the mandatory read-only gate. The existing external management CLI may orchestrate downloads
and invoke the versioned executable, but it does not reimplement the migration engine.

### Proposed modes and commands

The executable has three top-level internal modes:

1. Serving mode, selected by the existing no-subcommand invocation.
2. Bootstrap mode, selected by the existing `bootstrap` subcommand.
3. Migration-management mode, selected by a new `migration` command group.

The proposed migration-management commands are:

| Command | Writes | Purpose |
|---|---:|---|
| `thunderid migration status` | No | Show embedded target information, store levels, active or failed run state, and pending report metadata. |
| `thunderid migration check` | No | Run the complete optional preflight and produce an aggregate readiness report. |
| `thunderid migration initialize` | Yes | Register fixed baseline 1.0.0 when tables are absent, or manifest target levels for missing rows in existing tables; preserve existing records. |
| `thunderid migration apply` | Yes | Apply a new adjacent-release transition from clean accepted source levels. |
| `thunderid migration resume` | Yes | Resume one recognized failed run using its retained progress. |
| `thunderid migration report` | No | Reconstruct and redeliver a retained report without executing migration. |
| `thunderid migration recover` | Conditional metadata only | Acknowledge a stranded executor after confirming it stopped, allowing the recognized run to become resumable. |
| `thunderid migration abandon` | Conditional metadata only | Clear an unfinished run only after verifying that restoration or correction established a complete recognized state. It never performs an unchecked force-level update. |

`status` reports recorded facts; `check` evaluates readiness. Apply, resume, recover and abandon
remain distinct actions. Initialization policy below supersedes earlier verified-adoption descriptions.

Initialization uses one public `migration initialize` command and a trust-based registration policy.
The current fresh-creation scripts create both migration metadata tables, initially without records.

| State inspected before writes | Action |
|---|---|
| Both metadata tables absent | Create both tables and insert clean baseline `1.0.0` rows for the namespace's configured stores. |
| Both supported tables exist, a store row absent | Insert a clean row at that store's embedded manifest `requiredLevel`. |
| A valid store row exists | Preserve its level and status and report current state; initialization never migrates or resets it. |
| Partial tables, unsupported structure or inconsistent records | Refuse blind stamping and report incomplete/damaged metadata. |

No fresh-schema or legacy baseline validator is required for initialization. These are trusted
starting declarations, not proof of schema/data correctness. Administrators must use the current
creation scripts for fresh databases and must not create/empty metadata manually to bypass migration.
All untracked pre-framework installations are assumed to be valid `1.0.0` migration inputs.

Inspect the route before creating tables. Create both tables and every required baseline row in one
transaction per SQLite file or PostgreSQL database/schema namespace, so a failed legacy initialization
cannot leave empty tables that a retry stamps at target. Across namespaces, retain successful commits,
report partial failure and retry remaining namespaces without rewriting existing rows.

Any migration-aware binary can initialize the fixed baseline. If it is behind the invoked binary's
requirements, startup remains blocked and guidance directs the administrator through each owning
adjacent-release artifact. Reporting a complete route requires release-path information in the binary
or release documentation; the target manifest alone does not describe every historical hop.
A no-change release may have required level `1.0.0`; clean baseline stamping can then allow startup
without schema/data verification. This is an accepted consequence of the trust-based policy.

SQLite and PostgreSQL follow the same path: fresh scripts create application and empty metadata
tables, setup initializes records, then bootstrap runs. Existing installations never rerun full
fresh-creation scripts; initialize creates only missing metadata and does not change application data.
This supersedes separate public `adopt-baseline` and proposals for baseline validators, fresh/legacy
flags or an unverified metadata state. Exact operation verification remains mandatory during migration.

Thin `migrate.sh` and `migrate.ps1` wrappers may expose the same action names. They invoke the
versioned binary and contain no decision or migration logic.

### Proposed script responsibilities

**Status:** Agreed. Detailed flags and output behavior remain separate discussion items.

The scripts provide convenient entry points, while the versioned executable remains the only
implementation of compatibility and migration behavior.

| Entry point | Migration responsibility |
|---|---|
| `setup.sh` and `setup.ps1` | For fresh setup, invoke `thunderid migration initialize` before invoking bootstrap. Initialization may safely return success when compatible current-level metadata already exists. Setup never invokes `apply`, `resume`, `recover` or `abandon`. |
| `start.sh` and `start.ps1` | Start serving or invoke the existing bootstrap mode. They never initialize, adopt or migrate a database. The invoked binary performs the mandatory read-only compatibility gate and terminates when a configured relational store is incompatible. |
| `migrate.sh` and `migrate.ps1` | Expose the migration-management actions by forwarding the selected action and supported configuration arguments to the same versioned binary. They contain no store classification, migration ordering, SQL or recovery logic. |
| `thunderid migration ...` | Provide the authoritative command behavior used by the scripts, containers and deployment automation. |
| External ThunderID management CLI | May coordinate download, shutdown, migration and restart for an explicitly requested upgrade, displaying the planned migration action and administrator responsibilities. It must not require separate offline or backup acknowledgements. |

The ordinary fresh-installation path is:

```text
setup
    -> migration initialize
    -> bootstrap
    -> exit

start
    -> read-only compatibility gate
    -> serve
```

The ordinary adjacent-version upgrade path is:

```text
stop every ThunderID instance using the affected stores
    -> migrate check                 optional read-only preview
    -> migrate apply                 includes mandatory validation itself
    -> start
    -> read-only compatibility gate
    -> serve
```

An administrator may therefore run only `migrate apply` after stopping all affected instances.
`check` is provided for planning and automation and is not a prerequisite whose earlier success is
trusted by `apply`. The apply command repeats all authoritative validation immediately before making
changes.

For a database created before migration metadata existed, `check` reports that baseline
establishment is required. The exceptional path is explicit:

```text
migrate initialize
    -> migrate apply
```

Setup invokes initialization for fresh databases; start remains read-only. Both metadata tables
absent select the agreed fixed `1.0.0` trust-based registration path, not historical detection.

Invoking a migration wrapper without an action prints usage and performs no write. Each wrapper
propagates the executable's exit code unchanged and must not turn a failed operation into a warning.
Shell and PowerShell wrappers expose equivalent behavior. Detailed output formats, abandon
authorization, exact command syntax and exit-code precedence remain to be finalized.

### Administrator responsibilities and exceptional authorization

**Status:** Agreed.

Ordinary apply and resume require no offline or backup acknowledgement flags and no interactive
prompts. The administrator deliberately initiates migration, for example with `migrate.sh apply`.
Documentation and preflight and execution output prominently state that all affected instances must
be stopped and a recoverable backup must exist. These are operational responsibilities, not facts
the engine can reliably prove. The absence of acknowledgement flags does not weaken source
validation, store reservations, authoritative revalidation, checkpoints or post-verification.

Exceptional actions remain distinct. Recovering a stranded executor requires the expected run
identifier after ensuring the former executor stopped; no executor-stopped acknowledgement flag is
required. Abandoning progress requires explicit authorization and verified complete recognized state. Authorization
must be usable non-interactively; missing required authorization causes refusal rather than a prompt.

### Configuration and declarative-resource release notices

**Status:** Agreed. Initial delivery does not require a custom detector for each configuration or
declarative-resource change.

Each release documents relevant administrator-managed changes in its migration guide: affected
settings or resource types, required actions, examples where useful, and when to apply them.
Lightweight developer-authored release notices packaged with the binary provide summaries and links
to documentation sections. Their exact representation remains to be decided. They contain no
per-change detection or transformation functions.

- `status` displays release notices without assessing applicability to the deployment.
- `check` repeats them and, where available, reuses existing target-release configuration and
  declarative-resource validators against supplied files without serving or modifying those files.
- Missing input or changes not covered by existing validation require manual review, not a custom
  migration-specific detector or an assertion of compatibility.
- Validation results identify the files inspected. Passing validation does not prove every release
  action is complete or that files held elsewhere in deployment tooling are compatible.
- Database readiness and administrator review are separate results. Manual-review notices alone are
  not database preflight blockers. Configuration errors needed to locate stores can still prevent
  the command from running.

ThunderID does not rewrite administrator-owned files or record their manual updates as completed
database migration operations. Persisted application content owned by ThunderID remains
engine-managed, even if equivalent declarative definitions exist externally. Database migration
completion does not mean that administrator-managed release actions have been completed.

### Command output model and tentative examples

**Status:** Human-readable default and optional JSON output accepted for illustration. Exact JSON
schema, option names, report destinations and exit-code values remain to be agreed.

The status command includes the release-specific migration documentation URL from the embedded
manifest's `migrationDocumentation` field. It points to the documentation for the transition owned
by the invoked binary, not a generic migration overview or the latest release. ThunderID publishes
that documentation for each migration-bearing release. Status exposes the URL in both human-readable
and JSON output without requiring a network request to produce the result.

All migration commands use a consistent result model. Human-readable output is the default. An
optional JSON format provides machine-readable results without changing command behavior. Standard
output contains the selected format; diagnostic logs go to standard error. JSON output must not be
mixed with banners or human-readable logs. Output excludes credentials, sensitive connection strings
and copied application records. Exit codes indicate the command outcome without requiring callers
to parse human-readable text.

The final human-readable migration report remains subject to Decision 5's delivery-before-cleanup
rule. It may be delivered to the terminal or a requested file; a separate file artifact is not
mandatory. Selecting JSON does not waive human-readable report delivery. In JSON mode, the
human-readable report goes to the requested report file, or to standard error when no file is
requested. Standard output remains exclusively JSON.

The following examples are tentative, not implemented commands or a stable text-parsing contract.
They use a hypothetical adjacent transition from `1.0.0` to `1.1.0`: `config` is unchanged, while
`entity` and `runtime_persistent` change. Real output includes every configured in-scope relational
store. `<run-id>`, operation identifiers, failure codes and report paths are illustrative placeholders.
Exceptional-action authorization options are intentionally not named yet. Configuration and resource
notices below are hypothetical release changes, not claims about current ThunderID settings.

#### Status: an upgrade is pending

Command: `./migrate.sh status`

```text
ThunderID binary: 1.1.0
Accepted source release: 1.0.0
Migration documentation: https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0

Store                 Recorded level   Required level   State
config                1.0.0            1.0.0            clean
entity                1.0.0            1.1.0            clean
runtime_persistent    1.0.0            1.1.0            clean

Compatibility: not compatible with this binary
Migration: required
Administrator-managed release changes:
  Configuration: connection setting renamed; review before startup.
  Details: https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#configuration
  Declarative resources: flow definition requirements changed; review before import.
  Details: https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#declarative-resources
Applicability: not assessed. ThunderID will not modify these files.

Next: stop all affected instances, ensure a recoverable backup, then run
  ./migrate.sh apply
Complete manual release actions at the times specified in the guide.

No readiness checks executed. No changes made.
```

#### Status: tentative JSON representation

Command: `./migrate.sh status --output json`, with the option and fields still provisional

```json
{
  "command": "status",
  "binaryVersion": "1.1.0",
  "acceptedSourceVersion": "1.0.0",
  "migrationDocumentation": "https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0",
  "compatibility": "incompatible",
  "migrationRequired": true,
  "readinessEvaluated": false,
  "administratorReview": "required",
  "releaseNotices": [
    {
      "category": "configuration",
      "summary": "Connection setting renamed",
      "action": "Review deployment configuration before startup",
      "applicability": "not_assessed",
      "documentation": "https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#configuration"
    },
    {
      "category": "declarative_resources",
      "summary": "Flow definition requirements changed",
      "action": "Review affected definitions before import",
      "applicability": "not_assessed",
      "documentation": "https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#declarative-resources"
    }
  ],
  "stores": [
    {"id": "config", "completedLevel": "1.0.0", "requiredLevel": "1.0.0", "status": "clean"},
    {"id": "entity", "completedLevel": "1.0.0", "requiredLevel": "1.1.0", "status": "clean"},
    {"id": "runtime_persistent", "completedLevel": "1.0.0", "requiredLevel": "1.1.0", "status": "clean"}
  ]
}
```

The exact JSON contract will later define command outcomes independently from compatibility and
report-delivery outcomes. This status example describes an incompatible database without implying
that the status inspection itself failed.

#### Check: ready to attempt, with deferred verification

Command: `./migrate.sh check`

```text
ThunderID migration check: 1.0.0 to 1.1.0

Administrator responsibilities:
  Stop every ThunderID instance using the affected databases before applying.
  Ensure a recoverable backup exists.

config: already compatible
entity: source level accepted; required permissions verified
runtime_persistent: source level accepted; required permissions verified

Readiness:
  entity.add_attribute: ready
  entity.populate_attribute: deferred
    Exact verification requires the preceding column-addition operation.
  runtime_persistent.transform_records: ready

Database readiness: no preflight blockers found; deferred checks remain unverified

Administrator-managed release changes:
  Configuration: connection setting renamed; review before startup.
  Details: https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#configuration
  Declarative resources: flow definition requirements changed; review before import.
  Details: https://thunderid.dev/docs/migrations/1.0.0-to-1.1.0#declarative-resources

Existing target-release validators:
  Configuration: supplied deployment.yaml passed validation.
  Declarative resources: not assessed; no files supplied.
Administrator review: required. Release-change applicability was not detected.
Validation does not prove that all manual actions are complete.
ThunderID will not modify your files.

Apply will repeat validation and verify each operation at execution time.
No changes made. This result does not guarantee that apply will succeed.
```

#### Check: blocked by missing metadata

Command: `./migrate.sh check`

```text
Result: blocked
Store: entity
Reason: migration metadata is missing; its level is unestablished

For a fresh database, use setup or migration initialize.
For an untracked legacy installation, use migration initialize to register the fixed 1.0.0 baseline.
Adoption must verify the complete recognized baseline before recording a level.
For an installation already using migration metadata, investigate missing or damaged state.
No changes made. Apply was not started.
```

#### Initialize: fresh or existing databases

Command: `./migrate.sh initialize`

```text
Fresh tables present, rows absent:
  entity: clean target level 1.0.3 registered from embedded manifest
  No application data transformed. Next: bootstrap for fresh setup.

Legacy tables absent:
  migration tables and clean baseline 1.0.0 records created atomically
  No schema/data validation or application transformation performed.
  Migration required. Follow the documented adjacent-release path.

Existing records:
  entity: level 1.0.2, status clean
  Record preserved. Target requires 1.0.3; migration still required.
```

Versions are illustrative. Baseline registration does not itself establish target compatibility.

#### Apply: successful migration

Command: `./migrate.sh apply`

```text
ThunderID migration: 1.0.0 to 1.1.0
Run: <run-id>

Administrator responsibilities:
  Stop every ThunderID instance using the affected databases.
  Ensure a recoverable backup exists.

config: already compatible; unchanged
entity: migrated to 1.1.0
runtime_persistent: migrated to 1.1.0

Migration: completed
Compatibility: all configured relational stores match this binary
Report: delivered to terminal
Progress cleanup: completed
Next: complete any before-startup manual release actions, then ./start.sh
```

#### Apply: failed migration with resumable progress

Command: `./migrate.sh apply`

```text
ThunderID migration: 1.0.0 to 1.1.0
Run: <run-id>

Administrator responsibilities:
  Stop every ThunderID instance using the affected databases.
  Ensure a recoverable backup exists.

Migration: failed
Store: runtime_persistent
Operation: runtime_persistent.transform_records
Scope: deployment / example-deployment
Failure: DATA_VALIDATION_FAILED
Summary: an existing record does not satisfy the expected source invariant

entity: clean at target level 1.1.0
runtime_persistent: failed; completed level remains 1.0.0
Report: delivered to terminal
Progress: retained, including completed operations and committed checkpoints
Compatibility: startup remains blocked

Next: correct the reported issue, keep affected instances stopped, then run
  ./migrate.sh resume
Use this same release artifact. Do not initialize these databases again.
```

#### Resume: successful continuation

Command: `./migrate.sh resume`

```text
ThunderID migration resume: 1.0.0 to 1.1.0
Run: <run-id>

Administrator responsibilities:
  Keep every affected ThunderID instance stopped.
  Ensure the intended recovery backup remains suitable.

entity: already at target; completed operations not repeated
runtime_persistent: checkpoint verified; continuing retained work

Migration: completed
Compatibility: all configured relational stores match this binary
Report: delivered to terminal
Progress cleanup: completed
Next: complete any before-startup manual release actions, then ./start.sh
```

#### Resume: blocked by a potentially active executor

Command: `./migrate.sh resume`

```text
Result: blocked
Run: <run-id>
State: in_progress
The previous migration executor may still be running.
No takeover attempted. No changes made.

Next: verify that the former executor has stopped, then explicitly authorize
recovery of this exact run with migration recover. Resume only after recovery.
```

#### Recover: explicitly selected stranded-run recovery

Tentative action: `migration recover --run-id <run-id>`, after ensuring the former executor stopped

```text
Run: <run-id>
Result: recovered for resume
Matching stranded in_progress state changed to failed.
Completed levels and retained progress were not changed.
No migration operations executed.
Next: ./migrate.sh resume
```

Missing or mismatched run identity causes refusal. The command reminds the administrator to stop the
former executor, but requires no confirmation flag or interactive acknowledgement.

#### Report: redelivery after report-file failure

If apply completed the migration but failed to deliver a requested file report, show:

```text
Migration: completed
Compatibility: all configured relational stores match this binary
Report: delivery failed
Destination: reports/migration-<run-id>.txt
Summary: destination is not writable
Progress: retained; cleanup not started
Command outcome: unsuccessful because report delivery failed

The database compatibility gate is satisfied; manual release actions still require review.
Do not rerun migration operations to repair report delivery.
Next: correct the report destination and use migration report to redeliver.
```

Action: `migration report`, selecting the retained run and a valid destination

```text
Run: <run-id>
Migration outcome: completed
Report: redelivered to reports/migration-<run-id>.txt
No migration operations executed. No database metadata changed.
Retained progress is eligible for verified cleanup after successful report delivery.
```

#### Abandon: refusal before verified restoration

Action: `migration abandon`, with the expected run identifier and explicit authorization supplied

```text
Result: refused
Run: <run-id>
Reason: affected stores do not form a complete recognized restored state
Explicit authorization does not bypass restoration verification.
Progress and completed levels remain unchanged.

Next: complete the documented restoration or correction procedure and retry.
Alternatively, correct the migration failure and resume the retained run.
```

### Configuration and resource input selection

**Status:** Agreed. Exact wrapper option syntax remains to be finalized.

Migration commands use the same ThunderID home, deployment configuration, defaults and environment
resolution as serving. Migration and subsequent startup must resolve the same physical databases.
There are no migration-specific connection-string overrides or administrator-supplied manifests;
the invoked binary's embedded manifest remains authoritative.

Commands assess every configured in-scope relational store. The initial delivery does not provide
an arbitrary store-subset option that could omit a required store from validation or the coordinated
run. This does not mean every store is written: unchanged or already compatible stores retain their
levels, and only stores eligible for the requested action are modified.

`check` may accept explicitly supplied declarative-resource files for validation using existing
target-release validators. It does not crawl repositories or deployment systems to discover files.
Results identify inspected inputs, unavailable validation and omitted resources. Missing input is
unassessed, not implicitly compatible. These files are neither imported nor rewritten by check.

The minimal configuration-loading path used by migration must not initialize ordinary services or
apply declarative resources merely because a resource input was supplied. Wrapper environment-file
support follows the existing startup conventions and does not introduce a second configuration
precedence model. Exact argument syntax and supported resource-input multiplicity remain CLI design
details, not reasons to duplicate configuration or resource parsing logic.

### Output options and report routing

**Status:** Approach agreed for now. Exact parser syntax and wrapper forwarding remain tentative
until implementation.

| Option | Purpose |
|---|---|
| Existing `-serverHome` | Select the deployment's ThunderID home using existing resolution rules. |
| Existing `-resources` | Supply declarative resources to check for read-only validation, not import. |
| `--output text` or `--output json` | Select command result formatting. Text is the default. |
| `--report-file <path>` | Optionally select a file destination for the human-readable migration report. |

Wrappers may support the existing `--env` convention to load environment values before invoking the
binary. This does not introduce database connection or manifest overrides. Exceptional recovery
authorization options remain separate from ordinary output and input options.

In text mode, the human-readable report is delivered to the terminal by default, or to the requested
report file with a concise command-result summary on standard output. In JSON mode, standard output
contains only the machine-readable result. The human-readable report is delivered to the requested
file, or to standard error when no report file is requested. Diagnostic logs also use standard error,
so it is not a machine-readable report stream. Commands without a final migration report, such as
status, simply emit their selected result format.

The engine validates a requested report destination during initial preflight where applicable and
handles later delivery failures separately from database migration completion. Successful JSON
serialization alone does not satisfy human-readable report delivery. Progress cleanup remains
conditional on complete multi-store migration success and successful human-readable report delivery.
The result identifies the report destination, delivery outcome and cleanup outcome. Reports and
diagnostics remain subject to the same sensitive-data restrictions in either mode.

### Tentative exit-code mapping

**Status:** Tentative mapping recorded for implementation planning, not a finalized public contract.

| Code | Meaning |
|---|---|
| `0` | The requested command completed successfully. |
| `1` | The command could not complete because of an operational error. |
| `2` | Arguments or configuration are invalid. |
| `3` | Validation blocked the requested action before migration execution. |
| `4` | Migration execution failed or the requested run remains incomplete. |
| `5` | Migration completed, but required report delivery or progress cleanup failed. |

The exit code describes the requested command, not database compatibility alone:

- `status` returns `0` when inspection succeeds, even if stores are incompatible. An unreadable
  store prevents complete inspection and produces an unsuccessful command outcome.
- `check` returns `3` for detected blockers. Deferred checks or administrator manual-review notices
  alone do not cause failure and are reported explicitly.
- `apply` and `resume` return `0` for complete migrations or verified no-ops when required report
  delivery and cleanup also succeed. A valid no-op does not repeat migration operations.
- `recover` returns `0` when the requested authorized metadata transition succeeds. That does not
  mean the migration itself is complete.
- Wrappers propagate the executable's exit code unchanged.

JSON carries detailed command, migration, compatibility and reporting outcomes plus diagnostic
failure codes and next-action guidance. Numeric exit codes remain coarse. In particular, code `5`
must not cause automation to execute completed migration operations again. Resume, recovery and
report-only behavior remain governed by the authoritative database state, not the exit code alone.

When several outcomes coexist, unfinished or failed migration takes precedence over a simultaneous
report-delivery failure: code `5` is reserved for completed migration with unsuccessful reporting or
cleanup. The implementation must finalize error classification and command-specific precedence
before these codes become a public contract.

## Remaining design work

The fundamentals are settled. Continue with these unresolved design choices only:

1. **Engine-driven batch contract.** Finalize next-batch selection, transaction-bound transformation,
   checkpoint validation, completion signaling and the concrete access types. Reconcile all earlier
   operation interfaces with this ownership model.
2. **Partial successful-run cleanup.** Define how a saved complete report is supplied and correlated
   when progress cleanup already succeeded in only some SQL namespaces. Do not add receipt metadata,
   reconstruct a complete report from remaining rows or rerun operations.
3. **Abandon safeguards.** Finalize exact run selection, recognized restored/corrected states,
   conditional metadata cleanup and retry after partial multi-namespace cleanup.
4. **Historical path guidance.** Define how a later artifact or published documentation maps a newly
   registered fixed `1.0.0` baseline through every required adjacent-release artifact. Resolve the
   partial multi-store failed-hop wording without implying a rollback to one product version.
5. **Public command contracts.** Finalize argument syntax, JSON schemas, manual-action notice shape,
   report-file input/output, exit codes and error precedence. Behavioral intent and examples are
   already agreed; this step freezes the public automation contract.
6. **Deployment orchestration choices.** Decide what ThunderID itself provides for distribution,
   containers, Kubernetes/OpenChoreo and the external management CLI beyond documented procedures.

The following are parked implementation planning, not open fundamentals:

- map migration access onto current SQL providers and transaction facilities, and add early routing
  before ordinary service initialization;
- wire metadata creation scripts, manifest/plan build validation, current-release binding and wrappers;
- define and execute the PostgreSQL/SQLite test matrix for fresh versus upgraded state, interruptions,
  resume, multi-deployment work and shared/separate/hybrid physical layouts; and
- consolidate the final approved design into the ThunderID feature-standard structure after the
  unresolved choices above are settled.

Redis remains a separate later phase. The overview and summary are supporting artifacts, not
additional sources of authority over this decision log and the requirements.

## Reporting refinement: explicit report and cleanup

**Status:** Agreed behavior. Syntax is tentative; partial-cleanup evidence and retry remain open.

```sh
thunderid migration report --run-id <run-id>
thunderid migration report --run-id <run-id> --cleanup
```

- Ordinary `report` remains read-only: reconstruct and deliver retained run evidence without deleting
  progress or changing metadata.
- Explicit `report --cleanup` verifies that the correlated run fully succeeded and every participating
  store is clean at its required target, then generates and delivers the complete report before cleanup.
- Only after successful report delivery may it conditionally delete the selected run's progress and
  clear eligible retained failure details. Completed levels never change; operations never rerun.
- Failed, incomplete or unverifiable runs are not eligible for cleanup. Missing stores or evidence
  cannot be interpreted as proof of success.
- Cleanup across separate databases is not atomic. If some progress was already deleted, a retry
  must not reconstruct and label the remaining rows as a complete run report. The evidence and retry
  mechanism for this case must be settled before implementation.

This introduces an explicit mutating option to the otherwise read-only report command; earlier
read-only descriptions apply to ordinary `report` without `--cleanup`.

Reports are the administrator's external reference for successful, failed, incomplete and
housekeeping outcomes; no report-receipt metadata is added. Generate and deliver the complete
successful report before cleanup. A saved report can support exceptional partial-cleanup retry but
never replaces exact run correlation or verification of current database state. Failed/incomplete
runs retain all progress even after reporting. Abrupt interruption may prevent immediate delivery.

## Recovery refinement: recover safeguards

**Status:** Agreed behavior, including multi-store conflict/retry handling below. Argument syntax remains tentative.

- Require the administrator to explicitly select the stranded run by its exact run ID; never select
  the newest run or infer ownership from timestamps.
- Document that the administrator must ensure the previous executor has stopped; no
  `--confirm-executor-stopped` flag or interactive acknowledgement is required. The engine cannot reliably
  prove this across hosts; stopping the executor and other writers is an administrator responsibility.
- Before updating metadata, verify the selected run, its owning target release and the correlated
  affected-store states. Refuse mismatched or unrecognized evidence; preserve completed stores.
- Use conditional updates against the inspected state. If a relevant state changes before the update,
  refuse that transition rather than overwriting the concurrent change. Separate databases cannot
  provide a single atomic recovery transaction; use the retryable partial-transition behavior below.
- Change only matching stranded `in_progress` rows to `failed`, retaining progress and checkpoints.
  Recover does not execute operations, advance completed levels or delete progress.
- Continue with verified resume using the artifact that owns the interrupted transition. An already
  failed run ordinarily goes directly to resume after its cause is corrected.

Conditional metadata updates do not fence off a previous executor that is still running. Ensuring
that executor stopped remains an administrator safety responsibility, reinforced by documentation
and command-output reminders. Tentative syntax: `thunderid migration recover --run-id <run-id>`.

### Partial recovery across SQL namespaces

**Status:** Agreed.

- Inspect and validate every participating store before making recovery updates. If any required
  inspection fails, make no recovery changes.
- Apply conditional transitions to matching stranded rows. If an update fails after earlier stores
  were recovered, stop and report the partial result; do not undo successful transitions.
- Retrying recover with the same exact run ID treats matching `failed` rows as already recovered,
  transitions remaining matching `in_progress` rows, and leaves verified clean target stores unchanged.
  Completed-store correlation must be established from retained run evidence, not assumed from a
  clean level alone.
- Refuse mismatched run identities or unexpected state changes. Preserve progress and checkpoints;
  no additional recovery-progress table is introduced.
- Resume is blocked until no participating store remains stranded `in_progress` and the correlated
  run passes validation. Unavailable or unrecognized stores cannot be silently omitted.

For example, config may transition to `failed` before entity becomes unavailable, while runtime is
already clean at target. Recovery reports failure with the partial result. Once entity is available,
the same-run retry preserves config and runtime and recovers entity. The administrator must keep the
former executor and other writers stopped throughout recovery and resume.

## Repository integration note: current bootstrap limitation

The current `main` function parses global flags and loads configuration, but it does not dispatch
the `bootstrap` subcommand immediately. Before checking `isBootstrapInvocation`, it initializes the
cache, security permissions, an HTTP multiplexer and the complete application service graph through
`registerServices`.

Although bootstrap does not open the HTTP listener, it therefore initializes database-backed domain
services, Redis runtime storage, cryptographic services, observability, MCP handling, HTTP handlers
and other normal runtime components before the process knows it is only bootstrapping. Many service
initializers fail fatally rather than returning a structured command result.

This becomes unsafe for migration because the target release's ordinary services may query or assume
the target schema before compatibility has been established. The process could fail with an
unrelated service error before it reaches the compatibility gate or migration engine. Bootstrap also
writes built-in resources through the importer, so running it against an incompatible store could
create partial or misleading content changes.

The new command router must therefore run after minimal argument, home-directory, configuration and
logging setup, but before cache initialization or `registerServices`. Migration-management mode then
initializes only migration dependencies. Serving and bootstrap modes first pass the compatibility
gate; only then may bootstrap construct the selected service dependencies required to import built-in
resources. Fresh setup calls `migration initialize` before bootstrap.

## Initialization refinement: agreed trust-based registration

**Status:** Agreed provisionally. The initialization policy in Decision 7 supersedes previous
verified fresh initialization and legacy-adoption descriptions. No baseline validators or separate
adopt-baseline command are required. Release-path mapping from the fixed baseline, including the
pre-framework-to-first-framework entry, must be explicitly supplied by release tooling/documentation.

## Batch execution refinement: engine-driven orchestration

**Status:** Ownership clarified; callback types and concrete interfaces remain tentative.

The engine owns the batch loop, transactions, checkpoint persistence and resume orchestration.
Operations supply read-only next-batch selection, batch transformation, checkpoint validation and
scope verification. Selection and transformation use engine-bound access; data and the next checkpoint
commit together. This supersedes the earlier operation-owned loop with a developer-facing
BatchCommitter. Any internal committer remains an engine implementation detail, not an operation API.
The simplified example in the overview proposes SelectNext and ExecuteBatch callbacks; final contract
updates and validation rules remain to be designed before implementation.
