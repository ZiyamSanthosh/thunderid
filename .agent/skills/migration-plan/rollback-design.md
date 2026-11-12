# Rollback design

Rollback is an explicit operator action before contract. It undoes work and returns stores to an
earlier boundary so the old binary can serve again. The plan decides which operations can be undone.

## The three scopes

| Command | Scope | What happens |
|---|---|---|
| `migration rollback` | Failed operation | Runs the paired rollback operation of the failed forward operation. The run stays, the operation can be retried |
| `migration rollback --phase <p>` | Failed phase | Runs rollback operations for the phase's completed operations, newest first, then returns each store to the boundary before the phase |
| `migration rollback --revert-all` | Whole release before contract | Runs rollback operations for every executed operation that has one, newest first, and returns every changed store to its accepted source level with its progress cleared |

Rollback operations run in reverse plan order. Operations without a rollback operation are simply left
as they are. Their progress rows are removed, so a later `apply` runs them again and their verifiers
see the result already there (`target`) and skip them.

## When does an operation need a rollback operation?

Ask: **if the operator returns to the old binary, can it still run correctly with this operation's
result left in place?**

| Operation | Rollback operation |
|---|---|
| Additive schema change (new table, new nullable column, wider column, new index) | **None.** The old binary ignores it. Re-applying is a no-op because verification reports `target` |
| Index or key swap that keeps a legacy compatible index | **None**, for the same reason |
| Data step writing a new column the old binary ignores | Optional. Provide one if leaving stale values could confuse a later re-run or the old binary |
| Data step creating derived rows or links the old binary would misread or duplicate | **Required.** Remove what it created |
| Representation rewrite (Migrate) | **Required.** Restore the previous representation |
| Contract step that drops or tightens | **None, and say so.** Irreversible. It blocks revert-all once completed |
| Contract step that can truly be undone | Optional compensating rollback. If present, revert-all is not blocked by it |

If a forward operation has no rollback operation and is in Contract, a completed run of it blocks
`--revert-all`. That is the intended safety stop.

## Writing the rollback plan

For every operation with a rollback, record:

- **Forward id** it reverses. Exactly one rollback per forward operation.
- **What exactly it removes or restores.** Only what the forward step produced.
- **Whether the result is exact.** If the restore can differ from the original (a NULL becoming an
  empty object, a number losing precision, a link the product created natively matching one the
  migration created), state that in the plan. Prefer a design where the restore is exact.
- **Targets.** Same granularity as the forward operation, usually per deployment.
- **Mode.** Usually resumable for data, because the forward step was. The mode need not match the
  forward one. Kind must match: a data change is rolled back by a data change, a schema change by a
  schema change. The store must match too.
- **Verification.** Source is "forward result present". Target is "original shape restored". It must be
  idempotent: rolling back twice, or after a partial rollback, must be safe.

## Rules

1. A rollback operation must be safe if the forward operation only partly ran. Verification classifies
   the state; it does not assume completion.
2. A rollback must not destroy data the product created independently. If the forward step and the
   product can produce indistinguishable results, the rollback is ambiguous. Either mark forward results
   so they can be recognised, or accept and document the limit in the plan.
3. A rollback operation changes only its own store, like a forward operation.
4. Rollback order is reverse of the plan. If operation B depends on A, rolling back B first is required
   and is automatic. Do not register a rollback that assumes A was already undone.
5. Do not add a rollback to an operation just to have one. A rollback that cannot restore state
   misleads the operator. Write "none" and give the reason.
6. Rollback of a Migrate or Expand data step must not require the old binary to be running.

## Planning the window

The rollback window is the time between `contract_ready` and the contract command. Plan the product
release so nothing in the window makes rollback impossible, for example a new feature writing data
that only the new representation can hold. State any such limit in the plan.

## Interruption

If a rollback is interrupted, the run stays in rollback mode and forward `resume` is refused. The
operator re-runs `rollback`. Rollback operations that are resumable continue from their own
checkpoint. Design them so that continuing is always possible.
