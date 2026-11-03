---
name: migration-engine
description: Change or debug the ThunderID migration engine core in backend/internal/system/migration/engine and its CLI wiring in backend/cmd/server. Use when modifying the coordinator, state controller, executors, preflight, compatibility gate, route resolver, rollback, metadata store, execution locks, metadata schema, or migration commands and startup gate. Gives the architecture, the state transition rules, the invariants every change must keep, the test patterns with mock database clients, and the defects fixed before.
---

# Changing the Migration Engine

The engine is small but every part of it protects data. A change here affects every operator who
upgrades. Make the smallest change that fixes the problem and prove it with a test that fails first.

Read [migration-system](../migration-system/SKILL.md) and its [concepts](../migration-system/concepts.md)
before this file. Operation authors do not need this skill.

## File map

| File | Responsibility |
|---|---|
| `coordinator.go` | `MigrationCoordinator`: apply a phase, safe workflow, contract, resume. Takes execution locks, runs preflight, reserves, executes operations in order, finalizes, records failure |
| `state_controller.go` | `MigrationStateController`: every state transition per namespace transaction. Reserve, cancel, fail, resume, begin and complete rollback (operation, phase, revert-all), complete phase |
| `rollback.go` | Rollback orchestration: scope selection, candidates, reverse order, revert-all |
| `preflight.go` | Store assessment, operation readiness evaluation, `ReadAccess`, readiness result validation, `PlanContext` |
| `compatibility.go`, `route.go` | Serving gate and the release route search (breadth first over manifests) |
| `atomic_executor.go`, `online_executor.go`, `resumable_executor.go` | One executor per interruption mode |
| `operation.go` | Operation interfaces, `SQLOperation`, execution access fencing, target and selection validation |
| `plan.go`, `manifest.go` | Plan and manifest validation, `ReleaseDefinition` |
| `metadata.go`, `metadata_store.go`, `metadata_schema.go`, `postgres_metadata.sql`, `metadata_initializer.go` | Metadata model, SQL, schema creation, initialization |
| `execution_lock.go`, `postgres_execution_lock.go` | Advisory lock per namespace |
| `query_row.go` | Typed row accessors |
| `cmd/server/migration_cmd.go` and siblings | CLI parsing, namespace grouping, startup gate, declarative providers |

## Invariants

1. **A state transition is a compare-and-set on the whole previous state.** `TransitionState` and
   related queries match the stored row, including `UPDATED_AT`. A change that reuses an in memory state
   instead of re-reading it risks spurious conflicts. Re-read inside the transaction.
2. **One transaction per namespace for each transition.** Multi namespace runs reserve namespaces one
   after another, and a failure cancels the reservations already made. Never assume cross namespace
   atomicity.
3. **Progress and data commit together** for atomic operations (one transaction) and resumable batches
   (one transaction per batch with the checkpoint). Online operations commit `Execute` steps
   individually and finish with a transactional `Finalize`, post verification and progress update.
4. **Locks nest in namespace order** and are non blocking. Failure marking uses the lock context.
   Nothing may wait on an advisory lock.
5. **Verification is mandatory** around execution and is never skipped by the engine. Idempotency of
   re-runs depends on it.
6. **Stage and status together define serving.** `servingCompatibilityReason` decides who may serve.
   Changing it changes the zero downtime guarantee. Update the tests that pin the matrix.
7. **Run context lives in `MIGRATION_RUN`** (mode, phase, selection, rollback scope, failed operation
   identity). Anything that must survive a crash belongs there or in progress rows, never in memory.
8. **Rollback keeps the executing stage with status `in_progress`** for failed-operation and phase
   scopes, and the run mode records rollback. Only revert-all uses stage `rolling_back`. Any logic that
   treats "rollback in progress" must look at the run mode, not only at status `failed`.
9. **Selective migrate finalizes a store at `expanded`** with its deployment recorded. Resume and
   rollback must both accept that state.
10. **Schema operations always use the whole store target**, whatever the deployment selection.
11. **Contract only runs through the dedicated path.** The generic phase entry refuses it.
12. **The engine never imports business packages.** Services reach operations through release
    dependencies only.

## Transition map

```text
ReservePhaseSelection    complete/clean or previous boundary    -> executing stage / in_progress
CompletePhaseSelection   executing stage                        -> next boundary (or complete/clean at the end)
                         selective migrate                      -> expanded/completed + deployment state
MarkFailedWithFailure    reserved stores in the run             -> failed + failure identity in the run row
Resume                   failed                                 -> in_progress (same run)
CancelReservations       reserved, no progress                  -> previous boundary
BeginRollbackScope       failed/completed stores of a run       -> executing stage / in_progress, run mode rollback
CompleteRollback         rolled back stores                     -> boundary before the store's first declared phase, or the previous declared phase
CompleteOperationRollback failed operation                      -> run kept, operation progress removed
BeginRevertAll           changed stores                         -> rolling_back / in_progress
CompleteRevertAll        rolling_back                           -> complete/clean at the accepted source level
```

## How to make a change

1. **Reproduce first.** Write the failing test before the fix and confirm it fails for the stated
   reason. Revert the fix temporarily to prove the test catches it.
2. **Find every consumer.** A state or rule is usually read in several places: coordinator resume,
   rollback, preflight, compatibility gate, CLI output. Search for the stage, status or function you
   change and update all of them together.
3. **Keep the fix minimal.** No refactoring, renaming or cleanup of surrounding code.
4. **Keep metadata compatibility.** A change to the metadata tables or their meaning needs a bumped
   `migrationMetadataVersion`, an initializer and gate story, and a migration of metadata itself. That is
   a major decision. Ask.
5. **Update docs and skills** that describe the behaviour you changed.

## Test patterns

Engine tests live next to the code and use `testify` with the generated database mocks. Do not
hand edit mock files. Regenerate with `make verify_mocks` if an interface changes.

| Need | Helper in `engine/*_test.go` |
|---|---|
| Mock database client | `providermock.NewDBClientInterfaceMock(t)` from `backend/tests/mocks/database/providermock` |
| Transaction runner | `testTransactioner` (counts calls, runs the function) |
| Metadata store over a mock | `newTestMetadataStore(t, dbClient)` |
| Schema present | `expectReadySchema(dbClient)` |
| State rows | `phaseStateRow(stage, status, target, runID, updatedAt)`, `coordinatorStateRow`, `compatibilityStateRow`, `validStateRow` |
| Run and progress rows | `migrationRunRow(run)`, `completedProgressRow(...)` |
| Ids | `testStateID`, `testRunID` (valid UUIDv7) |
| Plans and manifests | `changedStoreManifest(storeID)`, `validPlanOperation(storeID)`, `coordinatorTestRelease()`, `twoStoreMigrateRelease()` |
| Coordinator | `newTestMigrationCoordinator(t, release, dbClient, transactioner)` |
| Preflight | `preflightStores(t, manifest, levelOverrides)`, `readinessTestOperation` |
| State controller | `newTestFinalizationController(t, release, dbClient, transactioner)` |

Mock expectations are matched by query object and arguments. When a fix adds a database read, existing
tests that exercise the path need a mock for it. Add the expectation rather than loosening the mock.
Order matters: several `.Once()` expectations for the same query are consumed in call order.

The CLI side uses `providermock.NewDBProviderInterfaceMock` and the helper
`expectMigrationExecutionLock` for the namespace lock.

Run with `-count=1` when iterating, so cached results do not hide a change.

## Defects fixed before. Do not reintroduce.

| Defect | Rule it taught |
|---|---|
| Resume rejected a store finalized by a selective migrate | Any "is the phase complete" check must know the selective finalization state. Use the shared `selectiveMigrateFinalized` helper |
| Forward resume of a crashed rollback | "Rollback in progress" guards must inspect every active store, not just failed ones |
| Preflight reported unexpected levels as at target | A store with no operations in a phase is at target only if its level is the accepted source or the required level |
| Schema operations recorded the deployment id as target in selective runs | Use `operationTargetID(definition, selection)`, never the raw selection |
| Phase rollback restored a fixed boundary | Boundaries come from the store's declared phases (`phasePlanForStore`), as reservation cancellation already did |
| Revert-all ignored failed stores with no progress rows | A store belongs to a run if it is failed or in progress, not only if it has progress rows |
| `CheckDeployment` skipped the route search | Every gate entry point fills the same report fields |
| Selection comparison used a nil sentinel | Use an explicit "loaded" flag, because empty selections decode to nil |
| Duplicate query ids | A package level test guards uniqueness |
| `migration initialize` raced with itself | Every write command takes the execution lock |

## Known open items

- Session advisory lock release relies on the driver closing a broken connection. A failed unlock can in
  theory return a connection that still holds the lock to the pool.
- `UpdateProgress` has no status or checkpoint guard. It is safe only while the advisory lock holds.
- Per deployment gating at `expanded` admits the new binary on every deployment.
- Restricted mode is configuration, and the startup gate enforces it only when
  `migration.startup_compatibility_check` is enabled.

Check these before changing the areas they touch.
