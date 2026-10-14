# Claude Review: Remaining Migration System Issues

Reviewer: Claude. Branch: `migration-plan`. Code reviewed: HEAD `0cfc71202` plus the uncommitted working tree (2026-10-13).
Scope: `backend/internal/system/migration/**`, `backend/cmd/server/migration_cmd.go`, `backend/cmd/server/main.go`, the v2.0.0 release operations, the metadata schema scripts.
Method: every item below was re-checked against the current code, not against the design documents. `go vet` (including the `migration_integration` build tag) and the unit tests for the migration packages and `cmd/server` pass. The Postgres integration suite under `backend/tests/migration` was **not** run, so every finding comes from reading the code. Confidence is stated per finding.

This file replaces all earlier versions. It lists only issues that are still open. Items from earlier reviews that have been fixed are not repeated (they include the revert-all state and fencing problems, the interrupted revert-all wedge, resume after an interrupted rollback, the silent no-op rollback, the split rollback cleanup, the `markFailed` abort, the metadata table sprawl, and the SMTP credentials).

Severity: **High** = data loss, wedged state, or outage risk. **Medium** = incorrect or misleading behavior, or a gap with a workaround. **Low** = hygiene.

## Summary

| ID | Severity | Area | Title |
|---|---|---|---|
| R1 | High | Rollback | Phase rollback fails when a namespace's run row was already deleted |
| R2 | High | Execution | Re-running migrate for an already-migrated deployment wedges the store |
| R3 | High | Backfill | No catch-up for rows written by the old binary during migrate |
| R4 | High | Rollback | The backfill rollback operation clears every non-null handle in scope |
| R5 | Medium | Rollback | Failed-operation rollback of a store-wide backfill leaves deployment markers |
| R6 | Medium | Resume | Resume rejects an interrupted rollback only for `failed` stores |
| R7 | Medium | Compatibility | Any failed contract operation retires the old binary |
| R8 | Medium | Metadata | No in-place metadata upgrade, and schema inspection is all-or-nothing |
| R9 | Medium | Coordinator | Cross-namespace reservation is not atomic |
| R10 | Medium | Contract | No drain check before contract, and the gate runs only at startup |
| R11 | Medium | Rollback | Revert-all overwrites store state without checking it or asking for confirmation |
| R12 | Medium | Tests | Failure paths are seeded with SQL and the integration suite has never run |
| R13 | Medium | Restricted mode | Restricted operating mode is static configuration, not tied to migration state |
| R14 | Medium | Platform | Stores on unsupported providers are skipped by the gate and have no migration path |
| R15 | Low | Failure handling | `MarkFailed` loads the run first and adds a misleading error |
| R16 | Low | CLI | Default `rollback` gives an unhelpful error for the `rolling_back` stage |
| R17 | Low | Readiness | Operation readiness inspects the whole store even for a `--deployment-id` run |
| R18 | Low | Docs | Operator documentation must reflect the implicit contract phase |
| R19 | Low | Initializer | An absent metadata schema is recorded as 1.0.0 even on a fresh 2.0.0 install |
| R20 | Low | Engine | `time.Time` compared with `!=` and exact `UPDATED_AT` matching |
| R21 | Low | Locking | Session advisory locks break behind transaction-pooling proxies |
| R22 | Low | Misc | Smaller items |
| R23 | Low | Repository | Review artifacts committed to the repository root |

---

## High

### R1. Phase rollback fails when a namespace's run row was already deleted
Confidence: medium-high (reasoned from code; the existing integration test hides it).

When a store finishes its phase, `CompletePhaseSelection` calls `DeleteRunIfInactive` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)), which deletes the `MIGRATION_RUN` row of that namespace once no store in it references the run. With the default configuration each store has its own database, so a store that finishes early (runtime_persistent has only one expand operation) has no run row in its database afterwards.

Phase rollback includes every store that has progress in the phase. `BeginRollbackScope` requires the run row in every namespace it touches: `if run == nil || run.Phase != phase { return ... }` ([state_controller.go:510](backend/internal/system/migration/engine/state_controller.go#L510)).

Sequence: `apply expand` finishes runtime_persistent (its run row is deleted), then a configdb operation fails. `rollback --phase expand` begins configdb, then fails on runtime_persistent with "active run does not match phase", and `markFailed` marks the begun stores failed. Every retry fails the same way, because the missing row never reappears.

The integration test `TestPostgresExpandPhaseRollbackRestoresEveryParticipatingStore` ([05_backfill_entity_type_handle_rollback_test.go](backend/tests/migration/releases/v2_0_0/05_backfill_entity_type_handle_rollback_test.go)) inserts a run row in every database before running the command. A real failure does not produce that state.

Fix direction: either keep the run row until the whole run has finished in every namespace, or let `BeginRollbackScope` recreate a missing row from the failing namespace's run (phase, selection) instead of erroring. Add a test that produces the failure with a real failing operation.

### R2. Re-running migrate for an already-migrated deployment wedges the store
Confidence: medium-high (needs a test).

`MIGRATION_PROGRESS` is unique on `(MIGRATION_STATE_ID, TARGET_LEVEL, OPERATION_ID, EXECUTION_TARGET_ID)` with no run ID ([postgres_metadata.sql](backend/internal/system/migration/engine/postgres_metadata.sql)). Only schema-change operations reuse an existing completed progress row (atomic executor). The resumable executor always inserts ([resumable_executor.go:89](backend/internal/system/migration/engine/resumable_executor.go#L89)). Progress of a finished run is not deleted on finalization.

A second `apply migrate --deployment-id A` after the first one completed hits a unique violation and leaves the store `failed`. Resume repeats the failure. Rollback is the only exit, and it clears A's handles (R4). The selective integration test never repeats a deployment ([deployment_selective_test.go](backend/tests/migration/releases/v2_0_0/deployment_selective_test.go)).

Fix direction: treat an existing completed progress row for the same target as "already done" (skip and verify), or exclude already-migrated deployments in preflight with a clear message.

### R3. No catch-up for rows written by the old binary during migrate
Confidence: high. Partly mitigated for contract.

The old binary keeps serving while the store is `migrating` and inserts entity types with `HANDLE NULL`, or renames existing ones.
- The backfill pages with `ID > lastID` ([05_backfill_entity_type_handle.go](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go)). Rows that appear behind the cursor are never visited, and renames after the backfill are never re-synced.
- On resume the executor continues from the saved checkpoint ([resumable_executor.go](backend/internal/system/migration/engine/resumable_executor.go), `executeBatches`) and never rescans from the beginning. If post-verification found remaining null handles, every retry fails the same way.
- What is now better: operation 07's verification detects null handles, so `apply contract` is blocked in preflight instead of failing inside the transaction ([07_finalize_entity_type_handle.go](backend/internal/system/migration/releases/v2_0_0/07_finalize_entity_type_handle.go)).
- What is still missing: once the store is `contract_ready`, migrate cannot be re-run (`requestedPhaseCompleted`), so the operator has no engine command to fix the null handles. The remediation text names no command.

Fix direction: a database-side mechanism installed in expand (trigger or default derived from the name), or a catch-up pass that rescans from the beginning before contract, or a resumable operation that restarts its scan when post-verification fails.

### R4. The backfill rollback operation clears every non-null handle in scope
Confidence: high.

[05_backfill_entity_type_handle_rollback.go](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle_rollback.go) selects `HANDLE IS NOT NULL` and clears all of them. Anything that did not come from this run is lost (handles created by the new binary, or by an earlier selective run). A deployment-specific rollback is correctly scoped to its deployments, but a store-wide rollback wipes everything. The documents now describe this accurately, but it is still data loss in a plausible scenario (rollback after the new binary has run).

Fix direction: record which rows the backfill touched (checkpoint list, marker column, or side table) and revert only those, or refuse rollback once the new binary may have written handles.

---

## Medium

### R5. Failed-operation rollback of a store-wide backfill leaves deployment markers
Confidence: medium-high.

The failed-operation scope runs the rollback operation for target `""`, which clears all handles in the store. `CompleteOperationRollback` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)) deletes progress and updates the run, but never deletes `MIGRATION_DEPLOYMENT_STATE` rows (only `CompleteRollback` and `CompleteRevertAll` do). If an earlier selective run migrated deployment A, A's marker stays while A's handles are gone, and `CheckDeployment` ([compatibility.go](backend/internal/system/migration/engine/compatibility.go)) lets the new binary start for A against `NULL` handles. The run row already knows the selection, so the data to fix this exists.

### R6. Resume rejects an interrupted rollback only for `failed` stores
Confidence: medium.

`rejectForwardResumeDuringRollback` ([coordinator.go](backend/internal/system/migration/engine/coordinator.go)) iterates `resume.failedByNamespace`, so it only inspects `failed` stores. A phase or failed-operation rollback killed mid-way leaves the store `in_progress` with `run.Mode = rollback`, which the check never examines (revert-all is caught separately by the `rolling_back` stage). Forward `resume` is then stopped only incidentally, when a rollback progress row exists and does not match the plan. Read the run mode for every active store.

### R7. Any failed contract operation retires the old binary
Confidence: high. Narrowed by the new null-handle check.

`contracting` maps to `source_retired` regardless of status ([compatibility.go](backend/internal/system/migration/engine/compatibility.go)). A failed atomic contract operation rolls its transaction back and changes nothing, but old pods that restart now refuse to start until the contract is fixed. The new null-handle check removes the most likely trigger, but a write between preflight and execution, a lock timeout, or any other error still causes it. Keep the old binary compatible while a contract run is `failed` and no contract operation has committed.

### R8. No in-place metadata upgrade, and schema inspection is all-or-nothing
Confidence: high.

`InspectSchema` requires all four metadata tables ([metadata_schema.go](backend/internal/system/migration/engine/metadata_schema.go)). A database initialized with an older table set is `SchemaPartial`, `Initialize` answers "partial metadata schema requires repair" ([metadata_initializer.go](backend/internal/system/migration/engine/metadata_initializer.go)), and the startup gate treats the store as incompatible. The handoff tells operators to recreate the proof-of-concept databases after every metadata change. Once released, any metadata schema change is a startup outage for existing installs. The DDL exists in five files (the engine copy and four `dbscripts`), which are identical today but only guarded by convention. Make `MIGRATION_STATE` and `MIGRATION_PROGRESS` the mandatory tables, create the rest through a versioned metadata upgrade keyed on `migrationMetadataVersion`, and generate the DDL from one source or add a drift test.

### R9. Cross-namespace reservation is not atomic
Confidence: medium.

`applyPhase` reserves namespaces one after another ([coordinator.go](backend/internal/system/migration/engine/coordinator.go)). A crash between them leaves store A `in_progress` and store B still at the source level. `resume` then fails because B is "not complete and not owned by the run", and only rollback recovers it (and see R1 for rollback). Consider recording an intent row first, or making resume able to reserve the missing namespaces under the same run.

### R10. No drain check before contract, and the gate runs only at startup
Confidence: high.

`ApplyContract` runs whenever the store is `contract_ready`. Nothing verifies that old pods are gone, and the compatibility gate is evaluated only when a process starts. Contract can drop structures a running old pod uses, and running target pods are not stopped when a revert-all begins (the gate now refuses them at startup only). Options: an explicit confirmation flag, a minimum age since `contract_ready`, a runtime recheck of the gate, or a live-binary registry.

### R11. Revert-all overwrites store state without checking it or asking for confirmation
Confidence: medium-high.

`revertAll` ([rollback.go](backend/internal/system/migration/engine/rollback.go)) reads each changed store's state and only recognizes an interrupted revert-all. `BeginRevertAll` then sets the completed level, target level, stage `rolling_back` and the new run ID for every store with progress, whatever the previous status was. A `failed` or `in_progress` forward run loses its reservation (and its progress and run context are deleted in `CompleteRevertAll`). A store that is `contract_ready` with the target binary serving is accepted without a prompt. Add an explicit confirmation flag, print which states will be overwritten, and treat other active runs as a conflict unless a flag says to discard them.

### R12. Failure paths are seeded with SQL and the integration suite has never run
Confidence: high.

The rollback and resume integration tests build their "failed" state by rewriting `RUN_ID` on progress rows and inserting `MIGRATION_RUN` rows and state updates by hand (`prepareFailedCompletedMigrateRun`, `prepareInterruptedRollbackRun`, `insertMigrationRun`, and an `INSERT ... ON CONFLICT DO NOTHING` in `resume_test.go`). That proves the rollback code reads those rows. It does not prove that a real failure produces them, and it hides R1. The handoff records that the real Postgres run could not complete because the disk filled up, so the assertions have never executed.

Add integration tests that cause real failures: a failing forward operation in a shared and in a separate-database setup followed by `rollback --phase`; a killed `rollback --revert-all` followed by a rerun; a contract failure followed by an old-binary start; a repeated `apply migrate --deployment-id`; and a store that finalizes before a later store fails.

### R13. Restricted operating mode is static configuration, not tied to migration state
Confidence: medium (middleware and wiring read, not every write path).

`operating_mode` ([config.go](backend/internal/system/config/config.go)) is a configuration value, and `RestrictedModeMiddleware` ([operatingmode.go](backend/internal/system/middleware/operatingmode.go)) blocks everything outside an allowlist. Nothing in the migration engine or the startup gate sets, requires or reads it. The target binary is protected during a selective migration only if the operator configured it, which is not checked. The allowlist includes `/flow/execute` and `/oauth2/token`, which write data; fencing for those paths lives in individual executors and was not audited. Let the gate require `restricted` while a deployment is mid-migration, and audit the allowlisted write paths.

### R14. Stores on unsupported providers are skipped by the gate and have no migration path
Confidence: high.

The startup gate now checks the supported stores and marks the others `skipped` ([migration_cmd.go](backend/cmd/server/migration_cmd.go)). That is an improvement, but a skipped store (for example SQLite, the default in `deployment.yaml`) has no protection and no migration path, and when every store is unsupported the gate returns nothing and only logs a warning ([main.go](backend/cmd/server/main.go)). Decide whether SQLite is a supported production target. If not, fail loudly; if so, provide migration support.

---

## Low

### R15. `MarkFailed` loads the run first and adds a misleading error
`MarkFailedWithFailure` calls `GetRun` before it skips `clean` stores and returns "active run is missing" when the row was already deleted. In revert-all and rollback failure paths the stores set can still include stores that finished, so the real error is joined with a misleading one. Load the run only when a store actually needs to be marked failed.

### R16. Default `rollback` gives an unhelpful error for the `rolling_back` stage
`phaseForExecutionStage` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go)) has no case for `rolling_back`. `resume` tells the operator to rerun the rollback command, but plain `rollback` and `rollback --phase` fail with `unsupported executing migration stage "rolling_back"`. Return a clear error that names `--revert-all`.

### R17. Operation readiness inspects the whole store even for a `--deployment-id` run
`CheckReadiness` receives only the plan context, not the selection, so the backfill readiness calls `verifyEntityTypeHandleBackfill(ctx, access, "")` ([05_backfill_entity_type_handle.go](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go)). Invalid or duplicate handles in deployment B block a selective migration of the healthy deployment A. Pass the selection to readiness checks, or document the behavior.

### R18. Operator documentation must reflect the implicit contract phase
Every changed store now ends through a contract phase. Stores with only expand operations stop at `contract_ready` after expand and become `clean` only when `apply contract` runs, and `apply contract` touches stores that have no contract operations. The diagrams were updated, but the operator-facing documents and the runbook still need to say that contract is required for every changed store. The handoff and status documents still say "compensatable" and "uncompensated", which no longer match the "rollback" vocabulary in the code.

### R19. An absent metadata schema is recorded as 1.0.0
[metadata_initializer.go](backend/internal/system/migration/engine/metadata_initializer.go) assigns `preFrameworkBaselineLevel` when the schema is absent. On a fresh 2.0.0 install `initialize` records 1.0.0 and reports a migration that is not needed. It converges only because each operation's pre-verification recognizes the target state and skips.

### R20. `time.Time` equality
`*state != expected` ([state_controller.go](backend/internal/system/migration/engine/state_controller.go), several places) compares `time.Time` structs, and state and run updates match `UPDATED_AT` exactly. It works because both sides come from the same driver and location. It is brittle (location pointer, sub-microsecond truncation).

### R21. Session advisory locks
[postgres_execution_lock.go](backend/internal/system/migration/engine/postgres_execution_lock.go) uses session-level `pg_try_advisory_lock`, which breaks behind transaction-pooling proxies such as PgBouncer. Document it or detect it.

### R22. Smaller items
- The backfill reloads `usedHandles` for every batch (quadratic at scale), [05_backfill_entity_type_handle.go:158](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go#L158).
- `ADD CONSTRAINT UNIQUE` (06) and `SET NOT NULL` (07) hold heavy locks. `lock_timeout` bounds waiting for the lock, not the build time. Acceptable for a small table.
- The migration command runs with `context.Background()` and no signal handling ([main.go:51](backend/cmd/server/main.go#L51)), so Ctrl+C kills the process without `failRun`. Forward runs and revert-all now recover from that, but the failure identity is lost and the last operation's progress stays `in_progress`. Installing a signal handler that marks the run failed (using a non-cancelled context) would keep the record exact.
- Phase rollback still skips a store whose phase progress was entirely reused from an earlier run (reusable schema progress has no row for the current run).
- Rollback and revert-all call `c.namespaceIndexes()` inside loops (cheap but repeated).
- `--phase` parsing for `rollback` is more permissive than the usage string; check combinations of `--phase` and a positional phase.

### R23. Review artifacts committed to the repository root
Commit `0cfc71202` added `claude-migration-system-review-findings.md`, `migration-engine-review-findings.md` and `claude-migration-diagrams.excalidraw` (about 20,900 lines, because the file is pretty-printed JSON) to the repository root. These are working notes, not product documentation. Consider moving them under a docs folder, regenerating the diagram without indentation, or keeping them out of the main history. Also keep the unrelated uncommitted `gate_client` change in `backend/cmd/server/deployment.yaml` out of migration commits.

---

## Suggested order of work
1. R1, R2, R12: they are the most likely to bite in a real rollout, and R12 is how you will find the rest.
2. R3 and R4: data integrity around the backfill and its rollback.
3. R8: decide the metadata upgrade path before the first release; after release it needs a migration of its own.
4. R10, R11, R13, R14: operator safety and platform coverage.
5. The Low items as part of normal cleanup.

## Integration tests that would confirm the unverified items
1. R1: a real failure in a later store after an earlier store finalized, then `rollback --phase expand` (separate databases and a shared database).
2. R2: run `apply migrate --deployment-id A` twice and assert the second run is a clean skip.
3. R3: insert an entity type with `HANDLE NULL` while the backfill runs, and assert a catch-up or a recoverable failure.
4. R5: selective migrate of A, then a failing store-wide migrate; fail-operation rollback; assert A's marker and handles agree.
5. R6: kill a phase rollback mid-way, then run `resume` and assert it refuses.
6. R7: force a contract failure and assert the old binary can still start.
7. R11: run `rollback --revert-all` while a forward run is `failed` and assert it asks for confirmation or refuses.
