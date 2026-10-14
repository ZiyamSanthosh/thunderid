# Zero-downtime version-upgrade current status

## Baseline

This document records the ThunderID implementation status for zero-downtime version upgrades. It
separates implemented product capabilities, remaining qualification work and conditional future
patterns.

The target architecture and release policy are defined in
[ThunderID zero-downtime migration decision](zero-downtime-migration-final-decision.md). The detailed
implementation backlog is maintained in
[Migration engine implementation backlog](migration-engine-implementation-backlog.md).

Repository baseline:

- Review date: 2026-10-13
- Branch: `migration-plan`
- Latest documentation baseline: `0cfc71202`

## Current conclusion

The PostgreSQL migration-engine foundation is functionally complete. ThunderID has a standalone,
phase-aware migration system with compatibility enforcement, readiness checks, resumable execution,
deployment-selective progress, restricted serving mode and explicit rollback.

ThunderID must not yet claim a production database-changing ZDM guarantee. The individual
capabilities have integration coverage and a manually validated POC, but the complete mixed-version
serving topology has not been automated and qualified under continuous traffic.

```text
Implemented:
    PostgreSQL migration metadata and compatibility gates
    Standalone initialize, status, check, apply, resume and rollback commands
    Expand, migrate and contract execution
    Atomic and resumable operations
    Operation-specific readiness checks
    Restricted operating mode and feature gates
    Deployment-selective progress
    Explicit pre-contract rollback

Required next:
    Automated mixed-version in-place qualification
    Serving-impact and backfill resource qualification
    Broader integrated failure injection
    Operator runbook

Conditional:
    Service-enabled operations for a real service-backed migration
    Change capture and conflict handling for writable representations
    Hosted migration, database-copy ZDM and additional providers
```

## Capability assessment

| Capability | Status | Evidence or remaining condition |
|---|---|---|
| Release manifests and ordered plans | Implemented | Validated release registry with expand, migrate, contract and rollback declarations |
| Migration state and progress metadata | Implemented | Four PostgreSQL metadata tables shared consistently across logical stores |
| Startup compatibility gate | Implemented | Runs before normal service initialization and rejects incompatible stores |
| Standalone migration commands | Implemented | Initialize, status, check, apply, resume and rollback run without starting the server |
| Atomic migration execution | Implemented | Transactional verification, execution, progress and completion |
| Resumable migration execution | Implemented | Bounded batches with transactional checkpoints and resume |
| Operation readiness | Implemented | Every forward 2.0.0 operation reports ready, deferred or blocked state |
| Deployment-selective migration | Implemented | Per-deployment targets and durable deployment completion state |
| Shared physical database support | Implemented | Logical stores are grouped and serialized by PostgreSQL namespace |
| Restricted operating mode | Implemented | Runtime endpoints remain available while incompatible features are fenced |
| Explicit rollback | Implemented | Failed-operation, failed-phase and complete pre-contract scopes |
| Realistic PostgreSQL operations | Implemented | Schema expansion, table creation, backfill, constraints, contract and rollback |
| PostgreSQL integration coverage | Implemented | Separate and shared layouts, resume, concurrency, deployment selection and rollback |
| Complete mixed-version serving qualification | Pending | Must keep source traffic active through standalone migration and target handoff |
| Serving-impact qualification | Pending | Must measure DDL lock behavior, latency and backfill resource use |
| Configurable backfill resource controls | Pending | Batch size, pacing or throttling and limits must be operator-controlled |
| Operator runbook | Pending | Must document the supported workflow and recovery boundaries |
| Service-enabled operation runtime | Conditional | Add only when a real migration requires a ThunderID business service |
| Change capture and conflict handling | Conditional | Required only for representations that remain writable during backfill |
| Database-copy migration | Conditional | Later fallback for migrations unsuitable for in-place expansion |
| Additional production providers | Conditional | PostgreSQL is the only currently qualified implementation target |

## Implemented workflow

The target ThunderID artifact owns the migration definitions and runs as a standalone administrative
process. The default pre-serving workflow is:

1. Initialize or inspect migration metadata.
2. Run the read-only status and readiness checks.
3. Apply `expand` while the source version remains compatible.
4. Apply or resume `migrate` until every selected target is verified.
5. Reach `contract_ready` and start the target version.
6. Shift traffic and drain every source instance.
7. Explicitly authorize and apply `contract`.

The default apply workflow stops before contract. Contract cannot be reached accidentally through
the generic phase execution path.

## Implemented safety controls

### Compatibility and readiness

- Every logical store has a durable migration level and stage.
- Source and target binaries are admitted only at compatible boundaries.
- Readiness is repeated after the migration namespace is reserved.
- Every realistic forward operation recognizes its exact source and target representations.
- Dependency-aware readiness defers checks that require preceding operations.
- Execution-time pre-verification remains mandatory after readiness.

### Serving protection

- PostgreSQL migration runners serialize per physical database namespace with advisory locks.
- Application traffic is not serialized by the migration execution lock.
- Schema operations use a bounded two-second PostgreSQL lock timeout where applicable.
- Restricted mode fences registration, recovery, administration, JIT provisioning and organization
  unit creation before mutation.

### Recovery and rollback

- Atomic failures roll back their database transaction.
- Resumable operations persist checkpoints with each committed batch.
- Failed and abandoned forward runs can resume from durable state.
- Administrators can roll back a failed operation, a failed phase or the complete pre-contract
  release.
- A completed uncompensated contract operation blocks complete rollback.

## Required work before the ZDM claim

### Automated mixed-version qualification

The qualification harness must run a supported source artifact and target artifact together. It
must continuously exercise authentication, authorization, token, logout and revocation traffic
while the target artifact performs migration. It must then start the target server, shift traffic,
drain the source and apply contract explicitly.

The test must prove that:

- allowed traffic remains successful during migration;
- fenced mutations fail before writing;
- interruption and resume preserve availability and correctness;
- target readiness is not reported before verification finishes;
- target startup succeeds only at a compatible boundary;
- source startup or continued serving is rejected after contract; and
- both separate and shared PostgreSQL layouts behave consistently.

### Serving-impact qualification

Measure latency, lock waits and database load for every realistic operation. Exercise lock timeout
behavior under contention and establish an acceptable serving budget. Add configurable backfill
batch sizing, pacing or throttling and resource limits before production qualification.

### Failure injection

Extend the integrated topology with failures during readiness, schema execution, backfill, target
startup, traffic handoff and contract authorization. Lower-level tests already cover resumable
interruption, abandoned runs, concurrent runners and rollback interruption.

### Operator documentation

Publish a runbook covering backups, prerequisites, restricted mode, every migration command,
monitoring, deployment selection, source draining, contract authorization, resume and rollback
boundaries.

## Conditional future work

The following work is not part of the immediate base qualification:

- service-enabled migration operations, until a real operation requires business-service access;
- change capture, transactional outbox and conflict policies, until a migration keeps an affected
  representation writable during backfill;
- target serving while `migrate` is incomplete;
- hosted background migration coordination;
- contract-only release plans;
- database-copy or blue-green orchestration;
- JSON reports and report-file routing; and
- MySQL, Microsoft SQL Server or Oracle implementations.

SQLite is not a production ZDM target.

## Foundation-release boundary

An older release that does not contain the compatibility gate, restricted mode and migration
contract cannot provide the same guarantees as a foundation-capable source. Qualification must name
the exact supported source and target artifact pair. A transition from a pre-foundation release
remains best-effort unless that source release is explicitly retrofitted and qualified.

After the foundation release, adjacent versions can claim ZDM only when their migration plan,
mixed-version behavior, readiness checks, rollback boundary and provider-specific impact have all
passed the qualification suite.

## Updating this status

Update this document whenever implementation or qualification evidence changes. Code, database
schemas, automated tests and reproducible deployment artifacts are authoritative. Historical review
documents are supporting context, not the source of truth.
