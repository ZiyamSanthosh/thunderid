# Online schema operations

Use when the work cannot or should not run inside a transaction, typically building an index without
blocking writes, followed by a short step that attaches it. Schema changes only.

## How the engine runs it

```text
prepare    record or reuse the progress row (durable)
PreVerify  classify, aware of leftovers from an earlier attempt (read only)
Execute    autocommit, no transaction: the heavy step
Finalize   a short transaction: attach constraint, swap names, mark validated
PostVerify must be target
complete   progress marked completed
```

A crash after `Execute` keeps what `Execute` committed. The progress row stays in progress. When the
operation runs again, `PreVerify` must recognise the leftover and `Execute` must continue or clean up.

## Interface

```go
type OnlineOperation interface {
	Operation                                        // Definition, ResolveTargets, PreVerify, PostVerify
	Execute(context.Context, OperationExecution) error
	Finalize(context.Context, OperationExecution) error
}
```

Definition: `Kind: engine.OperationKindSchemaChange`, `InterruptionMode: engine.InterruptionModeOnline`.
`ResolveTargets` returns `[]engine.ExecutionTarget{{}}`.

## Template

```go
type widgetColorIndexOperation struct {
	definition  engine.OperationDefinition
	createIndex dbmodel.DBQuery // CREATE UNIQUE INDEX CONCURRENTLY ...
	dropIndex   dbmodel.DBQuery // DROP INDEX CONCURRENTLY ...
	lockTimeout dbmodel.DBQuery // SET LOCAL lock_timeout = '2s'
	attach      dbmodel.DBQuery // ALTER TABLE ... ADD CONSTRAINT ... UNIQUE USING INDEX ...
}

func (o *widgetColorIndexOperation) Definition() engine.OperationDefinition { return o.definition }

func (o *widgetColorIndexOperation) ResolveTargets(
	context.Context, engine.ReadAccess, engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *widgetColorIndexOperation) PreVerify(
	ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target) // source | target | invalid, see below
}

func (o *widgetColorIndexOperation) Execute(ctx context.Context, execution engine.OperationExecution) error {
	state, err := inspectIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if state.present && state.valid {
		return nil // an earlier attempt already built it
	}
	if state.present && !state.valid {
		if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB, o.dropIndex); err != nil {
			return fmt.Errorf("drop invalid widget index: %w", err)
		}
	}
	if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB, o.createIndex); err != nil {
		return fmt.Errorf("create widget index concurrently: %w", err)
	}
	return nil
}

func (o *widgetColorIndexOperation) Finalize(ctx context.Context, execution engine.OperationExecution) error {
	state, err := inspectIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if !state.valid {
		return errors.New("finalize widget index: prepared index is unavailable")
	}
	if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB, o.lockTimeout); err != nil {
		return fmt.Errorf("set widget lock timeout: %w", err)
	}
	if _, err := execution.Access().Execute(ctx, engine.StoreIDConfigDB, o.attach); err != nil {
		return fmt.Errorf("attach widget constraint: %w", err)
	}
	return nil
}

func (o *widgetColorIndexOperation) PostVerify(
	ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}
```

## The classifier for an online operation

It must separate every combination the steps can leave:

| Observed | Classification |
|---|---|
| Legacy shape, no index | `source` |
| Legacy shape, valid index not attached | `source` (Execute reuses it, Finalize attaches) |
| Legacy shape, **invalid** index (interrupted build) | `source` (Execute drops it first) |
| Constraint attached, no leftover index | `target` |
| Constraint attached **and** leftover index, or any other mix | `invalid` with a finding |

`pg_index.indisvalid` and `indisready` identify an invalid concurrent build.

## Rules

- `CREATE INDEX CONCURRENTLY` and `DROP INDEX CONCURRENTLY` cannot run in a transaction. They belong in
  `Execute`, which is autocommit.
- Put `SET LOCAL lock_timeout` in `Finalize`. It has no effect in `Execute`.
- `Finalize` must be short. It is the only part that takes a strong lock. Use
  `ADD CONSTRAINT ... USING INDEX` so it does not scan the table.
- Add foreign keys `NOT VALID`, then `VALIDATE CONSTRAINT` in a separate step, so the add is cheap.
- Each step must tolerate being repeated: `IF NOT EXISTS`, or inspect first.
- Keep legacy unique indexes the old binary's `ON CONFLICT` needs, until contract.
- Contract steps that remove the compatibility artefacts are usually online as well. They share the
  construction with the expand step. The 2.0.0 primary key work derives each contract operation from its
  expand operation to guarantee the two stay symmetric.

## Checklist

- [ ] Heavy work is in `Execute` (autocommit). Attach and swap are in `Finalize`.
- [ ] The classifier covers invalid index, valid unattached index, attached constraint, and mixes.
- [ ] Lock timeout is set in `Finalize`.
- [ ] Re-running after a crash between any two steps completes correctly. Test it, see
      [failure scenarios](../migration-testing/failure-scenarios.md).
