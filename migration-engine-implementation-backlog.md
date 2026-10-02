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
- empty PostgreSQL `MIGRATION_STATE` and `MIGRATION_PROGRESS` tables in every fresh database
  creation script;
- Go models and validation for migration state and operation progress;
- a metadata store for validated state and operation-progress reads and writes;
- a transactional PostgreSQL initialization service for the fixed pre-framework baseline and fresh
  manifest targets;
- a read-only compatibility gate that evaluates every manifest store and aggregates failures;
- a validated migration plan registry with ordered operations and per-operation store, interruption
  and progress-scope declarations;
- a required provider-neutral operation readiness contract with immutable plan context and
  standardized ready, deferred and blocked results;
- an engine-owned preflight runner that provides declaration-scoped read access, validates every
  operation result, propagates blocked dependencies and aggregates findings in plan order;
- validated scope and verification contracts plus an immutable atomic `SQLOperation` that requires
  an explicit ordered SQL implementation for every registered database provider;
- engine-owned execution access that permits reads only from declared stores and writes only to an
  operation's owning store;
- an engine-owned operation execution envelope, with `SQLOperation` selecting its explicit provider
  implementation and executing its statements in declared order;
- an atomic store-scope executor that reuses ThunderID's transactioner to verify reservation
  ownership, pre-verify, execute or skip, post-verify and complete operation progress in one
  transaction;
- transactional store finalization that validates completed operation progress and conditionally
  advances only the run's still-reserved store to its target level;
- conditional run-state recovery primitives that cancel unused reservations before execution and
  mark still-reserved stores failed after an orderly execution error;
- transactional namespace-level store reservation using a conditional metadata update to prevent
  concurrent migration ownership;
- a provider-neutral migration execution-lock contract, with PostgreSQL session advisory locking,
  that serializes migration runners per physical database namespace without locking application
  tables or preventing serving traffic;
- a basic migration coordinator that generates one run identifier, reserves database namespaces in
  deterministic manifest order, repeats operation readiness after reservation, executes atomic
  store-scoped operations in global plan order, finalizes each store after its last operation and
  distinguishes pre-execution cancellation from durable execution failure;
- recovery for failed and abandoned atomic runs through `migration resume`, including run ownership
  and route validation, reuse of completed progress, readiness checks for only pending operations
  and conditional failed-to-in-progress transitions;
- stable command outcome logs that retain partial reports on failure and return success only when
  the requested compatibility or readiness condition is satisfied;
- a store-level release-route resolver that reports the next migration-owning ThunderID release and
  supports multi-hop paths through an ordered manifest catalog;
- an isolated PostgreSQL `migration initialize` command that runs before normal service setup and
  groups logical stores sharing the same configured database namespace;
- a read-only PostgreSQL `migration status` command that reports compatibility for every configured
  logical store without creating or modifying migration metadata;
- a read-only PostgreSQL `migration check` command that classifies stores as already at target,
  migration-required or blocked and aggregates operation-specific readiness findings;
- a PostgreSQL `migration apply` command that runs the basic coordinator as a dedicated execution
  of the ThunderID binary without starting serving components;
- a read-only PostgreSQL startup compatibility gate that runs before caches, bootstrap, background
  workers and request listeners, while unsupported development database providers remain outside
  the Phase 1 gate;
- a first 2.0.0 atomic PostgreSQL operation that removes the legacy `REVOKED_TOKEN` reason check
  constraint from `runtime_persistent`, with catalog-based readiness and verification plus a bounded
  DDL lock wait;
- a second 2.0.0 atomic PostgreSQL operation that creates the `AUTHORIZATION_PDP_CONNECTION` table
  and its unique index in `configdb`, with exact catalog-based readiness and verification;
- provider-neutral typed query-row helpers for operation readiness and verification queries;
- a Docker Compose PostgreSQL integration test that builds the target ThunderID binary, obtains the
  configured source release schema from its published image, initializes all four logical
  databases, executes the complete migration twice and verifies schema and metadata outcomes in
  both separate-database and single-shared-database layouts;
- provider-neutral namespace and metadata-store selection, with PostgreSQL currently registered
  as the only implementation;
- minimal structural database constraints; and
- a test that keeps the metadata schema identical across the four logical databases.

The metadata tables currently store only the information required for store compatibility,
migration-run correlation, failure reporting, ordered operation progress and optional resume data.

## Migration engine foundation status

The Phase 1 migration foundation is functionally complete for PostgreSQL. Implementation increments
do not define a separate downtime-oriented architecture.

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

## Deferred metadata additions

The following ideas were removed from the initial schema. They are not automatically required.

| Deferred item | Add only when |
|---|---|
| Current operation identifier in `MIGRATION_STATE` | State inspection cannot identify the active operation reliably from `MIGRATION_PROGRESS`. |
| Structured failure code and failure timestamp | Commands need machine-readable remediation or reliable failure-time reporting. |
| Run start, operation start and completion timestamps | Operational reporting or stale-run recovery has a defined use for them. |
| Checkpoint representation version | A checkpoint format must remain readable across different migration-engine versions. |
| Deployment or batch scope columns | The engine implements independently resumable scoped or partitioned backfills. |
| Run and status indexes | Measured query plans show that metadata lookups require them. Metadata tables are expected to remain small. |
| Compound database `CHECK` constraints | A rule is stable across engine versions and database enforcement materially improves correctness. |
| Additional migration tables | A new durable concept cannot be represented clearly in the two existing metadata tables. |

Primary keys, foreign keys, uniqueness and required-column constraints remain in the database.
Behavioral rules and state transitions remain in Go unless there is a demonstrated reason to enforce
them in every supported database.

## In-place ZDM backlog

These capabilities belong to the second implementation phase, after the basic engine works:

- expand-and-contract migration phases;
- phase-aware compatibility between old and new binaries;
- capability fencing and restricted-mode behavior;
- bounded and throttled backfills;
- change capture for records that remain writable during backfill;
- ordering and conflict handling without adding migration-only versions to every business table;
- transactional outbox support where application-level capture is required;
- activation, observation and contract steps; and
- mixed-version and interruption integration tests.

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
