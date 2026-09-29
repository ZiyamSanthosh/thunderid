# Zero-downtime version-upgrade current status

## Baseline

This document records the ThunderID implementation status relevant to zero-downtime version
upgrades. It separates capabilities present in product code from migration designs that are still
proposals.

The target architecture and mandatory release policy are defined in
[ThunderID zero-downtime migration decision](zero-downtime-migration-final-decision.md).

Repository baseline:

- Review date: 2026-09-23
- Branch: `migration-plan`
- Commit: `f677ecdbacfe`

Recheck this assessment after changes to server startup, database schemas, persistence stores,
deployment templates or migration tooling.

## Current conclusion

ThunderID does not currently support zero-downtime version upgrades as a product guarantee.

ThunderID has deployment-level availability features that can keep replicas serving during an
ordinary rolling replacement when both application versions use a compatible, unchanged database
representation. It does not yet have the database migration capabilities required to guarantee a
rolling upgrade when schema or persisted-data changes are involved.

The current position is:

```text
Available:
    Runtime high availability and rolling pod replacement
    Logical database separation
    Transaction-aware database access

Designed but not implemented:
    Version-aware migration engine
    Transactional outbox
    Online backfill and delta processing
    Expand-and-contract activation
    Database-copy CDC
    Database compatibility enforcement

Target:
    Guaranteed ZDM for adjacent versions after a foundation release
```

## Capability assessment

| Capability | Current status | Notes |
|---|---|---|
| Multiple server replicas | Available | Helm defaults to two replicas |
| Rolling pod replacement | Available | Helm uses rolling update with zero unavailable pods by default |
| Startup, readiness and liveness probes | Available | Supports safe traffic routing at the pod level |
| Logical database separation | Available | Config, entity, runtime-transient and runtime-persistent stores are separate logical concerns |
| Transaction-aware database writes | Available | Store operations can share a transaction through context-aware database access |
| Partial SSO versioning | Available | `SSO_SESSION.VERSION` exists, but the complete session aggregate does not have consistent mutation ordering |
| Login-only operating mode | Not implemented | There is no product mode that centrally permits login and token traffic while rejecting every incompatible management action |
| Migration state and progress metadata | Not implemented | No implemented migration-state or migration-progress tables were found |
| Application and schema compatibility gate | Not implemented | Startup does not verify a recorded database migration level before initializing services |
| Versioned migration engine | Not implemented | There is no released ordered migration, checkpoint, resume and verification engine |
| Runtime-persistent transactional outbox | Not implemented | Runtime-persistent mutations do not currently emit a common durable change event |
| Online backfill framework | Not implemented | There is no common throttled, resumable and version-aware backfill runner |
| Expand-and-contract activation control | Not implemented | There is no common state machine for old-read, dual-write, new-read and contraction stages |
| Database-copy CDC | Not implemented | Snapshot, change transport, transformation, destination application and cutover remain design work |
| Mixed-version database compatibility contract | Not established | Adjacent ThunderID versions are not certified to run concurrently against an expanded schema |
| Provider-qualified online DDL plans | Not established | Lock and DDL behavior is not yet encoded and tested as a migration capability per provider |

## Existing foundations

### Deployment-level availability

The Helm deployment provides useful runtime availability building blocks:

- two replicas by default;
- rolling deployment strategy;
- `maxSurge` set to one by default;
- `maxUnavailable` set to zero by default;
- startup, readiness and liveness probes; and
- a PodDisruptionBudget.

These features can keep traffic flowing while compatible pods are replaced. They do not make a
database-changing release compatible with the old version.

Relevant implementation:

- `install/helm/values.yaml`
- `install/helm/templates/thunderid-deployment.yaml`
- `install/helm/templates/pdb.yaml`

### Logical store separation

ThunderID separates four logical database responsibilities:

| Store | Current responsibility |
|---|---|
| `config` | Applications, authentication flows, identity providers and other configuration |
| `entitydb` | Identities, groups, credentials and indexed entity attributes |
| `runtime_transient` | Short-lived authorization, token-protocol and authentication-flow state |
| `runtime_persistent` | SSO sessions, consent and revocation state that survives restarts |

This is a useful ZDM foundation because a release can choose a migration approach per store rather
than moving all data as one unit.

### Transaction infrastructure

The database transaction layer places the active transaction in the request context. The shared
database client automatically uses that transaction for context-aware queries and mutations.

This makes it possible to add a runtime-persistent outbox event in the same transaction as its
business mutation. The outbox integration itself does not exist yet.

Relevant implementation:

- `backend/internal/system/transaction/transactioner.go`
- `backend/internal/system/transaction/context.go`
- `backend/internal/system/database/provider/dbclient.go`

### Runtime-persistent schema

The current runtime-persistent schema contains:

- revoked tokens;
- revocation criteria;
- SSO sessions;
- SSO session contexts;
- SSO session participants;
- consent; and
- consent authorization records.

`SSO_SESSION` contains a version used by some session updates. Context and participant mutations do
not consistently advance one aggregate generation, so the current version cannot by itself order
every SSO change for migration projection.

Relevant implementation:

- `backend/dbscripts/runtime_persistent/postgres.sql`
- `backend/internal/flow/session/store_constants.go`
- `backend/internal/flow/session/service.go`

## Current limitations

### Schema scripts initialize databases but do not form an upgrade system

ThunderID provides PostgreSQL and SQLite schema scripts and initialization tooling. The current
tooling does not provide an ordered history of version transitions, per-store migration levels,
verified resume or a compatibility decision for normal startup.

Relevant implementation:

- `backend/dbscripts/`
- `backend/scripts/init_script.sh`
- `setup.sh`

### Startup does not enforce database compatibility

Server startup loads configuration and initializes services without first checking a durable record
of database migration level against the running ThunderID version.

Consequently, startup cannot currently reject every incompatible, partially migrated or newer
database state before ordinary services initialize.

Relevant implementation:

- `backend/cmd/server/main.go`
- `backend/cmd/server/servicemanager.go`

### Runtime-persistent changes are not captured as a common stream

SSO, consent and revocation stores write their business tables directly. There is no common outbox
record created alongside every mutation.

Therefore, a long backfill cannot currently rely on a durable product-level stream to find every
insert, update and delete that occurred while the backfill was running.

### Cleanup is an independent writer

The PostgreSQL runtime-persistent cleanup procedure directly deletes expired SSO contexts,
participants and sessions. A future application-level outbox design must make cleanup produce
tombstones or replace it with an outbox-aware cleanup path.

Relevant implementation:

- `backend/dbscripts/runtime_persistent/postgres-cleanup.sql`

### Runtime-transient data cannot be assumed disposable during an upgrade

Runtime-transient records expire, but active records can represent in-flight authorization,
authentication, token, WebAuthn, verifiable-credential or replay-prevention state. Removing the store
during an upgrade can interrupt active flows or weaken single-use enforcement.

An online upgrade should share a compatible transient representation or drain it according to the
maximum relevant TTL.

### Login-only mode is not an implemented safety boundary

There is no central operating mode that blocks all configuration and management actions while
allowing a defined set of login and token endpoints. Individual declarative resources can be
read-only, but that is not equivalent to a migration-aware login-only mode.

Even under a future login-only mode, writes can remain in:

- `runtime_transient` for protocol and flow state;
- `runtime_persistent` for SSO, consent and revocation;
- `entitydb` for passkey counters, provisioning or lifecycle state, depending on enabled features;
  and
- `config` during startup or reconciliation unless those writers are explicitly fenced.

## What is possible today

### Rolling application replacement without database changes

A zero-unavailable rolling replacement is operationally possible when:

- the database representation is unchanged;
- both binaries are behaviorally compatible with the shared stores;
- configuration changes remain backward compatible;
- no bootstrap or initialization behavior conflicts across versions; and
- readiness accurately represents the ability to serve requests.

This is not currently published as a formally tested version-upgrade contract.

### Database-changing upgrade

For a release requiring schema or persisted-data transformation, controlled downtime is currently
the reliable general approach:

1. Block Control Plane mutations.
2. Stop Data Plane writers.
3. Take and verify a backup.
4. Apply the database transformation.
5. Validate the result.
6. Start the new version.

A particular additive change may be safe to apply online after provider-specific analysis, but that
is a migration-specific exception rather than a general ThunderID ZDM capability.

## Proposed direction, not current functionality

The working migration strategy proposes:

1. A foundation release after 1.0.
2. Per-store migration metadata and startup compatibility checks.
3. A standalone Go migration mode or executable.
4. A transactional outbox for runtime-persistent mutations.
5. Resumable online backfills and version-aware delta processing.
6. In-place expand-and-contract as the default online migration.
7. Database-copy migration with outbox-based CDC for incompatible or high-risk changes.
8. Controlled downtime as the final fallback.

These items are documented proposals and must not be described as released ThunderID features.

Relevant proposals:

- `version-upgrade-migration-strategy.md`
- `migration-upgrade-overview.md`
- `migration-design-decisions.md`
- `cdc-database-copy-migration-recommendation.md`
- `login-only-mode-current-state.md`

## Version-upgrade implication

### From 1.0

The 1.0 release does not provide complete outbox coverage or a formal mixed-version database
contract. The next release can attempt a rolling transition only when its changes remain compatible
and have been specifically verified. Planned downtime remains the supported safety fallback.

### After a foundation release

Once every writable instance runs a release containing the migration foundation, later adjacent
releases can rely on the recorded compatibility level and runtime-persistent change stream.

The intended target is:

```text
1.0 to foundation release
    Best-effort online transition with downtime fallback

Foundation release to next adjacent release
    ZDM through in-place expand-and-contract by default
    Database copy with outbox-based CDC when required
```

## Minimum work before claiming ZDM support

ThunderID should not claim database-changing ZDM until it has:

1. A defined adjacent-version compatibility policy.
2. Durable migration state for each participating logical store.
3. A read-only startup compatibility gate.
4. Complete runtime-persistent outbox coverage, including cleanup paths.
5. Resumable and version-aware backfill support.
6. Idempotent event consumers with independent checkpoints.
7. Explicit representation activation and contraction controls.
8. Provider-specific DDL and lock qualification.
9. Mixed-version, failure-recovery and cutover tests.
10. Operator documentation covering preflight, monitoring, validation and rollback boundaries.

## Updating this baseline

When implementation work begins, update the capability table and baseline commit. Evidence should
come from product code, schemas, tests and deployment artifacts. Design documents alone do not change
a capability from proposed to available.
