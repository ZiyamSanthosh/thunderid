# Rollback operations

A rollback operation reverses one forward operation. Decide first whether one is needed, see
[rollback design](../migration-plan/rollback-design.md). Additive schema operations normally have none.

## Interface

```go
type RollbackOperation interface {
	Operation                       // Definition, ResolveTargets, PreVerify, PostVerify
	ForwardOperationID() OperationID
}
```

A rollback operation also implements one execution interface (`AtomicOperation`, `OnlineOperation` or
`ResumableOperation`), exactly like a forward operation.

Plan validation requires:

- `ForwardOperationID()` names an existing operation in `Expand`, `Migrate` or `Contract`;
- each forward operation has at most one rollback operation;
- the rollback uses the **same store** and the **same kind** as the forward operation (a data change is
  undone by a data change). The interruption mode may differ;
- its own id is unique in the plan.

Register it in `MigrationPlan.Rollback`. List order does not matter, the engine runs rollbacks in
reverse forward order.

## Verification semantics

The engine uses the same four methods, so interpret the states for the reverse direction:

| State | Meaning for a rollback operation |
|---|---|
| `source` | The forward result is present and still has to be undone |
| `target` | The original representation is already restored. Nothing to do |
| `invalid` | Neither. For example, rows changed independently |

`PostVerify` must say `target`. A rollback executed twice, or after a partial rollback, must be safe.

## Sharing code with the forward operation

When the forward and rollback differ only in direction, build both from one struct with a direction
field and swap the summaries, as the 2.0.0 data rewrites do. Keep one transform function with a
`direction` parameter so the two can never drift apart.

```go
type widgetColorDirection string

const (
	widgetColorForward  widgetColorDirection = "forward"
	widgetColorRollback widgetColorDirection = "rollback"
)

type rollbackWidgetColorOperation struct {
	*widgetColorOperation
}

func (o *rollbackWidgetColorOperation) ForwardOperationID() engine.OperationID {
	return "2.1.0/configdb/63_backfill_widget_color"
}
```

The rollback's `Definition()` has its own id, usually the forward id plus `_rollback`, and the same
store and kind.

## Rules

1. **Undo only what the forward step produced.** If the product can independently create a result
   indistinguishable from the forward one, the rollback can delete genuine data. Make forward results
   recognisable, or document the limit in the plan and the operation file.
2. **Aim for an exact restore.** Round trip the representation: a NULL must come back as NULL, not an
   empty object, and large numbers must keep precision if they can occur. Where exactness is impossible,
   say so in a comment on the rollback and in the plan.
3. **Same granularity.** Per deployment targets in forward means per deployment targets in rollback, and
   a selective rollback uses the deployment from its target.
4. **Guard against concurrent writers.** The old binary may be running again after rollback. Update with
   conditions that match the forward result (`WHERE column = <forward value>`) rather than blind
   writes.
5. **Resumable in the same way.** Rows to undo are still in scope after a crash, so a keyset cursor over
   rows that still hold the forward result works naturally.
6. **Do not rely on the forward operation's progress.** Rollback state comes from the data and the
   verifiers.

## Example from release 2.0.0

The handle backfill writes a handle for every entity type without one. Its rollback selects rows with a
non NULL handle in batches ordered by id (keyset cursor), sets `HANDLE = NULL` for each in the batch,
and returns `Completed` when fewer than a full batch was found. It is a resumable data change over the
same store as the forward operation and is registered with the forward operation id.

## Checklist

- [ ] Same store and kind as the forward operation. Forward id is correct.
- [ ] Verifiers recognise original, forward result and partial states.
- [ ] Only recognisable forward results are changed.
- [ ] Restore is exact, or the limit is documented.
- [ ] Safe to run twice and to resume.
- [ ] Unit tests for the transform in both directions, integration test for a full forward then
      rollback then forward again.
