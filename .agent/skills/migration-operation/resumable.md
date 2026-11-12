# Resumable data operations

Use for data changes that touch many rows, run per deployment, or must survive a restart without
repeating finished work. The engine calls `ExecuteBatch` repeatedly. **Each call runs in its own
transaction together with the checkpoint update**, so progress is never ahead of or behind the data.

## How the engine runs it

```text
insert progress row, empty checkpoint        (durable)
PreVerify before any work                    (read only, only on a fresh start)
loop until Completed:
    begin transaction
      ExecuteBatch(previous checkpoint) -> new checkpoint, Completed flag
      validate the result
      if Completed: PostVerify must be target, mark progress completed
      write the new checkpoint
    commit
```

- A crash or error rolls back only the batch in flight. Earlier batches and their checkpoints remain.
- `resume` loads the last checkpoint and continues. `PreVerify` is **not** repeated on resume.
- The final batch's `PostVerify` runs in the same transaction. If it fails, the last batch rolls back.
- The context is checked between batches, so a cancelled run stops cleanly.

## Result rules enforced by the engine

| Rule | Consequence of breaking it |
|---|---|
| Checkpoint is valid JSON and at most 64 KiB | Batch fails |
| An incomplete batch returns a non empty checkpoint | Batch fails |
| An incomplete batch returns a checkpoint **different** from the previous one | Batch fails. It would loop forever |
| `Completed: true` requires `PostVerify` to say `target` | Batch fails and rolls back |

## Interface

```go
type ResumableOperation interface {
	Operation
	ExecuteBatch(context.Context, OperationExecution, json.RawMessage) (ResumableBatchResult, error)
}

type ResumableBatchResult struct {
	CheckpointData json.RawMessage
	Completed      bool
}
```

Definition: `Kind: engine.OperationKindDataChange`, `InterruptionMode: engine.InterruptionModeResumable`.

## Template

```go
const widgetBatchSize = 100

type widgetCheckpoint struct {
	LastID string `json:"lastID"`
}

type backfillWidgetColorOperation struct {
	definition engine.OperationDefinition
}

func (o *backfillWidgetColorOperation) Definition() engine.OperationDefinition { return o.definition }

func (o *backfillWidgetColorOperation) ResolveTargets(
	ctx context.Context, access engine.ReadAccess, selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return resolveDeployments(ctx, access, selection) // see atomic-sql.md
}

func (o *backfillWidgetColorOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := widgetCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode widget checkpoint: %w", err)
		}
	}
	access := execution.Access()
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryWidgetBatch,
		checkpoint.LastID, execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read widget batch: %w", err)
	}
	for _, row := range rows {
		id, err := row.String("id")
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		// compute the new value, then write it. Same transaction as the checkpoint.
		if _, err := access.Execute(ctx, engine.StoreIDConfigDB, queryUpdateWidget, "blue", id); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("update widget %q: %w", id, err)
		}
		checkpoint.LastID = id
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	return engine.ResumableBatchResult{CheckpointData: data, Completed: len(rows) < widgetBatchSize}, nil
}
```

with, for the batch query, an ordered keyset page filtered by deployment:

```sql
SELECT ID FROM "WIDGET"
WHERE COLOR IS NULL AND ID > $1 AND ($2 = '' OR DEPLOYMENT_ID = $2)
ORDER BY ID LIMIT 100
```

## Designing the checkpoint

- Use a **keyset cursor** (`ID > last`), never `OFFSET`. Keys must be totally ordered. If uniqueness
  is `(DEPLOYMENT_ID, ID)`, order and cursor by both, as the 2.0.0 reference rewrites do.
- The checkpoint must strictly advance with every non final batch, or the engine rejects it. A last id
  that changes does this naturally.
- Store only what is needed to continue. Counters and the cursor, never the data.
- Version the checkpoint (`"version"`) if its shape might change in a later fix, so old checkpoints
  still decode.
- Select rows still needing work (`WHERE COLOR IS NULL`) as well as using the cursor. After a resume
  this makes already converted rows cost nothing and tolerates rows changed in between.

## Per deployment

`ResolveTargets` returns one target per selected deployment. Each target has its **own progress row and
checkpoint**. Read the deployment from `execution.Target().DeploymentID` and filter every query by it.
An empty deployment id means the whole store.

Cursor and uniqueness checks must be deployment aware. A generated value must be unique within its
deployment, not globally.

## Batch behaviour

- Default size 100. Pick a size so one batch is a short transaction. Large JSON rows justify smaller.
- A batch must be safe to retry from scratch: after a rollback the same rows are read again.
- Write only through `execution.Access().Execute`. It is bound to the operation's store.
- Read other stores only if listed in `ReadStoreIDs`.
- Do not call services or the network inside a batch, except an injected read only provider that is
  already resolved, see [cross-store-and-services.md](cross-store-and-services.md).
- A batch is allowed to find no rows and return `Completed: true` with its checkpoint.

## Verifiers

- `PreVerify`: `source` when work remains in scope, `target` when none, `invalid` for ambiguity.
  Include the deployment filter. Run once on a fresh start only.
- `PostVerify`: `target`, with the same classifier. It decides whether the last batch may commit.
- Ambiguity (collisions, unknown shapes) appears as `invalid` from `PreVerify` and from readiness, so
  the operator sees it before the run, not during it.

## Catch-up

If the plan calls for a catch-up, register the same operation a second time in Migrate with a different
id. Make the id a field of the operation struct and expose two constructors. The pre verification
returns `target` when nothing is left, so the catch-up is free when unneeded.

## Checklist

- [ ] Keyset cursor, strictly advancing checkpoint, bounded batch.
- [ ] Deployment filter in every query. Per deployment checkpoint.
- [ ] Writes use `execution.Access().Execute` only.
- [ ] `Completed` is true exactly when no rows remain after this batch.
- [ ] Classifier returns `target` for an empty scope.
- [ ] Rollback operation exists or the plan states why not.
- [ ] Logic is in pure helper functions so unit tests can cover it, see
      [unit tests](../migration-testing/unit-tests.md).
