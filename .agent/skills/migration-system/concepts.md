# Migration concepts

## Logical stores and namespaces

ThunderID has four logical stores, each a database in configuration:

| Store id | Holds |
|---|---|
| `configdb` | Identity configuration: applications, flows, roles, identity providers, entity types |
| `entitydb` | Identity data: users, agents, groups, organization units |
| `runtime_transient` | Short lived state: codes, requests, flow contexts |
| `runtime_persistent` | Long lived state: revocations, SSO sessions, consent |

Stores can live in separate databases or share one. Stores on the same `host:port/database` form one
**namespace**. A namespace has one advisory lock and one transaction scope. The CLI groups stores
into namespaces and refuses a shared namespace whose stores use different connection identities.

Each store has its own state row and its own operations. Design for both layouts: separate and shared.

## Release manifest

`release-manifest.json` per release. For every store:

| Field | Meaning |
|---|---|
| `acceptedSourceLevel` | The level the store must be at before this release can migrate it |
| `requiredLevel` | The level the store has after the release |
| `changed` | Whether this release migrates the store |

Rules enforced by validation: a changed store advances its level and its required level equals the
release version; an unchanged store keeps its level and may not have operations. Levels are canonical
semantic versions. `migrationMetadataVersion` is the version of the metadata tables, not of the release.

## Plan and operations

A plan has four lists: `Expand`, `Migrate`, `Contract` and `Rollback`. List order is execution order.
Every changed store needs at least one operation. An operation is described by:

| Field | Values |
|---|---|
| `ID` | `<release>/<store>/<NN>_<snake_name>`, unique in the plan |
| `StoreID` | The one store it writes |
| `ReadStoreIDs` | Extra stores it may read |
| `Kind` | `schema_change` or `data_change` |
| `InterruptionMode` | `atomic`, `online` or `resumable` |

Allowed combinations: `resumable` requires `data_change`. `online` requires `schema_change`. A schema
change cannot define a target resolver; a data change must.

Every operation implements: `Definition`, `ResolveTargets`, `PreVerify`, `PostVerify`, plus one
execution interface (`Execute`, `Execute` and `Finalize`, or `ExecuteBatch`). It may implement
`CheckReadiness`. A rollback operation also implements `ForwardOperationID`.

### Execution target

The unit one execution applies to. Empty deployment id means the whole store. A data change may
resolve to one target per selected deployment; each gets its own progress row and checkpoint.

### Verification states

| State | Meaning | Engine reaction |
|---|---|---|
| `source` | Exactly the shape this operation expects to change | Run it |
| `target` | Already has the result | Record complete, do not run |
| `invalid` | Neither, or half done in an unrecognised way | Stop, report findings |

`invalid` needs at least one finding (`Code`, `Message`). Summary text is required on every result.

### Readiness states

Checked before anything runs, read only, whole plan at once.

| State | Meaning |
|---|---|
| `ready` | Everything observable now is satisfied |
| `deferred` | Depends on an earlier operation of the plan. Lists it in `DeferredUntil` |
| `blocked` | A known condition prevents safe execution. Needs findings with remediation text |

Readiness is checked again after the namespace is reserved, and verification runs again at execution.
All three are required. Readiness gives operators early warning, verification is the safety check.

## Metadata tables

Created by `migration initialize` in every namespace.

| Table | One row per | Holds |
|---|---|---|
| `MIGRATION_STATE` | logical store | completed level, target level, stage, status, active run id, failure summary |
| `MIGRATION_PROGRESS` | operation per target | run id, order, status, checkpoint JSON (max 64 KiB) |
| `MIGRATION_DEPLOYMENT_STATE` | store per deployment | the level a deployment completed |
| `MIGRATION_RUN` | active run | mode, phase, selection, rollback scope, failed operation identity |

## State machine of a store

Stages: `complete`, `expanding`, `expanded`, `migrating`, `contract_ready`, `contracting`,
`rolling_back`. Statuses: `clean`, `in_progress`, `completed`, `failed`.

```text
complete/clean @source
   apply expand   -> expanding/in_progress -> expanded/completed
   apply migrate  -> migrating/in_progress -> contract_ready/completed
   apply contract -> contracting/in_progress -> complete/clean @target
any executing stage may become failed. rolling_back is used by revert-all.
```

A phase can only start from the boundary the previous phase left. A store that declares only some
phases is handled: its first declared phase starts from clean, and a metadata-only contract is added
implicitly after its last declared phase.

## Runs and locks

A run is one reservation of stores for one phase, with a UUIDv7 run id. The coordinator takes a
PostgreSQL session advisory lock per namespace (non-blocking) before doing anything. Application
traffic never touches this lock. A second runner fails immediately with "already locked".
