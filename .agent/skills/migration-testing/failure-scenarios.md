# Failure scenarios to cover

Migrations fail in the field: a lock timeout, a killed pod, a conflicting row, an operator running two
commands. Each operation type has scenarios that must be covered. Cover them with a unit test where
the logic is pure, and with an integration test where real recovery is involved.

## By operation type

### Atomic (schema or data)

| Scenario | Expected |
|---|---|
| Source state | Runs and reaches the target |
| Already target (re-run, or re-apply after rollback) | Skipped as success, no error |
| Invalid shape | Readiness blocked, apply refuses with the finding |
| Statement fails halfway | Whole transaction rolled back, progress row absent, store `failed`, `resume` runs it from scratch |
| Table lock held by another session | Fails within the lock timeout, no partial change, `resume` succeeds after the lock is released |
| `PostVerify` does not return target | Transaction rolls back |

### Online

| Scenario | Expected |
|---|---|
| Crash after `Execute`, before `Finalize` | Progress row in progress, leftover recognised, resume runs `Finalize` and completes |
| Interrupted concurrent index build (invalid index) | Classified as source, invalid index dropped, rebuilt |
| `Finalize` fails (lock timeout) | Transaction rolled back, resume retries `Finalize` |
| Constraint attached and leftover index present | `invalid` with finding |
| Re-run on target | Skipped |

### Resumable

| Scenario | Expected |
|---|---|
| More rows than one batch | Several batches, checkpoint advances each time |
| Fail on batch N | Batches before N durable, batch N rolled back, resume continues at checkpoint, final data equals an uninterrupted run |
| Process killed between batches | Stores `in_progress` (abandoned), resume continues |
| Rows added or changed after the first batches | Handled, or caught by the catch-up |
| Empty scope | Completes immediately as target |
| Ambiguous or conflicting row | Readiness blocked or pre verification `invalid`, nothing partially written |
| Deployment selection | Only the selected deployments change, each with its own progress row |
| Checkpoint shape change in a later fix | Old checkpoints still decode |

### Rollback operations

| Scenario | Expected |
|---|---|
| Forward, then rollback | Original representation restored exactly |
| Rollback, then forward again | Same result as the first forward |
| Rollback interrupted | Rollback mode kept, forward `resume` refused, re-running `rollback` finishes |
| Partial forward then rollback | Only the forward results are undone, nothing else |
| Rows created independently of the forward step | Not deleted |

## Engine behaviours to exercise for each release

These cut across operations. The existing suite covers many; add to them when your operation changes
the picture.

| Scenario | Command sequence |
|---|---|
| Failed operation, retry | `apply expand` (fails) then `resume` |
| Failed operation, undo only that | `rollback` then `resume` |
| Failed phase, undo phase | `rollback --phase expand`, then `apply expand` again |
| Whole release before contract | `apply expand`, `apply migrate`, `rollback --revert-all`, then apply again |
| Revert-all after contract started | Refused when a contract operation completed without a rollback operation |
| Selective migrate | `apply migrate --deployment-id A`, then the others, then `apply contract` |
| Resume after a selective run where one store already finished | `resume` accepts it |
| Concurrent runners | Second command fails with "already locked", first completes |
| Contract through the generic path | Engine unit test: `ApplyPhase` with contract returns `ErrContractRequiresDedicatedExecution`. Only `ApplyContract`, behind `apply contract`, runs it |
| Compatibility at each boundary | `status` admits both binaries at expanded, refuses the old one after migrate starts |
| Shared and separate layouts | Same results for both |

## Injecting failures

Prefer real failures over mocks in the integration suite:

- a **conflicting row** inserted before the run (duplicate value that violates the new unique rule);
- an **object that blocks** the step (an unexpected index or column);
- a **held lock** from a second connection (`LOCK TABLE ... IN ACCESS EXCLUSIVE MODE` in an open
  transaction) to prove lock timeouts fail fast;
- **deleting progress rows** or **adding an abandoned run** to model a killed process, as the existing
  resume tests do;
- stopping a run by cancelling the container at a chosen moment is also valid if deterministic.

## What to assert after every failure test

1. The command exit status and message tell the operator what to do.
2. `MIGRATION_STATE` shows the expected stage and status, and the failure summary names the operation.
3. The data is either fully old for that unit of work or fully new, never a mix the old or new binary
   would misread.
4. After `resume` or rollback plus re-apply, the final data equals an uninterrupted run.
