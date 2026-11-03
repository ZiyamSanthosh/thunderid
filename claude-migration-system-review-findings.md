# Claude Review: Migration System Implementation Findings

Reviewer: Claude (code review of the in-progress migration system on branch `migration-plan`).
Scope: `backend/internal/system/migration/**`, `backend/cmd/server/migration_cmd.go`, `backend/cmd/server/main.go`, the v2.0.0 release operations, and the metadata schema scripts.
Method: findings come from reading the code, not from the design documents. `go vet` and the unit tests for the migration packages and `cmd/server` pass. The Postgres integration suite under `backend/tests/migration` was **not** run, so every finding is code-reading based. Confidence is stated per finding.

## Review history

| Round | Date | What was reviewed |
|---|---|---|
| 1 | 2026-10-12 | Initial review of engine, coordinator, rollback v1, v2.0.0 operations |
| 2 | 2026-10-12 | Reworked rollback (failed-operation, phase and revert-all scopes, `MIGRATION_FAILURE` table) |
| 3 | 2026-10-12 | Re-read of the rollback, state-controller and metadata code after further changes. `go vet` clean, unit tests pass, integration suite not run. |
| 4 | 2026-10-12 | Committed rollback lifecycle (`c5e0a474b`) plus uncommitted follow-ups, the new integration tests, `migration-engine-handoff.md`, and a first look at restricted operating mode. `go vet` (including the `migration_integration` build tag) and unit tests pass. Integration suite not run. |
| 5 | 2026-10-12 | Amended rollback commit (`5f90a415e`): new `rolling_back` stage, rollback-aware resume, explicit rollback errors, resumable revert-all, phase rollback over every participating store, and the larger integration test file. `go vet` (including the `migration_integration` tag) and unit tests pass. Integration suite not run. |
| 6 | 2026-10-12 | Re-amended commit (`c24ca9604`): rollback completion now does its cleanup inside the state-transition transaction. `go vet` (including the `migration_integration` tag) and unit tests pass. Integration suite not run. |
| 7 | 2026-10-12 | Metadata rewrite (`cd9f1caa3`): `MIGRATION_RUN_TARGET` and `MIGRATION_FAILURE` replaced by one `MIGRATION_RUN` table, run mode persisted, failure written atomically with the failed state, run row cleaned up on completion. `go vet` (including the `migration_integration` tag) and unit tests pass. Integration suite not run. |
| 8 (latest) | 2026-10-13 | Amended commit (`78c3c0148`): per-store startup gate, every changed store now passes through a contract phase, dead-code removal. `go vet` (including the `migration_integration` tag) and unit tests pass. Integration suite not run. |

### Round 3: what changed since round 2
Verified improvements:
- `revertAll` now rejects forward progress rows whose status is neither `completed` nor `in_progress` before building candidates ([rollback.go:487](backend/internal/system/migration/engine/rollback.go#L487)).
- The scope handling in `rollback` is tidied: the result is built before the phase check, and `result.Scope` is set when `--phase` is given ([rollback.go:83](backend/internal/system/migration/engine/rollback.go#L83)).
- `BeginRevertAll` no longer uses an inline closure to collect store IDs (cosmetic).
- New unit tests in `rollback_test.go` cover `failedOperationCandidates` (recorded target, single in-progress fallback, ambiguity, compensated failure). `state_controller_test.go` has tests for `CompleteOperationRollback`, `BeginRevertAll` and `CompleteRevertAll`.

Not changed (still open exactly as described below): the stage overload for revert-all, the revert-all reset limited to stores with rollback candidates, the retry-with-`nil`-progress behavior, the split cleanup transactions, `markFailed`, the compatibility mapping for `contracting`, the five-table metadata schema and the v2.0.0 operations (05 and 08 are untouched). I found no change to `compatibility.go`, `preflight.go`, `resumable_executor.go` or the release operations. The `MIGRATION_*` tables and their DDL are unchanged.

Net assessment: round 3 is a small increment. The structural issues from F1 to F7 and F24 remain, so the status columns below are still accurate.

### Round 4: what changed since round 3
Verified improvements:
- **F2 is fixed.** `revertAll` now resets every changed store that has any forward progress, not only stores that have a rollback candidate (`storeHasProgress`, [rollback.go:491](backend/internal/system/migration/engine/rollback.go#L491)). It returns `ErrNoMigrationToRollback` only when no store has progress. The new integration test asserts that entitydb and runtime_persistent end at `1.0.0:complete:clean` after `rollback --revert-all`. Caveat: schema changes made by expand operations without a rollback operation stay applied. That is acceptable only because they are additive, and it should be stated in the operator documentation.
- **Candidate ordering is shared and tested.** `sortRollbackCandidates` ([rollback.go:408](backend/internal/system/migration/engine/rollback.go#L408)) is now applied to the failed-run path as well as revert-all (reverse operation order, then target ID descending), with a unit test.
- **F6 is partially mitigated for selective runs.** The deployment-specific phase rollback test shows that a handle in an unselected deployment (`deployment-c`) survives. The rollback operation itself is unchanged and still clears every non-null handle inside the targeted scope, so a store-wide rollback still wipes handles that did not come from the backfill.
- **F8 and F9 are covered for the phase-scope happy path.** The phase rollback tests assert that progress, failure rows and deployment states are gone afterwards. The crash window between `CompleteRollback` and the cleanup calls is not tested and is unchanged.
- **F26 is partly addressed.** There are now integration tests for phase rollback, deployment-specific phase rollback, revert-all, revert-all blocked by an uncompensated contract operation, and resuming interrupted compensation with the default rollback (see F27 for their limits).

Not changed in round 4: F1, F3, F4, F5, F7, F10 to F25 and F24. In particular:
- `revertAll` still passes `nil` progress and still borrows the `contracting` stage, so an interrupted revert-all still cannot be retried (F3) and the new binary is still not fenced during it (F4). The new interrupted-compensation test covers only the default rollback scope, not revert-all.
- The metadata schema still has five tables (F24). The handoff tells the next person to recreate the proof-of-concept databases because `MIGRATION_FAILURE` was added, which is exactly the missing in-place upgrade path from F14.
- The handoff states "the rollback architecture has already been decided". F24 and the run-record proposal are therefore a decision the owner has to make explicitly, not a defect fix.

### Round 5: what changed since round 4
The rollback commit was amended (`5f90a415e`, previously `c5e0a474b`). Verified improvements:
- **F3 (graceful failure case) and F4 are largely fixed.** Revert-all now uses its own stage, `rolling_back` ([metadata.go](backend/internal/system/migration/engine/metadata.go)), instead of borrowing `contracting`. The gate rejects the target binary in that stage with `rollback_in_progress` and allows the source binary ([compatibility.go:241](backend/internal/system/migration/engine/compatibility.go#L241), [compatibility.go:256](backend/internal/system/migration/engine/compatibility.go#L256)). A failed revert-all is recognised by its failure row (direction `rollback`) and re-run under the same run ID, with in-progress rollback progress handed back to the executor ([rollback.go](backend/internal/system/migration/engine/rollback.go), `revertAll` and `interruptedRevertAllRunID`). Remaining limit: the gate only runs at startup, so target pods that are already running are not stopped (F17).
- **F10 is fixed.** `resume` returns `ErrMigrationRollbackInterrupted` when a store is in `rolling_back`, or when the failure record says the last failure was a rollback ([coordinator.go](backend/internal/system/migration/engine/coordinator.go), `rejectForwardResumeDuringRollback`).
- **F13 is fixed.** Failed-operation rollback now returns `ErrRollbackUnavailable` or `ErrRollbackAlreadyCompleted` instead of a silent success.
- **F7 is mostly fixed.** Phase rollback includes any store that has progress in the phase (`hasPhaseProgress`), not only stores whose operations have a rollback operation, so a store that finished its phase is reset together with the failed one. A new integration test covers an expand rollback across several stores. Residual edge: a store whose phase progress was entirely reused from an earlier run (reusable schema progress) has no progress row for this run and is still skipped.
- **Terminology** moved from "compensation" to "rollback" (`RolledBack`, `ROLLED_BACK` column). The DDL in all five places was updated consistently. The rollback operation file was renamed to `05_backfill_entity_type_handle_rollback.go` and its description now reads "Remove the entity-type handle representation before returning to the source version", which describes its real behavior (F6) more honestly.
- **F26 is further addressed.** The integration file has grown to include an expand rollback across stores, a revert-all resume, and a resume refusal after an interrupted rollback.

Not changed in round 5: F1, F5, F6 (the operation still clears every non-null handle in scope), F8, F9, F11, F12, F14, F15 to F25, F27 (tests still seed failure state with raw SQL and have not been run), F28 and F29.
- F28 specifically: [migration-engine-handoff.md](migration-engine-handoff.md) still says "compensation", "compensates" and "clears handles populated by the backfill", which no longer matches the code's vocabulary or behavior.
- F24: still five tables. The new `ROLLED_BACK` column was added to the existing failure table, which again changes the metadata schema without an upgrade path (F14).

### Round 6: what changed since round 5
The commit was amended again (`c24ca9604`). Only three files changed, all around rollback completion:
- **F9 is mostly fixed.**
  - `CompleteRollback` ([state_controller.go:490](backend/internal/system/migration/engine/state_controller.go#L490)) now deletes the run's progress, the deployment-state markers (all markers for a store-wide run, or only the selected deployments) and the failure row in the same transaction as the state transition. The coordinator no longer does that cleanup afterwards.
  - `CompleteOperationRollback` now takes the recorded failure, checks that it matches the run, state, operation and target, and writes the "rolled back" failure update in the same transaction as the state change.
  - The crash windows I described for phase and failed-operation rollback completion are closed.
  - Still open: `failRun` writes the failure row and the failed state in two separate steps, and the failed-operation scope still never deletes deployment markers (F8).
- Unit tests in `state_controller_test.go` were extended for the new behavior.

Not changed in round 6: F1, F3 (crash case, F30), F5, F6, F8, F11, F12, F14, F15 to F25, F27 to F29, F31. The metadata schema is still five tables (F24).

### Round 7: what changed since round 6
The commit was amended again (`cd9f1caa3`), mainly rewriting the migration metadata. The schema is now four tables: `MIGRATION_STATE`, `MIGRATION_PROGRESS`, `MIGRATION_DEPLOYMENT_STATE` and `MIGRATION_RUN` (see [postgres_metadata.sql](backend/internal/system/migration/engine/postgres_metadata.sql)). All four `dbscripts/*/postgres.sql` copies carry an identical definition.

Verified improvements:
- **F24 is mostly done.** `MIGRATION_RUN_TARGET` and `MIGRATION_FAILURE` are merged into `MIGRATION_RUN`: run ID, mode (`forward`, `rollback`, `revert_all`), phase, rollback scope, selection, and an optional failure (store, forward and current operation, target, direction, rolled-back flag, summary). This follows the "run record" half of my proposal. The failure lives in the run row rather than on the state row, which is an acceptable variation. The setup script for the manual proof of concept now verifies the four-table schema and rejects leftovers of the old tables.
- **F9 is fully fixed.** `failRun` no longer writes a separate failure row. `MarkFailedWithFailure` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)) records the failure in the run row in the same transaction that marks the stores failed.
- **F18 is fixed.** The run row is deleted when no store in the namespace references it (`DeleteRunIfInactive`) on finalization, cancellation, rollback completion and revert-all completion. `resume` clears the failure and resets the mode inside the same transaction as the state change.
- **F30 (and F3) are fixed.** `BeginRevertAll` persists `mode = revert_all` in the same transaction as the state change, and `revertAll` now recognises an interrupted run from `rolling_back` plus either `failed` or `in_progress` status ([rollback.go](backend/internal/system/migration/engine/rollback.go), `interruptedRevertAllRunID`). A Ctrl+C or crash mid-revert-all therefore reuses the same run ID and adopts the in-progress rollback progress.
- **F11 is fixed.** After a store finalizes, the coordinator removes it from the reserved or active set (`delete(reservedStores, ...)`, `delete(resume.activeStores, ...)`), so `markFailed` no longer touches finalized stores.
- **F10 is strengthened.** `rejectForwardResumeDuringRollback` now also looks at the run mode, not only the failure direction (but see F33).
- Rollback begin and completion now verify the run mode and scope (`BeginRollbackScope`, `CompleteOperationRollback`).

Not changed in round 7: F1, F5, F6, F8, F12, F14 (no in-place metadata upgrade, and `InspectSchema` still requires all four tables), F15 to F17, F19 to F23, F25, F27 to F29, F31.
- F8: `CompleteOperationRollback` still does not delete deployment markers for a store-wide failed-operation rollback, even though the run row now knows the selection.
- F27: the integration tests still seed state with SQL, and they now also insert `MIGRATION_RUN` rows by hand (`insertMigrationRun`). See F32 for why that matters.
- F31: `phaseForExecutionStage` still has no case for `rolling_back`.

### Round 8: what changed since round 7
The commit was amended again (`78c3c0148`).

Verified improvements:
- **Every changed store now ends through the contract phase.** `phasePlanForStore` appends an implicit contract phase to any store whose last declared phase is not contract, and the coordinator finalizes reserved stores that have no operations in the current phase (new loops at the end of `applyPhase` and `resume`; `finalizationRequirements` accepts an empty contract). Expand-only stores (entitydb, runtime_persistent) therefore stop at `contract_ready` after expand (the updated `resume_test.go` asserts `1.0.0:contract_ready:completed`) and only become `clean` at 2.0.0 when `apply contract` runs. This fixes a problem I had not reported: previously those stores became `clean` at 2.0.0 immediately after expand, so a restarting old binary hit `LevelMismatch` on them for the rest of the migration. Now the old binary can restart on every store until contract starts, and the target binary becomes ready on all stores together.
- **F16 is partially fixed.** The startup gate no longer switches itself off when any store uses an unsupported provider. `startupMigrationNamespaces` checks the supported stores and marks the rest `Skipped` with reason `unsupported_provider` ([migration_cmd.go](backend/cmd/server/migration_cmd.go), [compatibility.go](backend/internal/system/migration/engine/compatibility.go)); the aggregate `Compatible()` ignores skipped stores but requires at least one checked store. The startup log reports skipped stores. Remaining gaps: a skipped (for example SQLite) store has no protection and no migration path, and when every store is unsupported the gate still returns nothing and only warns.
- **Dead code removed:** `Apply`, `Reserve`, `Finalize`, `UpdateState` (an unconditional state writer), and the unused readiness and preflight `Check` helpers. Removing `UpdateState` also removes a way to bypass the conditional transitions.
- `storeCompletedPhase` now only treats "phase not declared" as benign, and returns other errors.

Not changed in round 8: F1, F5, F6, F8, F12, F14, F15, F17, F19 to F25, F27 to F29, F31 to F34. In particular **F32 is still open**: `DeleteRunIfInactive` still deletes the run row of a namespace whose stores have all finished the phase, and `BeginRollbackScope` still requires the run row. With the new lifecycle this affects the same stores as before (an expand-only store now finishes at `contract_ready` and has no run row either).
- F27: `resume_test.go` still builds its failure state with SQL and now also inserts `MIGRATION_RUN` rows by hand (`INSERT ... ON CONFLICT DO NOTHING`).

Severity: **High** = data loss, wedged state, or an outage risk. **Medium** = incorrect or misleading behavior with a workaround. **Low** = hygiene.

Status column values: **Open**, **Partially fixed**, **New** (introduced or exposed by the rollback rework).

## Summary table

| ID | Severity | Area | Title | Status |
|---|---|---|---|---|
| F1 | High | Backfill | No catch-up for rows written by the old binary during migrate | Open |
| F2 | High | Rollback | `--revert-all` does not revert every store | Fixed (round 4) |
| F3 | High | Rollback | An interrupted revert-all cannot be retried or resumed | Fixed (round 7) |
| F4 | High | Rollback | New binary is not fenced during revert-all | Mostly fixed (round 5) |
| F5 | High | Progress | Re-running migrate for an already-migrated deployment wedges the store | Open |
| F6 | High | Rollback | Rollback operation clears every handle, not only the backfilled ones | Partially mitigated (round 4) |
| F7 | High | Rollback | Phase rollback skips stores without rollback progress, leaving mixed boundaries | Mostly fixed (round 5) |
| F8 | Medium | Rollback | Failed-operation rollback of a store-wide backfill leaves deployment markers | Partially fixed |
| F9 | Medium | Rollback | Rollback cleanup is split across transactions | Fixed (round 7) |
| F10 | Medium | Resume | `resume` ignores the failure record and its direction | Fixed (round 5) |
| F11 | Medium | Failure handling | `markFailed` aborts when a store already finalized in the same namespace | Fixed (round 7) |
| F12 | Medium | Compatibility | A failed contract blocks the old binary even though nothing changed | Open |
| F13 | Medium | Rollback | Failed-operation rollback can silently do nothing | Fixed (round 5) |
| F14 | Medium | Metadata | Metadata schema is duplicated five times and has no in-place upgrade path | New |
| F15 | Medium | Coordinator | Cross-namespace reservation is not atomic | Open |
| F16 | Medium | Platform | Postgres only, and the startup gate silently turns off elsewhere | Partially fixed (round 8) |
| F17 | Medium | Contract | No check that old binaries have drained before contract | Open |
| F18 | Medium | Metadata | Failure rows and run-target rows are never cleaned up | Fixed (round 7) |
| F19 | Low | Initializer | Absent metadata schema is recorded as 1.0.0 even on a fresh 2.0.0 install | Open |
| F20 | Low | Security hygiene | Credentials in `backend/cmd/server/deployment.yaml` | Open |
| F21 | Low | Engine | `time.Time` compared with `!=` / exact `UPDATED_AT` match | Open |
| F22 | Low | Locking | Session advisory locks break behind transaction-pooling proxies | Open |
| F23 | Low | Misc | Smaller items (see end) | Open |
| F24 | Medium | Metadata | Five metadata tables should be reduced (merge failure into state, replace run targets with a run record) | Mostly done (round 7) |
| F25 | Medium | Rollback | Revert-all ignores the current state of the stores it overwrites | New (round 3) |
| F26 | Medium | Tests | Revert-all, phase rollback and the failure paths have no integration test | Partially fixed (round 4) |
| F27 | Medium | Tests | New integration tests seed failure state with raw SQL and have not been run | New (round 4) |
| F28 | Low | Docs | Handoff document overstates what the rollback operation does | New (round 4) |
| F29 | Medium | Restricted mode | Restricted operating mode is static configuration, not tied to migration state | New (round 4) |
| F30 | High | Rollback | Ctrl+C or a crash during revert-all still wedges it (stale in-progress rollback row, no failure record) | Fixed (round 7) |
| F31 | Low | Rollback | Default `rollback` rejects the new `rolling_back` stage with an unhelpful error | New (round 5) |
| F32 | High | Rollback | Phase rollback fails when a namespace's run row was already deleted at finalization | New (round 7) |
| F33 | Medium | Resume | Resume rejects an interrupted rollback only for `failed` stores, not `in_progress` ones | New (round 7) |
| F34 | Low | Failure handling | `MarkFailed` errors on already-finalized namespaces because it loads the run first | New (round 7) |
| F35 | Low | Docs | Lifecycle docs and diagrams still describe expand-only stores finalizing straight to Clean | New (round 8) |

---

## High

### F1. No catch-up for rows written by the old binary during migrate
Confidence: high.

- The old binary keeps serving while the store is `migrating` ([compatibility.go:237](backend/internal/system/migration/engine/compatibility.go#L237)), and it inserts entity types with `HANDLE NULL`.
- The backfill pages with `ID > lastID` ([05_backfill_entity_type_handle.go:31](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go#L31)). Rows that appear behind the cursor are never visited. Renames made by the old binary after the backfill are never re-synced either.
- `PostVerify` then reports that some handles still need backfilling and the operation fails. On resume the executor continues from the saved checkpoint ([resumable_executor.go:209](backend/internal/system/migration/engine/resumable_executor.go#L209)) and never rescans, so it fails the same way again.
- Rows the old binary writes after migrate completes survive until contract. `SET NOT NULL` in [07_finalize_entity_type_handle.go:27](backend/internal/system/migration/releases/v2_0_0/07_finalize_entity_type_handle.go#L27) then fails, and `requestedPhaseCompleted` stops migrate from being re-run once the store is `contract_ready` ([preflight.go:748](backend/internal/system/migration/engine/preflight.go#L748)).

Fix direction: give the engine a catch-up concept. Options: a trigger or generated default installed in expand, a final re-scan that runs inside contract before the constraint, or a resumable operation that restarts its scan from the beginning when post-verification fails.

### F2. `--revert-all` does not revert every store
Confidence: high. **Status: fixed in round 4** (see "Round 4: what changed"). The description below is kept for history.

[rollback.go:505](backend/internal/system/migration/engine/rollback.go#L505) only resets stores that have a rollback candidate. In v2.0.0 only configdb has a rollback operation. After revert-all, configdb is `clean` at 1.0.0 while entitydb and runtime_persistent remain `clean` at 2.0.0. The old binary requires 1.0.0 for every store, so it fails with `LevelMismatch` and cannot start. The system ends in a mixed-level state that no binary accepts.

Fix direction: either reset all changed stores (after confirming the expand-only schema changes are backward compatible), or refuse revert-all unless every changed store has a defined revert path.

### F3. An interrupted revert-all cannot be retried or resumed
Confidence: medium-high (not executed).

- `BeginRevertAll` reuses `MigrationStageContracting` as its executing stage ([state_controller.go](backend/internal/system/migration/engine/state_controller.go), `BeginRevertAll`). A failed revert-all therefore looks like a failed contract run.
- Re-running revert-all always passes `nil` progress to the executor ([rollback.go:533](backend/internal/system/migration/engine/rollback.go#L533)), so the executor inserts a new progress row. The stale in-progress row from the failed run collides with the `UNIQUE (MIGRATION_STATE_ID, TARGET_LEVEL, OPERATION_ID, EXECUTION_TARGET_ID)` key ([postgres_metadata.sql:27](backend/internal/system/migration/engine/postgres_metadata.sql#L27)).
- `resume` rejects the rollback operation IDs ("does not match the plan"). Default rollback does nothing, because it only considers forward operations of the contract phase.

Net effect: no supported recovery path, only manual SQL.

Fix direction: persist the run mode and give revert-all its own stage or status. Let revert-all resume from existing rollback progress.

### F4. New binary is not fenced during revert-all
Confidence: high.

While revert-all runs the stage is `contracting`. The compatibility gate treats that as serving-as-target ([compatibility.go:252](backend/internal/system/migration/engine/compatibility.go#L252)), and the gate only runs at startup. Live 2.0.0 pods keep serving while entity-type handles are wiped, including handles users created after the migration. The command also has no confirmation prompt or `--force` flag.

Fix direction: a distinct "reverting" stage that the gate rejects for both binaries, an explicit confirmation flag, and a runtime re-check or drain requirement.

### F5. Re-running migrate for an already-migrated deployment wedges the store
Confidence: medium-high (needs a test).

`MIGRATION_PROGRESS` is unique without `RUN_ID`. Only schema-change operations reuse existing progress ([atomic_executor.go:115](backend/internal/system/migration/engine/atomic_executor.go#L115)); the resumable executor always inserts ([resumable_executor.go:89](backend/internal/system/migration/engine/resumable_executor.go#L89)). A second `apply migrate --deployment-id A` hits a unique violation and leaves the store `failed`. Resume repeats the failure, and rollback is the only exit, which then clears A's handles (F6). The selective integration test never repeats a deployment ([deployment_selective_test.go](backend/tests/migration/releases/v2_0_0/deployment_selective_test.go)).

Fix direction: treat an existing completed progress row for the same target as "already done" (skip), or include the run in the uniqueness rule and reconcile through verification.

### F6. Rollback operation clears every handle, not only the backfilled ones
Confidence: high.

[08_rollback_entity_type_handle_backfill.go:18](backend/internal/system/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill.go#L18) selects `HANDLE IS NOT NULL` and clears all of them. Anything that did not come from this run is lost (handles created by the new binary, or by a previous selective run). The default failed-operation scope reduces the blast radius, but the operation itself is still blunt.

Fix direction: record which rows the backfill touched (checkpoint list, marker column, or a side table) and compensate only those.

### F7. Phase rollback skips stores without rollback progress
Confidence: medium (reasoned, not reproduced).

[rollback.go:324](backend/internal/system/migration/engine/rollback.go#L324) skips stores that have no rollback-relevant progress and no active run. If store A finished its phase and store B failed, B returns to the earlier boundary while A stays ahead. Re-running the phase is then blocked, because `validatePhaseBoundary` requires every store to be at the previous boundary. Operations without a rollback operation (01 to 04, 06) also "succeed" at rollback by resetting state while their schema change stays applied. That is harmless only because those operations are idempotent, and nothing checks or reports it.

---

## Medium

### F8. Failed-operation rollback of a store-wide backfill leaves deployment markers
Confidence: medium-high. Status: partially fixed (phase scope and revert-all now delete deployment state).

The failed-operation scope runs the rollback operation for target `""`, which clears all handles in the store, but it does not delete any `MIGRATION_DEPLOYMENT_STATE` rows. If an earlier selective run migrated deployment A, A's marker remains while A's handles are gone, so `CheckDeployment` ([compatibility.go:138](backend/internal/system/migration/engine/compatibility.go#L138)) lets the new binary start against `NULL` handles.

### F9. Rollback cleanup is split across transactions
Confidence: high.

In phase scope, `CompleteRollback` commits first. Deployment-state deletion and `DeleteFailure` then run as separate calls ([rollback.go:183](backend/internal/system/migration/engine/rollback.go#L183) to [212](backend/internal/system/migration/engine/rollback.go#L212)). A crash in between leaves the store rolled back with stale deployment markers (the F8 effect) and a stale failure row. In failed-operation scope, `UpsertFailure` also runs after the state has been changed ([rollback.go:170](backend/internal/system/migration/engine/rollback.go#L170)).

Fix direction: do the state transition, progress deletion, deployment-state deletion, and failure update in one transaction per namespace.

### F10. `resume` ignores the failure record and its direction
Confidence: high.

`MigrationFailure.ExecutionDirection` is recorded but never read by `resume` (grep shows it is only written and read in rollback). After a failed rollback-direction operation the state is `failed` with the same run, so `resume` would continue the forward operation over a half-compensated state. The `DeleteFailure` comment says it is used "after a run is resumed", but resume never calls it.

Fix direction: `resume` must read the failure, refuse or finish compensation when the last failure was a rollback, and delete or mark the failure once forward progress succeeds.

### F11. `markFailed` aborts when a store already finalized in the same namespace
Confidence: medium (reasoned from [state_controller.go:302](backend/internal/system/migration/engine/state_controller.go#L302) and [coordinator.go](backend/internal/system/migration/engine/coordinator.go) `markFailed`/`failRun`).

`MarkFailed` skips `clean` stores but returns an error for a store whose phase already finalized to `completed` (no active run). `applyPhase` and `failRun` pass every reserved store, including already finalized ones. In a shared-database setup (for example configdb finalized after expand, then an entitydb operation fails) the whole namespace transaction errors, the failing store is never marked `failed`, and the real error is wrapped with a confusing one. The store stays `in_progress`, which resume still accepts, so the effect is a lost failure summary and a misleading error.

### F12. A failed contract blocks the old binary even though nothing changed
Confidence: high.

`contracting` maps to `SourceRetired` regardless of status ([compatibility.go:241](backend/internal/system/migration/engine/compatibility.go#L241)). A failed atomic contract operation rolls back its transaction and changes nothing, but old pods that restart now fail to start. Failure in `contracting` should keep the old binary compatible until a contract operation has actually committed.

### F13. Failed-operation rollback can silently do nothing
Confidence: high.

If the failed operation has no rollback operation (atomic schema operations 01 to 04, 06, 07), `failedOperationCandidates` returns no candidates and `rollback` returns success ([rollback.go:100](backend/internal/system/migration/engine/rollback.go#L100)). The store stays `failed` and the CLI prints a success-style result. It should say "nothing to compensate, fix the cause and resume".

### F14. Metadata schema is duplicated five times and has no in-place upgrade path
Confidence: high.

The same DDL now exists in [postgres_metadata.sql](backend/internal/system/migration/engine/postgres_metadata.sql) and in each of `backend/dbscripts/{configdb,entitydb,runtime_persistent,runtime_transient}/postgres.sql`. `InspectSchema` requires all five tables ([metadata_schema.go:73](backend/internal/system/migration/engine/metadata_schema.go#L73)). Adding `MIGRATION_FAILURE` already turns any database initialized without it into `SchemaPartial`; `Initialize` then answers "partial metadata schema requires repair" ([metadata_initializer.go:71](backend/internal/system/migration/engine/metadata_initializer.go#L71)) and the startup gate treats the store as incompatible. After the first release, any new metadata table would be a startup outage for existing installs. A repair or metadata-upgrade path (keyed on `migrationMetadataVersion`) and a single source of truth for the DDL are needed before release.

### F15. Cross-namespace reservation is not atomic
Confidence: medium.

[coordinator.go:319](backend/internal/system/migration/engine/coordinator.go#L319) reserves namespaces one after another. A crash between them leaves store A `in_progress` and store B `clean` at the source level. `resume` then fails ([coordinator.go:668](backend/internal/system/migration/engine/coordinator.go#L668)), because B is "not complete and not owned by the run". Recovery works only through rollback.

### F16. Postgres only, and the startup gate silently turns off elsewhere
Confidence: high.

`supportedMigrationDatabaseTypes` contains only Postgres ([metadata_store.go](backend/internal/system/migration/engine/metadata_store.go)). SQLite is the default in [deployment.yaml](backend/cmd/server/deployment.yaml), and `startupCompatibilityCheckSupported` returns false for it, so [main.go:95](backend/cmd/server/main.go#L95) only logs a warning. SQLite upgrades get no migration and no compatibility protection. If that is intended it should be explicit (fail or document), not a warning.

### F17. No check that old binaries have drained before contract
Confidence: high.

`ApplyContract` runs whenever the store is `contract_ready`. Nothing verifies that old pods are gone, and the gate only runs at startup. Contract can drop structures a still-running old pod uses. The only safeguard is operator discipline. Options: explicit confirmation flag, minimum age since `contract_ready`, or a live-binary registry.

### F18. Failure rows and run-target rows are never cleaned up
Confidence: high. Status: partially fixed (phase rollback and revert-all delete failure rows).

`MIGRATION_FAILURE` rows survive a successful resume. `MIGRATION_RUN_TARGET` rows are never deleted anywhere. `MIGRATION_PROGRESS` is pruned only by rollback. All three grow without bound.

---

## Low

### F19. Absent metadata schema is recorded as 1.0.0
[metadata_initializer.go:75](backend/internal/system/migration/engine/metadata_initializer.go#L75) assigns `preFrameworkBaselineLevel` when the schema is absent. On a fresh 2.0.0 install `initialize` therefore records 1.0.0 and reports a migration that is not needed. It converges only because every operation's pre-verification recognizes the target state and skips.

### F20. Credentials in `deployment.yaml`
The working-tree diff of [backend/cmd/server/deployment.yaml](backend/cmd/server/deployment.yaml) adds an SMTP host, username and password, plus an unrelated `gate_client` block. Keep these out of the migration commit and rotate the credentials if they were real.

### F21. Time equality
`*state != expected` compares `time.Time` structs ([state_controller.go:366](backend/internal/system/migration/engine/state_controller.go#L366)), and `ResumeState` matches `UPDATED_AT` exactly. It works because both sides come from the same driver and location. It is brittle (location pointer, sub-microsecond truncation).

### F22. Session advisory locks
[postgres_execution_lock.go](backend/internal/system/migration/engine/postgres_execution_lock.go) uses session-level `pg_try_advisory_lock`. That breaks behind transaction-pooling proxies such as PgBouncer. Document it or detect it.

### F23. Smaller items
- `usedHandles` is reloaded for every batch of the backfill (quadratic at scale), [05_backfill_entity_type_handle.go:110](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go#L110).
- `ADD CONSTRAINT UNIQUE` (06) and `SET NOT NULL` (07) hold heavy locks. `lock_timeout` only bounds waiting for the lock, not the build time. Fine for a small table.
- Failure recording is two separate writes (`UpsertFailure`, then `markFailed`). If the second fails, a failure record exists that the state does not show.
- `--phase` parsing for `rollback` is more permissive than the usage string; check combinations of `--phase` and a positional phase.
- Revert-all with no candidates returns `ErrNoMigrationToRollback` even when stores still need resetting.
- `rollback` and `revertAll` call `c.namespaceIndexes()` inside loops (cheap but repeated).

---

## F24. Metadata table reduction (proposal for the implementing model to verify and implement)

Confidence: medium-high that the reduction is worthwhile. The exact column set is a proposal and should be validated against the code before implementing.
Timing: the metadata schema can still change freely before the first release. After release every change needs an upgrade path (see F14), so decide this now.

### Problem
The engine keeps five tables per physical database: `MIGRATION_STATE`, `MIGRATION_PROGRESS`, `MIGRATION_DEPLOYMENT_STATE`, `MIGRATION_RUN_TARGET`, `MIGRATION_FAILURE` ([postgres_metadata.sql](backend/internal/system/migration/engine/postgres_metadata.sql)).
- `InspectSchema` requires all five ([metadata_schema.go:73](backend/internal/system/migration/engine/metadata_schema.go#L73)), so adding or removing any table is an all-or-nothing compatibility event.
- The DDL is duplicated in five files (F14).
- Two tables are written in separate statements from the state change, which creates crash windows (F9, F10, F18):
  - `failRun` upserts the `MIGRATION_FAILURE` row first, then calls `markFailed` ([coordinator.go](backend/internal/system/migration/engine/coordinator.go), `failRun`).
  - Rollback deletes failure rows and deployment states after `CompleteRollback` has committed ([rollback.go:183](backend/internal/system/migration/engine/rollback.go#L183) to [212](backend/internal/system/migration/engine/rollback.go#L212)).
- `MIGRATION_FAILURE` and `MIGRATION_RUN_TARGET` have no lifecycle owner and are never cleaned up after success (F18).

### Which tables stay
| Table | Decision | Reason |
|---|---|---|
| `MIGRATION_STATE` | Keep | Core, one row per store, read by the compatibility gate |
| `MIGRATION_PROGRESS` | Keep | Many rows per store, carries resumable checkpoints |
| `MIGRATION_DEPLOYMENT_STATE` | Keep | Different cardinality (per deployment) and outlives a run. It exists only for selective migration (`--deployment-id`). If that feature is dropped, drop this table too. |

### Change 1: merge `MIGRATION_FAILURE` into `MIGRATION_STATE`
Rationale: a store has at most one active run, and failure identity matters only while `STATUS = 'failed'`. A run stops at its first failing operation, so one failure per run per store is enough.

Proposed nullable columns on `MIGRATION_STATE`:
- `FAILED_OPERATION_ID` (forward operation that failed)
- `FAILED_CURRENT_OPERATION_ID` (operation that was executing, differs from the forward one during rollback)
- `FAILED_TARGET_ID` (execution target, empty string for complete store)
- `FAILED_DIRECTION` (`forward` or `rollback`)
- `FAILED_COMPENSATED` (boolean)

Existing `FAILURE_SUMMARY` stays and remains the human-readable text.

Required behavior:
1. The failure columns are written in the **same conditional UPDATE** that sets `STATUS = failed` (extend `FailState`, [metadata_store.go](backend/internal/system/migration/engine/metadata_store.go)). No separate upsert.
2. They are cleared by every transition that leaves `failed` (resume, finalize, cancel, complete rollback, revert-all) in the same UPDATE.
3. `State.Validate` ([metadata.go](backend/internal/system/migration/engine/metadata.go)) must require the failure columns when `STATUS = failed` and forbid them otherwise.
4. `resume` must read the direction from the state row and refuse (or finish compensation) when the last failure was a rollback-direction failure (fixes F10).
5. `failedOperationCandidates` ([rollback.go:406](backend/internal/system/migration/engine/rollback.go#L406)) reads the failure from the state row instead of calling `GetFailure`.

Code to remove or rewrite: `MigrationFailure` type, `UpsertFailure`, `GetFailure`, `DeleteFailure`, `DeleteFailuresForState`, the failure queries (`MIG-METADATA_MGT-25` and neighbours), `metadataFailureTable` in [metadata_schema.go](backend/internal/system/migration/engine/metadata_schema.go), the `operationFailure` plumbing in `failRun`, and the `DeleteFailuresForState` call in `CompleteRevertAll` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)).

### Change 2: replace `MIGRATION_RUN_TARGET` with a run record
Rationale: the deployment selection is a property of a run. It cannot be derived from `MIGRATION_PROGRESS`, because targets that have not started yet have no progress row.

Proposed table `MIGRATION_RUN` (one row per run, per namespace):
- `RUN_ID` (primary key)
- `MODE` (`forward`, `rollback`, `revert_all`)
- `PHASE` (`expand`, `migrate`, `contract`)
- `SELECTION` (JSON array of deployment IDs, empty for complete store)
- `UPDATED_AT`

Required behavior:
1. Written in the same transaction as the store reservation (`ReservePhase`, `BeginRollback`, `BeginRevertAll`).
2. `resume` and `rollback` read the mode, phase and selection from this row first, instead of inferring them from stage, progress and failure rows. This removes the need to borrow `MigrationStageContracting` for revert-all (F3, F4): revert-all gets `MODE = revert_all`, and the compatibility gate can reject both binaries while it runs.
3. Rows are deleted when the run finalizes or the rollback completes (fixes the run-target half of F18).

Code to remove or rewrite: `RecordRunTargets`, `ListRunTargets`, `loadResumeSelection` ([coordinator.go](backend/internal/system/migration/engine/coordinator.go)), `metadataRunTargetTable`, and the corresponding queries.

Minimal alternative if `MIGRATION_RUN` is too large a step now: do Change 1 only and leave `MIGRATION_RUN_TARGET` as is, but add its cleanup on finalize and rollback. That already removes most of the atomicity problems.

### Resulting schema
| Table | Role |
|---|---|
| `MIGRATION_STATE` | Store position, plus failure columns |
| `MIGRATION_PROGRESS` | Per-operation, per-target progress and checkpoints |
| `MIGRATION_DEPLOYMENT_STATE` | Per-deployment completion markers |
| `MIGRATION_RUN` | Run mode, phase and selection |

### Companion changes (do together)
1. **Single source for the DDL.** Generate or verify `backend/dbscripts/{configdb,entitydb,runtime_persistent,runtime_transient}/postgres.sql` and `postgres_metadata.sql` from one definition, or add a test that fails when they drift.
2. **Tolerant schema inspection.** `InspectSchema` should require only `MIGRATION_STATE` and `MIGRATION_PROGRESS`. The remaining tables should be created on demand by a versioned metadata upgrade keyed on `migrationMetadataVersion`, so adding a table later is not a startup outage (F14).
3. **Audit history is out of scope.** If run or failure history is wanted for audit, use a separate append-only table that is never read by control flow.

### Acceptance criteria
1. Every transition into `failed` is a single conditional UPDATE that also writes the failure columns, and every transition out clears them. Verify with a test that injects an error between what used to be two writes.
2. No code path reads a failure or selection from a table other than `MIGRATION_STATE` and `MIGRATION_RUN`.
3. `resume` after a rollback-direction failure does not continue forward silently.
4. Revert-all is distinguishable from contract in persisted state, and the compatibility gate rejects both binaries while it runs.
5. After success, rollback or revert-all, no stale `MIGRATION_RUN` row remains.
6. A database initialized with the older five-table schema is either upgraded or reported with a clear repair instruction, not silently treated as partial.
7. Unit tests under `backend/internal/system/migration/engine` and the Postgres integration tests under `backend/tests/migration` are updated and pass, including the existing rollback and selective-deployment tests.

### Files expected to change
`engine/metadata.go`, `engine/metadata_store.go`, `engine/metadata_schema.go`, `engine/coordinator.go`, `engine/state_controller.go`, `engine/rollback.go`, `engine/postgres_metadata.sql`, the four `backend/dbscripts/*/postgres.sql`, `engine/compatibility.go` (revert-all fencing), and the matching `_test.go` files and migration integration tests.

---

## Round 8 additions

### F35. Lifecycle docs and diagrams still describe expand-only stores finalizing straight to Clean
Confidence: high.

The implicit contract phase (see Round 8) changes what `apply expand` and `apply contract` do for entitydb and runtime_persistent. `apply contract` now also touches stores that have no contract operations (it reserves them and flips them to `clean` with zero operations). Any operator documentation, the handoff, and the lifecycle diagram I produced (`claude-migration-diagrams.excalidraw`, frame 1 shows a dashed "stores with expand only" shortcut straight to Clean 2.0.0) are now wrong. The diagram also predates the `rolling_back` stage and the run record. Update them, and add an operator note that contract is required even for stores whose schema change was expand-only.

Update: `claude-migration-diagrams.excalidraw` was regenerated on 2026-10-13 to match the current code (no expand-only shortcut to Clean, the `rolling_back` stage, the run record, and a new metadata tables frame). The handoff and operator documentation still need the same correction.

---

## Round 7 additions

### F32. Phase rollback fails when a namespace's run row was already deleted at finalization
Confidence: medium-high (reasoned from the code; the integration test hides it).

When a store finishes its phase, `CompletePhaseSelection` calls `DeleteRunIfInactive` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)), which deletes the `MIGRATION_RUN` row of that namespace as soon as no store in it still references the run. With the default configuration each store has its own database, so a store that finishes early (for example runtime_persistent, which only has an expand operation) has no run row in its database after that.

Phase rollback then includes such a store, because since round 5 any store with progress in the phase is included. `BeginRollbackScope` requires the run row in every namespace it touches:
`if run == nil || run.Phase != phase { return ... "active run does not match phase" }`.

Sequence:
1. `apply expand` finishes runtime_persistent (its run row is deleted), then an operation in configdb fails.
2. `rollback --phase expand` begins configdb successfully, then fails on runtime_persistent with "active run does not match phase", and `markFailed` marks the already-begun stores failed.
3. The next attempt fails the same way, because the missing row never reappears.

The new integration test ([05_backfill_entity_type_handle_rollback_test.go:120](backend/tests/migration/releases/v2_0_0/05_backfill_entity_type_handle_rollback_test.go#L120)) inserts a run row in **every** database before calling the command, which is a state a real failure does not produce, so it passes without proving this path.

Fix direction: let rollback create the run row in namespaces that have participating stores but no run row (recreate it from the failed namespace's run, including phase and selection), or stop deleting the run row on finalization until the whole run has finished across namespaces. Add an integration test that produces the failure with a real failing operation.

### F33. Resume rejects an interrupted rollback only for `failed` stores, not `in_progress` ones
Confidence: medium.

`rejectForwardResumeDuringRollback` ([coordinator.go](backend/internal/system/migration/engine/coordinator.go)) iterates `resume.failedByNamespace`, so it only inspects `failed` stores. A phase or failed-operation rollback killed mid-way leaves the store `in_progress` with `run.Mode = rollback`, which the check never examines (revert-all is caught separately by the `rolling_back` stage). Forward `resume` is then stopped only incidentally, when a rollback progress row exists and does not match the plan (`inspectCompletedProgress`). If the interruption happens before the first rollback operation writes progress, forward resume proceeds, which is harmless then. Make the check read the run mode for every active store.

### F34. `MarkFailed` errors on already-finalized namespaces because it loads the run first
Confidence: medium.

`MarkFailedWithFailure` calls `GetRun` before it skips `clean` stores, and returns "active run is missing" when the row is gone. In revert-all and rollback failure paths the stores set can still contain stores that already finished (their run rows were deleted), so the real error is joined with a misleading one. Load the run lazily, only when a store actually needs to be marked failed.

---

## Round 5 additions

### F30. Ctrl+C or a crash during revert-all still wedges it
Confidence: medium-high (reasoned from code and from `main.go`; not executed).

The resumable revert-all only recognises an interrupted run when the store is `failed` in stage `rolling_back` **and** a `MIGRATION_FAILURE` row with direction `rollback` exists (`interruptedRunIDs`, then `interruptedRevertAllRunID` in [rollback.go](backend/internal/system/migration/engine/rollback.go)). Those rows are only written by `failRun` after an operation returns an error. The migration command path returns before the server's signal handling is installed ([main.go:76](backend/cmd/server/main.go#L76) versus [main.go:148](backend/cmd/server/main.go#L148)), so Ctrl+C, SIGTERM, an OOM kill or a lost connection ends the process without `failRun`.

Result: the store stays `in_progress` in stage `rolling_back`, the rollback operation's progress row stays `in_progress` under the old run ID, and no failure row exists. The next `rollback --revert-all` does not match the interrupted-run condition, generates a new run ID, and ([rollback.go](backend/internal/system/migration/engine/rollback.go), the `progress.RunID != runID` branch) treats the stale row as not belonging to the run and executes with `nil` progress. The insert then violates the unique key `(MIGRATION_STATE_ID, TARGET_LEVEL, OPERATION_ID, EXECUTION_TARGET_ID)`, `failRun` marks the store failed under the new run, and every later attempt hits the same stale row. The integration test for this scenario only simulates the graceful failure case, so it cannot catch it.

Fix direction:
1. Detect an interrupted revert-all from the state alone (`rolling_back`, `in_progress` or `failed`), not from the failure row.
2. Let `revertAll` adopt the existing rollback progress rows for the same state and target level regardless of their run ID (or delete stale in-progress rollback rows before restarting).
3. Install signal handling in the migration command path so a cancellation marks the run failed, and make `failRun` use a non-cancelled context (the same `WithoutCancel` approach the lock release uses).

The same cancelled-context problem applies to forward runs: when `ctx` is cancelled, `failRun` cannot write. Forward `resume` copes because it accepts `in_progress`, but the failure identity is lost.

### F31. Default `rollback` rejects the new `rolling_back` stage with an unhelpful error
Confidence: high.

`phaseForExecutionStage` ([state_controller.go:973](backend/internal/system/migration/engine/state_controller.go#L973)) was not updated for `MigrationStageRollingBack`. `resume` now tells the operator to "rerun the applicable migration rollback command", but plain `rollback` and `rollback --phase` fail inside `inspectRollbackRun` with `unsupported executing migration stage "rolling_back"`. Return a clear error that names `--revert-all`.

---

## Round 4 additions

### F27. New integration tests seed failure state with raw SQL and have not been run
Confidence: high.

The new tests in [08_rollback_entity_type_handle_backfill_test.go](backend/tests/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill_test.go) build their "failed" scenario by hand: `prepareFailedCompletedMigrateRun` rewrites `RUN_ID` on existing progress rows, inserts `MIGRATION_FAILURE` and `MIGRATION_RUN_TARGET` rows, and sets the state to `migrating:failed` with an UPDATE. `prepareInterruptedRollbackRun` does the same for a half-finished rollback. This proves the rollback code reads and cleans those rows. It does not prove that a real failure produces those rows, so `failRun`, `markFailed` and the transition between them are untested end to end (F9, F10, F11, F12).

The handoff also states that the real PostgreSQL run could not complete because the disk filled up. The tests compile (`go vet -tags migration_integration` is clean) but their assertions have never executed, so treat them as unverified.

Still missing:
1. Interrupt `rollback --revert-all` and run it again (F3).
2. A real forward failure in a shared namespace after an earlier store finalized (F11).
3. A real contract failure followed by a start attempt of the old binary (F12).
4. A rollback of a selective run whose crash happens between `CompleteRollback` and the deployment-state cleanup (F9).

### F28. Handoff document overstates what the rollback operation does
Confidence: high.

[migration-engine-handoff.md](migration-engine-handoff.md) says the compensation "clears handles populated by the backfill". The query ([08:18](backend/internal/system/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill.go#L18)) clears every non-null handle in the target scope, whether or not the backfill wrote it (F6). It also says atomic operations that committed no progress "require no compensation". That is true, but the CLI still returns a success-style result without telling the operator that nothing was compensated (F13). Correct both statements, or fix the behavior, before the document is used as the reference.

### F29. Restricted operating mode is static configuration, not tied to migration state
Confidence: medium (I read the middleware and its wiring, not every fenced code path).

`operating_mode` ([config.go:694](backend/internal/system/config/config.go#L694)) is a configuration value, and `RestrictedModeMiddleware` ([operatingmode.go](backend/internal/system/middleware/operatingmode.go)) blocks everything outside an allowlist. Nothing in the migration engine or the startup gate sets it, requires it, or reads it back.
- The new binary is only protected during a selective migration if the operator remembered to configure restricted mode, which is not checked.
- The gate could require `restricted` while a deployment is mid-migration (selective mode), and refuse `full` until the deployment's marker is complete.
- The allowlist includes `/flow/execute` and `/oauth2/token`, which write data. Write fencing for those paths lives in individual executors (for example the JIT provisioning and OU creation errors). I did not audit that every write path reachable through the allowlisted endpoints is fenced.

---

## Round 3 additions

### F25. Revert-all ignores the current state of the stores it overwrites
Confidence: medium-high (reasoned from code).

`revertAll` ([rollback.go:443](backend/internal/system/migration/engine/rollback.go#L443)) reads each store's state but never checks its status. `BeginRevertAll` ([state_controller.go:608](backend/internal/system/migration/engine/state_controller.go#L608)) then unconditionally sets `CompletedLevel` to the accepted source level, `TargetLevel` to the required level, the stage to `contracting`, and the active run to the new run ID. The only guard is that the state equals what `revertAll` read a moment earlier.
- A store that is `failed` or `in_progress` from a forward run has its reservation replaced silently. The old run's failure context and progress are then deleted by `CompleteRevertAll` (`DeleteProgressForTarget`, `DeleteFailuresForState`).
- A store that is `completed` at a phase boundary (for example `contract_ready` with the new binary serving) is accepted without any confirmation (see F4).
- Stores with no rollback candidate are not touched at all (F2), so the command can leave the release half reverted while reporting success.

Fix direction: refuse revert-all unless the operator passes an explicit confirmation flag, and report which states it will overwrite. Treat existing `failed` or `in_progress` stores as a conflict unless a flag says to discard them. This fits naturally with the run record in F24, where the mode and previous run can be recorded.

### F26. Revert-all, phase rollback and the failure paths have no integration test
Confidence: high.

The integration test for rollback ([08_rollback_entity_type_handle_backfill_test.go](backend/tests/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill_test.go)) exercises only the default `rollback` followed by `resume`. Nothing runs `rollback --phase`, `rollback --revert-all`, a failed rollback followed by a retry, or the selective-deployment rollback. The unit tests cover the state controller and candidate selection, but not the multi-store, multi-namespace flows where findings F2, F3, F7, F9 and F11 live.

Add at least these integration tests (the unit-level mocks cannot show the cross-table behavior):
1. `rollback --phase migrate` after a selective run: deployment states and handles are consistent afterwards (F8, F9).
2. `rollback --revert-all` on a four-store release: every changed store ends at its accepted source level and the old manifest's gate passes (F2).
3. Kill `rollback --revert-all` mid-run, then run it again (F3).
4. Fail an operation in a shared-namespace setup after an earlier store has finalized, then check the failed store's status and summary (F11).
5. Force a contract failure and check that the old binary can still start (F12).

---

## Architecture observations

1. **Run mode is encoded in four ad-hoc places.** Revert-all borrows the `contracting` stage, `ExecutionDirection` lives in the failure table, `Compensated` is a boolean on that row, and the failure row is separate from the run. `resume` and the default `rollback` have to guess which applies. F3, F4 and F10 are that guessing going wrong. A single `MIGRATION_RUN` record (mode: forward, rollback or revert-all; scope; selection; status) that every command reads first would replace most of `inspectResumableRun` and `inspectRollbackRun`.
2. **Revert-all is the least safe command and cannot be made correct while most stores have no compensation.** Consider dropping it, or limiting it to the pre-contract window behind an explicit confirmation.
3. **Migrate has no ongoing-write story.** Expand and contract assume writers keep working during migrate. The only mechanism is a one-shot cursor backfill (F1). Prefer a database-side mechanism (trigger or default) installed in expand, or a final catch-up step in contract.
4. **Compatibility is checked only at startup** (F12, F17). For zero downtime, the gate needs to be rechecked at runtime or contract needs a drain guarantee.
5. **Engine surface duplication.** `applyPhase` and `resume` are near copies of the plan-walking loop. One "execute a run" function driven by the run record would shrink the code and the state matrix to test.

## Recommended test additions (to confirm the unverified findings)

1. F5: run `apply migrate --deployment-id A` twice and assert the second run is a no-op or a clean skip.
2. F3: kill `rollback --revert-all` mid-backfill and assert it can be retried.
3. F2: after `--revert-all`, assert every changed store is at its accepted source level and the old manifest's gate passes.
4. F8/F9: selective migrate A, then a failing store-wide migrate; roll back and assert A's deployment state and handles are consistent.
5. F11: shared-namespace failure after an earlier store finalized; assert the failing store ends `failed` with its summary.
6. F1: insert an entity type with `HANDLE NULL` while the backfill runs, and assert the migration either catches it or fails recoverably.
7. F12: force a contract failure and assert the old binary can still start.
