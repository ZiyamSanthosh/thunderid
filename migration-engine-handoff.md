# Migration engine handoff

## Current position

The PostgreSQL migration-engine foundation is functionally complete. ThunderID now has the engine,
commands, metadata, compatibility gates, realistic release operations, readiness checks, recovery
and rollback behavior needed to qualify an in-place zero-downtime migration workflow.

The system is not yet a production ZDM guarantee. The remaining required work is integrated
mixed-version qualification while a source instance continues serving real traffic, followed by
serving-impact controls and operator documentation.

The current branch contains these recent migration commits:

- `3db8e9f95 Add explicit migration rollback lifecycle`
- `8ba2d3df6 Add operation readiness checks`
- `70e2d51f6 Align entity type handle rollback naming`
- `0cfc71202 Update migration design records`

## Implemented foundation

- Embedded release manifests and ordered `expand`, `migrate` and `contract` plans.
- Four-table PostgreSQL migration metadata shared by all logical stores.
- Initialization, status, phase readiness, apply, resume and rollback commands.
- Startup compatibility enforcement before normal service initialization.
- Atomic and resumable execution with transactional progress and checkpoints.
- Namespace-level PostgreSQL execution ownership locks.
- Complete-store and deployment-selective migration targets.
- Separate-database and shared-physical-database operation.
- Restricted operating mode and feature gates for incompatible writes.
- Explicit contract authorization after source instances are drained.
- Failed-operation, failed-phase and complete pre-contract rollback.
- Realistic 2.0.0 PostgreSQL schema and data operations.
- Operation-specific readiness checks for every forward 2.0.0 operation.
- PostgreSQL integration coverage for migration, recovery, concurrency, deployment selection and
  rollback behavior.
- Manual PostgreSQL POC setup and teardown scripts.

## Readiness coverage

The 2.0.0 release operations now report deterministic preflight state before reservation:

1. The revoked-token constraint operation recognizes the exact source and target schemas.
2. The authorization PDP table operation validates relation names, columns, primary key and index.
3. The entity-type handle expansion validates the source column and nullable target column.
4. The entity type length expansion accepts only the supported 50-to-100 transition.
5. The handle backfill defers until expansion, then validates handle syntax and uniqueness.
6. Handle uniqueness defers until backfill and validates the required column and constraint layout.
7. Contract validates the final constraint layout and confirms that no null handles remain.

Execution-time verification remains mandatory because the database can change after preflight.

## Rollback coverage

Rollback is explicit and is never triggered automatically. Supported scopes are:

- the failed operation and execution target;
- completed work in the failed phase; and
- every compensatable operation before contract.

The realistic rollback operation is
`2.0.0/configdb/04_backfill_entity_type_handle_rollback`. It clears the complete handle
representation in bounded resumable batches before returning to the source version.

## Required next work

### 1. Integrated in-place qualification

Automate a complete topology that:

1. starts a supported source artifact in restricted mode;
2. continuously exercises authentication, authorization, token, logout and revocation traffic;
3. proves that fenced mutations fail before their first write;
4. runs the target artifact as a standalone migration process;
5. exercises interruption and resume during the pre-serving phases;
6. verifies that target readiness is reported only after operation verification;
7. starts a target serving instance against the migrated databases;
8. shifts traffic, drains the source instances and explicitly runs contract; and
9. proves that the source version is rejected after contract.

Run this qualification with separate logical databases and with all logical stores sharing one
physical PostgreSQL database.

### 2. Serving-impact and resource qualification

- Measure request latency while schema operations acquire their bounded locks.
- Exercise the two-second lock timeout under contention.
- Add configurable backfill batch sizing, pacing or throttling and resource limits.
- Verify that migration load stays within the documented serving budget.

### 3. Broader failure injection

Add integrated failures during source traffic, readiness evaluation, migration execution, target
startup, traffic handoff and contract authorization. Existing lower-level coverage already includes
resumable interruption, abandoned-run recovery, concurrent-runner rejection and interrupted
rollback.

### 4. Operator documentation

Publish a runbook covering prerequisites, backups, restricted mode, every migration command,
deployment selection, monitoring, failure recovery, source draining, contract and rollback
boundaries.

### 5. Documentation maintenance

Keep `migration-engine-implementation-backlog.md` and
`zero-downtime-upgrade-current-status.md` aligned with implementation evidence. Historical review
files are reference material and are not the source of truth for pending work.

## Conditional future work

Do not implement these without a concrete release or product requirement:

- service-enabled migration operations;
- change capture, transactional outbox and conflict handling;
- target serving while `migrate` is still running;
- hosted background migration coordination;
- contract-only release plans;
- database-copy or blue-green migration;
- machine-readable report files; and
- production providers other than PostgreSQL.

The service-dependency construction seam already exists. Add concrete service initialization only
when an actual operation needs it.

## Current workspace note

`backend/cmd/server/deployment.yaml` contains local uncommitted configuration, including plaintext
SMTP credentials. Do not commit that file until the credentials are removed and rotated if they are
real.

## Validation baseline

The latest readiness work passed:

```bash
cd backend
go test ./internal/system/migration/...
go test -tags migration_integration -run '^$' ./tests/migration/releases/v2_0_0
```

The second command compiles the integration suite without starting Docker. The user separately
confirmed that the POC workflow behaves as expected after the readiness changes.

## Recommended next task

Implement the automated mixed-version in-place qualification harness. Do not add speculative
service operations or change-capture infrastructure before that qualification or a concrete
migration requires them.
