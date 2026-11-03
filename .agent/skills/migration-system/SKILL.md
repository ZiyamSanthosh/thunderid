---
name: migration-system
description: Entry point and map of the ThunderID database migration system. Use whenever work touches backend/internal/system/migration, the `thunderid migration` commands, release manifests, migration operations, the startup compatibility gate, restricted operating mode, or any question about upgrading a ThunderID database with zero downtime (expand, migrate, contract). Explains the mental model and routes to the planning, operation, testing, review and engine skills.
---

# ThunderID Migration System

ThunderID upgrades its PostgreSQL databases while the server keeps serving. A release ships a
**manifest** (which database levels it needs) and an ordered **plan** of **operations**. A standalone
command runs the plan in three **phases**: expand, migrate, contract. State lives in four metadata
tables inside the databases themselves, so a crashed run can be resumed or rolled back.

Read this file first. Then load only the skill the task needs.

## Which skill to use

| The task is | Load |
|---|---|
| Turning a developer's change request ("add a column", "rename this attribute", "move this data") into a migration design: which phase, which operation type, rollback, compatibility | [migration-plan](../migration-plan/SKILL.md) |
| Writing or changing one operation: SQL, verifiers, readiness, batches, rollback operation, registration | [migration-operation](../migration-operation/SKILL.md) |
| Writing tests for an operation or a plan: unit tests, Docker integration tests, failure injection | [migration-testing](../migration-testing/SKILL.md) |
| Reviewing a migration pull request or auditing an operation | [migration-review](../migration-review/SKILL.md) |
| Changing the engine itself: coordinator, state controller, executors, gate, metadata store | [migration-engine](../migration-engine/SKILL.md) |
| An operation that needs a ThunderID business service, not just SQL | [migration-service-operation](../migration-service-operation/SKILL.md) after reading [migration-operation](../migration-operation/SKILL.md) |

A normal feature flows through the skills in this order: **plan, operation, testing, review**.
Do not write an operation before the plan exists and the developer has confirmed it.

## Background references

- [concepts.md](concepts.md): stores, namespaces, manifest, levels, plan, operations, state machine, metadata tables.
- [lifecycle.md](lifecycle.md): the upgrade timeline, who serves when, commands, failure and recovery, restricted mode.

## Where the code lives

| Path | Holds |
|---|---|
| `backend/internal/system/migration/engine/` | The engine: coordinator, state controller, executors, preflight, compatibility gate, rollback, metadata store, locks |
| `backend/internal/system/migration/releases/<version>/` | One immutable release: `release.go` (plan), `release-manifest.json`, one file per operation (`NN_name.go`), shared helpers |
| `backend/internal/system/migration/current/` | Selects the release compiled into the binary and exposes the manifest catalog |
| `backend/cmd/server/migration_*.go` | CLI wiring: argument parsing, namespace building, declarative providers, startup gate |
| `backend/internal/system/middleware/operatingmode.go` and `featuregate` | Restricted mode fences |
| `backend/tests/migration/` | Docker based integration suite (build tag `migration_integration`) |

## Invariants that must never break

1. **Old and new binaries overlap.** Expand must be harmless to the old binary. Migrate and contract run
   only after the old binary is retired. See [lifecycle.md](lifecycle.md).
2. **Every operation is idempotent through its verifiers.** Before running it must classify the target as
   source, target or invalid. A target that already has the result is success, never an error.
3. **One writable store per operation.** An operation writes only its own store. It may read other
   stores it declares. There are no cross-database transactions.
4. **Data and progress commit together** for atomic and resumable operations. Never move progress
   bookkeeping outside the engine's executors.
5. **Contract is explicit and irreversible by default.** The generic phase command refuses it. Once a
   contract operation completes without a rollback operation, complete rollback is blocked.
6. **Releases are immutable once shipped.** Never edit the operations or manifest of a released version.
   Fix forward with a new release.
7. **PostgreSQL only.** Every SQL operation needs an explicit PostgreSQL implementation. A query with no
   text for the active database type is refused.
8. **Deployment scoped data migrates per deployment.** Data operations must honour a deployment
   selection and record progress per deployment.

## Verify any change

```bash
cd backend
go test ./internal/system/migration/... ./cmd/server/
go test -tags migration_integration -run '^$' ./tests/migration/releases/<version>   # compiles the Docker suite only
```

The Docker suite itself needs a running container runtime. See [migration-testing](../migration-testing/SKILL.md).

Repository rules still apply: always use the full product name ThunderID; no em dashes in prose or strings;
one commit per pull request; keep changes minimal.
