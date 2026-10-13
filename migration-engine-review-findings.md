# Migration engine review findings

## Purpose

Findings from a read-through of the migration engine as implemented on the `migration-plan` branch
(`backend/internal/system/migration/`, plus the command wiring in `backend/cmd/server/`).

This is a review record, not a commitment to act. Each finding states what was observed, why it
matters and whether it is reachable today, so it can be confirmed or dismissed independently.
Line references are from the branch state at review time and will drift.

Status values: `open`, `confirmed`, `rejected`, `fixed`.

| ID | Severity | Area | Summary | Status |
|---|---|---|---|---|
| F1 | High | Coordinator | `markFailed` is given stores that already completed the phase | open |
| F2 | Medium | Plan contract | `RollbackCapableOperation` is declared and validated but never invoked | open |
| F3 | Medium | Phase model | Expand-only plans leave no resting backward-compatible state | open |
| F4 | Medium | Startup gate | Gate is skipped for every store when any store uses an unsupported provider | open |
| F5 | Low | Dead code | Production-unused API surface kept alongside the phase-aware variants | open |
| F6 | Low | Coordinator | `storeCompletedPhase` swallows every plan lookup error | open |
| F7 | Low | Resumable executor | Batch loop has no bound on batch count or duration | open |
| D1 | Design | Source retirement | No runtime re-evaluation of migration state by serving instances | open |

## F1. `markFailed` is given stores that already completed the phase

**Severity**: High. **Status**: open.

**Where**: `backend/internal/system/migration/engine/coordinator.go:364`, `:373` (apply path) and
`:459`, `:470` (resume path); `backend/internal/system/migration/engine/state_controller.go:299`
and `:304` (`MarkFailed`).

**Observed**: `applyPhase` builds `reservedStores` at reservation time and never removes a store
from it after `CompletePhase` succeeds for that store. When a later operation or finalization
fails, `markFailed` is called over the full original set.

For a store whose phase plan declares a following phase, `CompletePhase` leaves the state
`completed` at the preceding boundary stage with `ACTIVE_RUN_ID` cleared. `MarkFailed` skips only
`clean` states (`state_controller.go:299`), so it reaches the run-ownership check and returns
`store is reserved by a different run`. `MarkFailed` wraps one namespace in a single `Transact`,
so that error aborts the transaction.

**Why it matters**: the store that actually failed is never marked failed. It is left
`in_progress` with no failure summary, which is the only failure diagnostic the metadata keeps.
The run degrades from "failed with a recorded cause" to "abandoned". `migration resume` still
recovers it, because `inspectResumableRun` accepts `in_progress` stores, so this is a
diagnosability and correctness-of-state problem rather than data loss.

**Reachability**: not reachable with the shipped 2.0.0 plan. That plan is expand-only, so
`CompletePhase` finalizes to `clean` and `MarkFailed` skips it. It becomes reachable for the first
plan where one store declares operations in two phases.

**Candidate fix**: remove a store from the reserved set once its finalization succeeds, or make
`MarkFailed` treat `completed` with an empty `ACTIVE_RUN_ID` as already out of the run and skip it.
Prefer the former, since the reserved set is the coordinator's own bookkeeping.

**Test to add**: a multi-phase plan where store A finalizes its phase before store B's operation
fails, in both the separate-database and shared-database layouts. The shared layout is the one that
exposes the aborted transaction.

## F2. `RollbackCapableOperation` is declared and validated but never invoked

**Severity**: Medium. **Status**: open.

**Where**: `backend/internal/system/migration/engine/plan.go:98` (interface) and `:249`
(plan validation rejecting atomic operations that implement it).

**Observed**: no executor calls `Rollback`. The only other references are the plan validation rule
and its unit test.

**Why it matters**: the interface is a published contract. An operation author can implement
`Rollback` and reasonably expect the engine to call it after a failed batch, and nothing in the
engine says otherwise. The implementation backlog lists "optional operation rollback after
resumable execution or immediate post-verification failure" as a later phase item, so the
abstraction currently precedes its behavior, which the backlog's own change rules discourage.

**Candidate fix**: either wire it into `ResumableOperationExecutor.executeBatches` with explicit
semantics for what is rolled back and what is left to resume, or delete the interface and the
validation rule until the behavior is implemented.

## F3. Expand-only plans leave no resting backward-compatible state

**Severity**: Medium. **Status**: open.

**Where**: `backend/internal/system/migration/engine/state_controller.go` (`CompletePhase` and
`phasePlanForStore`), `backend/internal/system/migration/engine/compatibility.go:207` and `:218`
(`servingCompatibilityReason`), `backend/internal/system/migration/releases/v2_0_0/release.go`
(expand-only plan).

**Observed**: `phasePlanForStore` derives the declared phases from the operations that exist for a
store. When a store has expand operations only, `next` is empty, so `CompletePhase` calls
`FinalizeState` and the store advances directly to `completed_level = <target>`, stage `complete`,
status `clean`. There is no durable `expanded` stage at rest.

`servingCompatibilityReason` admits a source binary only while the state carries a target level
that differs from the source binary's required level. Once the store is `clean` at the new level,
the source binary sees `level_mismatch`.

**Why it matters**: the expand and contract phases exist so old and new binaries can overlap.
For an expand-only plan that property does not hold: the moment expand finishes, binaries of the
previous release are refused at startup, even though expand changes are backward compatible by
construction (the 2.0.0 operations drop a `CHECK` constraint and create a new table, both of which
an older binary can serve against).

**Reachability**: harmless for 1.0.0 to 2.0.0, because 1.0.0 is the pre-framework baseline and has
no compatibility gate. It becomes real at 2.0.0 to 3.0.0: a 2.0.0 instance restarting after an
expand-only 3.0.0 run gets `level_mismatch` and exits.

**Candidate fix**: decide and then enforce. Either state explicitly that a release wanting a
mixed-version window must declare at least one post-expand phase for the affected store, and
validate that in the plan registry, or let the engine hold a store at `expanded` when the plan has
no later phase and advance the level under a separate explicit step. Note that
`validateSafeWorkflow` (`coordinator.go:254`) already enforces the mirror-image rule for
contract-only stores, so there is a precedent for where the check belongs.

## F4. Startup gate is skipped for every store when any store uses an unsupported provider

**Severity**: Medium. **Status**: open.

**Where**: `backend/cmd/server/migration_cmd.go:361` (`startupCompatibilityCheckSupported`),
consumed by `runStartupCompatibilityCheck` and reported at `backend/cmd/server/main.go:95`.

**Observed**: the predicate returns false if any of the four data sources has an unregistered
migration adapter, and `runStartupCompatibilityCheck` then returns `nil, nil`. The server logs a
warning and starts.

**Why it matters**: a mixed deployment, for example PostgreSQL `configdb` and `entitydb` with a
SQLite `runtime_transient`, silently ungates the PostgreSQL stores as well. The compatibility
guarantee degrades to a single warning line, and nothing distinguishes "no gate is possible here"
from "the gate passed".

**Reachability**: any mixed-provider configuration. SQLite is dev-only, so the blast radius is
limited, but the all-or-nothing shape is the issue rather than SQLite itself.

**Candidate fix**: evaluate per store, skipping only the stores whose provider has no registered
adapter, and report explicitly which stores were gated and which were skipped.

## F5. Production-unused API surface

**Severity**: Low. **Status**: open.

**Where**:

- `backend/internal/system/migration/engine/metadata_store.go:475` (`UpdateState`) and its query
  `queryUpdateMigrationState` at `:30` (`MIG-METADATA_MGT-03`).
- `backend/internal/system/migration/engine/state_controller.go:76` (`Reserve`) and `:392`
  (`Finalize`), the phase-less wrappers.
- `backend/internal/system/migration/engine/coordinator.go:191` (`Apply`), the phase-less wrapper.
- `backend/internal/system/migration/engine/preflight.go:492` (`PreflightChecker.Check`) and the
  `checkStores` and `checkPendingStores` chain it is the sole caller of, including
  `readinessEvaluator.check` at `:222`.

**Observed**: each is referenced only from tests. Every production path goes through the
phase-aware variants: `ReservePhase`, `CompletePhase`, `ApplyPhase` or `ApplyContract`, and
`CheckPhase`.

**Why it matters**: `AGENTS.md` asks for dead code to be deleted cleanly and for no premature
abstractions. `UpdateState` in particular is an unconditional write that bypasses the
compare-and-swap discipline the rest of the metadata store is built on, so leaving it available is
a small correctness hazard as well as dead weight.

**Candidate fix**: delete, and move the tests onto the phase-aware entry points. Confirm first that
nothing outside `backend/` (tools, integration harness) depends on them.

## F6. `storeCompletedPhase` swallows every plan lookup error

**Severity**: Low. **Status**: open.

**Where**: `backend/internal/system/migration/engine/coordinator.go:635`.

**Observed**: any error from `phasePlanForStore` results in
`return state.Status == StatusClean || state.Status == StatusCompleted, nil`. The intended case is
"this store declares no operations in the active phase", which is legitimate. The same branch also
absorbs the unsupported-phase error, treating an invalid phase as a completed one.

**Candidate fix**: distinguish the two, for example with a sentinel error for "store has no
operations in this phase", and propagate anything else.

## F7. Resumable batch loop has no bound

**Severity**: Low. **Status**: open.

**Where**: `backend/internal/system/migration/engine/resumable_executor.go:200` (`executeBatches`).

**Observed**: the loop runs until the operation reports `Completed` or the context is cancelled.
`validateResumableBatchResult` requires each incomplete batch to advance the checkpoint, which
prevents a stalled loop but not an unbounded one.

**Why it matters**: an operation with a faulty completion condition runs until the context dies,
with no progress signal distinguishing slow from wrong. The implementation backlog already lists
"configurable backfill throttling and resource limits" as a later item, so this is a note for when
that item is picked up rather than an independent defect.

**Candidate fix**: when throttling is implemented, add a batch-count or duration ceiling that fails
the operation with a distinct error rather than relying on context cancellation.

## D1. No runtime re-evaluation of migration state by serving instances

**Severity**: Design gap for automatic source retirement. **Status**: open.

**Where**: `backend/cmd/server/main.go:89` (the gate runs once, before caches, bootstrap, workers
and listeners), `backend/internal/system/migration/engine/compatibility.go:207`
(`MigrationStageContracting` to `CompatibilityReasonSourceRetired`).

**Observed**: the compatibility gate is admission control at process start. It is binary: the
process either serves everything or exits. Nothing re-reads migration state afterwards.

**Why it matters**: the `source_retired` rule encodes the intent that a source binary must stop
serving once contract starts. A source instance that is already running when contract begins never
re-reads the state, so the rule only takes effect on the next restart, which is not the moment it
is needed. The intent exists in code and is unenforceable where it counts.

**Relationship to restricted mode**: restricted mode is now configuration-driven and explicitly
administrator-controlled. Administrators apply the same mode to every serving source instance and
restart them before migration work that requires fencing. Runtime migration-state polling is
therefore not a prerequisite for the implemented HTTP restrictions or feature gates. The remaining
consumer for re-evaluation is automatic enforcement of the `source_retired` compatibility rule.

**Open design questions**, to be settled before automatic source retirement is implemented:

1. Is the intended operational model a genuine overlap of old and new instances under load, or does
   the orchestrator (Helm, OpenChoreo) drain the old version before contract? If it is the latter,
   the runtime loop may be unnecessary.
2. If migration state must retire running source instances automatically, define the propagation
   mechanism, bounded reaction time and behavior when state cannot be read.

**Recommendation**: keep automatic source-retirement re-evaluation separate from restricted mode.
Implement it only when the deployment model requires migration state, rather than administrator
configuration and orchestration, to retire already-running source instances.

## Verification performed

- `go test ./internal/system/migration/...` passes.
- `go vet ./internal/system/migration/... ./cmd/server/...` is clean.
- No integration run against PostgreSQL was performed as part of this review.
