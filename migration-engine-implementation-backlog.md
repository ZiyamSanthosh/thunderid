# Migration engine implementation backlog

## Purpose

This document tracks migration capabilities intentionally omitted from the current implementation.
It is an implementation backlog, not a requirement to build every previously discussed idea.

The migration engine should remain small. A deferred field, table, constraint or abstraction is
added only when an implemented behavior requires it.

## Current foundation

The current implementation contains:

- an embedded and validated release manifest;
- canonical logical database identifiers;
- empty PostgreSQL `MIGRATION_STATE`, `MIGRATION_PROGRESS`, `MIGRATION_DEPLOYMENT_STATE` and
  `MIGRATION_RUN` tables in every fresh database creation script;
- Go models and validation for migration state and operation progress;
- a metadata store for validated state and operation-progress reads and writes;
- a transactional PostgreSQL initialization service for the fixed pre-framework baseline and fresh
  manifest targets;
- a read-only compatibility gate that evaluates every manifest store and aggregates failures;
- a phase-aware serving compatibility gate that keeps source binaries eligible through expansion,
  migration and the `contract_ready` boundary, admits target binaries from `contract_ready`, and
  rejects source binaries once contract starts;
- a validated migration plan registry with ordered operations and per-operation store, interruption
  and execution-target declarations;
- release-plan-owned `Expand`, `Migrate` and `Contract` operation groups, with canonical phase order
  validation and phase-partitioned plan access;
- durable store-stage vocabulary and metadata for `complete`, active phase execution,
  `expanded` and `contract_ready`, with stage validation across initialization and recovery;
- phase-aware state-controller transitions that reserve an explicit phase, validate phase-local
  progress, restore the preceding boundary when unused reservations are cancelled, skip undeclared
  phases and advance the completed store level only after its last declared phase;
- an optional provider-neutral operation readiness contract with immutable plan context and
  standardized ready, deferred and blocked results;
- an engine-owned readiness evaluator that treats operations without additional readiness checks
  as ready, provides declaration-scoped read access to operations that opt in, validates their
  results, propagates blocked dependencies and aggregates findings in plan order;
- validated execution-target and verification contracts plus an immutable atomic `SQLOperation` that requires
  an explicit ordered SQL implementation for every registered database provider;
- engine-owned execution access that permits reads only from declared stores and writes only to an
  operation's owning store;
- an engine-owned operation execution envelope, with `SQLOperation` selecting its explicit provider
  implementation and executing its statements in declared order;
- an atomic complete-store executor that reuses ThunderID's transactioner to verify reservation
  ownership, pre-verify, execute or skip, post-verify and complete operation progress in one
  transaction;
- an online schema executor that durably records in-progress work, executes PostgreSQL autocommit
  DDL outside a transaction, and uses source and target verification to complete or safely resume
  operations whose DDL and progress updates cannot be atomic;
- a resumable complete-store executor that commits bounded data batches independently, persists
  each checkpoint in the same transaction as its data changes, rejects checkpoints that do not
  advance and continues an interrupted operation from its last committed checkpoint;
- transactional store finalization that validates completed operation progress and conditionally
  advances only the run's still-reserved store to its target level;
- conditional run-state recovery primitives that cancel unused reservations before execution and
  mark still-reserved stores failed after an orderly execution error;
- transactional namespace-level store reservation using a conditional metadata update to prevent
  concurrent migration ownership;
- a provider-neutral migration execution-lock contract, with PostgreSQL session advisory locking,
  that serializes migration runners per physical database namespace without locking application
  tables or preventing serving traffic;
- a phase-aware migration coordinator that generates one run identifier, reserves database
  namespaces in deterministic manifest order, repeats operation readiness after reservation,
  executes the explicitly selected phase in global plan order, completes each store after its last
  phase-local operation and distinguishes pre-execution cancellation from durable execution
  failure;
- recovery for failed and abandoned atomic runs through `migration resume`, including run ownership
  and phase inference, route validation, reuse of phase-local completed progress, recognition of
  stores that already completed the active phase, readiness checks for only pending operations and
  conditional failed-to-in-progress transitions;
- stable command outcome logs that retain partial reports on failure and return success only when
  the requested compatibility or readiness condition is satisfied;
- a store-level release-route resolver that reports the next migration-owning ThunderID release and
  supports multi-hop paths through an ordered manifest catalog;
- an isolated PostgreSQL `migration initialize` command that runs before normal service setup and
  groups logical stores sharing the same configured database namespace;
- a read-only PostgreSQL `migration status` command that reports compatibility for every configured
  logical store without creating or modifying migration metadata;
- a read-only PostgreSQL `migration check` command that evaluates an explicitly selected phase,
  classifies stores as already at the phase boundary, migration-required or blocked and aggregates
  operation-specific readiness findings;
- a PostgreSQL `migration apply` command that runs an explicitly selected phase as a dedicated
  execution of the ThunderID binary without starting serving components;
- a safe automatic `migration apply` workflow that holds one execution lock while running `expand`
  and `migrate` in order, then stops before contract; explicit phase arguments retain manual CP/DP
  control;
- an explicit `migration apply contract` path that cannot be reached by the default workflow or the
  generic phase-execution API and retains engine-owned contract preflight checks;
- a read-only PostgreSQL startup compatibility gate that runs before caches, bootstrap, background
  workers and request listeners, checks every store backed by a registered provider and explicitly
  reports unsupported development-provider stores as skipped;
- validated `full` and `restricted` operating modes, with `full` as the configured default and an
  administrator-controlled restart required to change the mode on serving instances;
- restricted-mode HTTP enforcement that keeps authentication, authorization, token, health,
  discovery and required frontend-support endpoints available while rejecting management surfaces;
- a generic feature gate that fences registration, user onboarding, recovery, administration, JIT
  user provisioning, organization-unit creation, account linking, legacy-subject conversion,
  profile updates, credential enrollment or replacement and passkey authentication before mutation,
  including nested flow-executor paths. Restricted federated authentication retains a read-only
  lookup of the legacy `sub` representation while suppressing its conversion to `linkedIds`.
  Passkey authentication is fenced because successful verification requires a security-sensitive
  credential sign-counter update;
- unit coverage for every feature gate and real-server integration coverage proving that restricted
  management, registration, recovery, organization-unit creation, profile updates, credential
  updates and passkey authentication are rejected while password authentication and token issuance
  remain available. The profile-update test also verifies after restoring full mode that the fenced
  attribute was not persisted;
- deployment-selective migrate execution through repeatable `--deployment-id` arguments, with
  deployment-level completion state, independently resumable operation targets and store-wide
  completion of any remaining targets;
- explicit administrator-controlled rollback for failed operations, completed work in a failed
  phase and complete pre-contract releases, including reverse operation ordering,
  deployment-specific targets, resumable rollback checkpoints and rejection after an uncompensated
  contract operation;
- a first 2.0.0 atomic PostgreSQL operation that removes the legacy `REVOKED_TOKEN` reason check
  constraint from `runtime_persistent`, with exact catalog-based pre-verification and
  post-verification plus a bounded DDL lock wait;
- a second 2.0.0 atomic PostgreSQL operation that creates the `AUTHORIZATION_PDP_CONNECTION` table
  and its unique index in `configdb`, with exact catalog-based pre-verification and
  post-verification;
- a representative 2.0.0 multi-phase PostgreSQL migration that adds and backfills entity-type
  handles in bounded resumable batches, enforces uniqueness, finalizes the target representation
  during contract and provides an explicit rollback operation for the backfill;
- operation-specific readiness checks for every forward 2.0.0 operation, including exact source and
  target schema recognition, relation-name conflict detection, handle validation, dependency-aware
  deferred readiness and null-handle contract validation;
- online deployment-scoped primary-key operations for the affected configuration and entity
  tables, including concurrent unique-index preparation, validated replacement foreign keys,
  bounded transactional cutover and recovery from interrupted online preparation;
- provider-neutral typed query-row helpers for operation readiness and verification queries;
- a Docker Compose PostgreSQL integration test that builds the target ThunderID binary, obtains the
  configured source release schema from its published image, initializes all four logical
  databases, executes the complete migration twice and verifies schema and metadata outcomes in
  both separate-database and single-shared-database layouts, including interruption after a
  committed resumable batch and continuation from its persisted checkpoint;
- provider-neutral namespace and metadata-store selection, with PostgreSQL currently registered
  as the only implementation;
- minimal structural database constraints; and
- a test that keeps the metadata schema identical across the four logical databases.

The metadata tables currently store only the information required for store compatibility,
migration stage, migration-run correlation, failure reporting, ordered operation progress and
optional resume data.

## Migration engine foundation status

The database-backed Phase 1 migration foundation is functionally complete for PostgreSQL. Integrated
mixed-version qualification, serving-impact qualification, configurable backfill resource controls
and operator documentation remain before ThunderID can claim the base database-changing ZDM path as
production-qualified. Implementation increments do not define a separate downtime-oriented
architecture.

| Optional enhancement | Purpose |
|---|---|
| Machine-readable result document | Add an optional JSON result mode if external migration automation needs a contract beyond structured command logs and process success or failure. |
| Report-file routing | Add report-file delivery only when a durable standalone migration report becomes a product requirement. |

## Shared physical database support

The metadata model, command namespace grouping and coordinator support multiple logical-store rows
in one physical database. PostgreSQL integration coverage exercises both four separate databases
and one database shared by all four logical stores. The implementation:

1. resolves a PostgreSQL namespace from its endpoint, database and configured user without exposing
   credentials;
2. groups logical stores before initialization, inspection or execution;
3. uses one client, transactioner and execution lock per physical namespace;
4. processes namespace groups deterministically;
5. makes repeated metadata-table creation safe for fresh shared databases; and
6. verifies initialization, apply, idempotency, failure recovery, abandoned-run recovery and
   concurrent-runner rejection in both supported layouts.

Partially shared layouts use the same grouping behavior and remain covered by command-level tests.

## Execution hosting and orchestration

Migration implementations remain part of the target ThunderID binary. The migration engine must
not depend on a Control Plane upgrade coordinator. Product-native Control Plane coordination is a
future enhancement above the engine.

The default operational model uses a dedicated migration process or job from the target ThunderID
artifact. Source instances continue serving while Expand establishes a shared compatibility
boundary. The generic safe apply command now stops after Expand. Target instances can then start in
restricted mode, every source instance is drained, and Migrate runs while the target continues
serving. Contract remains explicit until the rollback window ends. The migration job does not open
an HTTP listener and exits after its requested work. Only one coordinator may own a physical
database namespace at a time; this restriction does not prevent target application traffic.

The migration engine admits the target at the Expanded boundary but does not implement feature-level
fallback reads. Each product change that migrates an in-place representation must provide and test
its own target dual-read behavior before the release can use this topology safely.

## Restricted operating mode

Restricted mode is a serving-product control, not migration-engine state. Administrators configure
`operating_mode: restricted` on source instances before Expand when source writes must be fenced,
and on target instances throughout Migrate when target writes must be fenced. They restore `full`
mode after the migration reaches the approved boundary. Automated phase-to-mode wiring is not
required.

The current feature set is a stable baseline. Developers adding new write-capable flows, executors
or non-HTTP entry points are responsible for adding and enforcing the appropriate feature gate.
Optional future work is limited to UI treatment for unavailable functions and deployment automation
if either becomes a concrete product requirement.

## Deferred metadata additions

The following ideas were removed from the initial schema. They are not automatically required.

| Deferred item | Add only when |
|---|---|
| Current operation identifier in `MIGRATION_STATE` | State inspection cannot identify the active operation reliably from `MIGRATION_PROGRESS`. |
| Structured failure code and failure timestamp | Commands need machine-readable remediation or reliable failure-time reporting. |
| Run start, operation start and completion timestamps | Operational reporting or stale-run recovery has a defined use for them. |
| Checkpoint representation version | A checkpoint format must remain readable across different migration-engine versions. |
| Run and status indexes | Measured query plans show that metadata lookups require them. Metadata tables are expected to remain small. |
| Compound database `CHECK` constraints | A rule is stable across engine versions and database enforcement materially improves correctness. |
| Additional migration tables | A new durable concept cannot be represented clearly in the four existing metadata tables. |

Primary keys, foreign keys, uniqueness and required-column constraints remain in the database.
Behavioral rules and state transitions remain in Go unless there is a demonstrated reason to enforce
them in every supported database.

## In-place ZDM backlog

### Required before the base PostgreSQL ZDM claim

The representative `expand`, `migrate` and `contract` migration, deployment-selective execution and
rollback lifecycle are implemented. The next increment is integrated in-place qualification proving
that:

1. a source instance in restricted mode continues authentication, authorization and token traffic
   while Expand runs;
2. target startup succeeds at Expanded only when the affected product features can read legacy data;
3. every source instance is drained before Migrate starts;
4. the target continues serving while resumable Migrate operations execute and resume;
5. compare-and-set updates reject concurrent changes instead of overwriting them; and
6. contract remains explicit until the rollback window ends.

The qualification must also cover serving impact and operational recovery:

- continuously exercise authentication, authorization, token, logout and revocation traffic across
  source-serving Expand, target handoff and target-serving Migrate;
- add real-server federated-flow coverage for JIT provisioning, account linking and read-only
  legacy-subject resolution. Other restricted-mode feature gates are covered by the operating-mode
  integration suite;
- measure request latency and PostgreSQL lock behavior during each schema operation;
- exercise migration interruption, resume, readiness failure, target-startup failure and explicit
  contract authorization in the integrated topology;
- run against both separate logical databases and one shared physical database; and
- verify the supported source and target artifact pair rather than only loading the source schema.

Before production qualification, add configurable backfill batch sizing, pacing or throttling and
resource limits so operators can bound migration load while the target serves.

Publish an operator runbook covering prerequisites, backups, restricted-mode activation, migration
commands, monitoring, deployment-selective execution, source draining, contract authorization,
failure recovery and rollback boundaries.

### Conditional in-place capabilities

The following capabilities are not required until a concrete release or deployment workflow needs
them:

- an engine-managed `contract_ready` boundary for stores with contract-only plans; the current safe
  workflow rejects such plans because no pre-contract phase can establish target-serving readiness;
- product-owned dual-read compatibility for each in-place representation migration. This is a
  release prerequisite, not generic migration-engine behavior;
- standalone service-enabled execution only after a real migration operation requires a ThunderID
  business service; the dependency-injection seam is already present and no speculative operation
  should be added;
- change capture for every affected record set that remains writable during backfill, including but
  not limited to login and token-related data;
- ordering and conflict handling without adding migration-only versions to every business table;
- transactional outbox support where application-level capture is required;
- activation and observation-period orchestration above the implemented explicit contract command.

## Database-copy ZDM backlog

These capabilities belong to the later database-copy and blue-green phase:

- source snapshot coordination;
- ordered source change capture;
- transformation and idempotent destination application;
- destination inbox or equivalent deduplication state when required;
- replication lag and readiness checks;
- cutover fencing and final catch-up;
- rollback-boundary handling; and
- reconciliation before and after cutover.

## Database-provider backlog

The common orchestration selects a registered database adapter and metadata query set rather than
containing PostgreSQL branches. PostgreSQL is currently the only registered implementation. SQLite
is not a production ZDM target and does not need migration-engine support in this phase. MySQL,
Microsoft SQL Server and Oracle support should be added by registering their namespace identity,
metadata queries, schema DDL and operation implementations after the common engine contract is
stable.

## Change rules

When implementing a backlog item:

1. Add only the schema and abstractions required by that behavior.
2. Keep migration policy and state-transition validation centralized in the engine.
3. Increment `migrationMetadataVersion` when stored metadata structure or interpretation becomes
   incompatible.
4. Add tests for interruption, retry and invalid state before relying on resume behavior.
5. Update this document by removing or refining the completed item.
