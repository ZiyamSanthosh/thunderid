---
name: migration-operation
description: Write or change a ThunderID migration operation in a release package. Use after a migration plan exists, when implementing an atomic SQL operation, an online schema operation, a resumable data operation, a rollback operation, a catch-up operation or a service backed operation. Covers the operation contract, verifiers (source, target, invalid), readiness checks, SQL and locking rules, deployment targets, checkpoints, naming, query ids and plan registration.
---

# Writing a Migration Operation

Prerequisite: an approved migration plan from [migration-plan](../migration-plan/SKILL.md). If there
is none, create one first. The plan fixes the store, phase, kind, mode, verification, rollback and
dependencies. This skill turns that into code.

Background: [migration-system](../migration-system/SKILL.md).

## Step 1: Pick the implementation form

| Plan says | Form | Playbook |
|---|---|---|
| schema change, short, one transaction | `engine.NewSQLOperation` with atomic mode | [atomic-sql.md](atomic-sql.md) |
| new table with expected shape | the release's table builder helper if one exists, else atomic SQL | [atomic-sql.md](atomic-sql.md) |
| data change, one quick statement per target | `engine.NewSQLOperation` atomic with a target resolver | [atomic-sql.md](atomic-sql.md) |
| schema change needing DDL outside a transaction (concurrent index, constraint swap) | custom type implementing `OnlineOperation` | [online.md](online.md) |
| data change over many rows, per deployment, restartable | custom type implementing `ResumableOperation` | [resumable.md](resumable.md) |
| undo of a data change | custom type implementing `RollbackOperation` | [rollback-operations.md](rollback-operations.md) |
| needs entities or settings declared outside the database | any of the above plus an injected provider | [cross-store-and-services.md](cross-store-and-services.md) |

Rules for the combination of kind and mode are in [concepts](../migration-system/concepts.md): resumable
needs a data change, online needs a schema change, a data change needs a target resolver, a schema
change must not have one.

## Step 2: Implement the contract

Every operation implements these. The engine calls them in this order for each target.

| Method | When | Must |
|---|---|---|
| `Definition()` | always | Return the same id, description, store, read stores, kind and mode. Return a copy of slices |
| `CheckReadiness()` (optional) | `check` and before execution, read only | Return ready, deferred or blocked. See [verifiers-and-readiness.md](verifiers-and-readiness.md) |
| `ResolveTargets(selection)` | before execution | Schema change: one store wide target. Data change: one target per selected deployment, or one store wide target |
| `PreVerify(target)` | before execution, read only | Classify `source`, `target` or `invalid`. `target` means skip, success |
| `Execute` / `ExecuteBatch` | execution | Change the data. Atomic and resumable batches run inside a transaction the engine owns |
| `Finalize` (online only) | after `Execute`, in a short transaction | Attach constraints, swap names |
| `PostVerify(target)` | after execution | Must return `target`, otherwise the operation fails and the transaction rolls back (atomic) |

Access is fenced. `ReadAccess` reads the declared stores. `ExecutionAccess` also writes, but only to
the operation's own store. Anything else is refused by the engine.

## Step 3: The rules that prevent most bugs

1. **Idempotent.** Running the operation when the target state already exists must succeed without
   change. Re-applying after a rollback relies on this.
2. **Verify, do not assume.** Classify from the catalogue or the data. Never infer from "the previous
   operation ran".
3. **Handle every partial state.** An interrupted online or resumable operation leaves something behind.
   The verifiers recognise it and the execution continues or cleans up.
4. **Never guess on ambiguity.** Return `invalid` with a finding naming the row and the reason.
5. **Bound everything.** Batches are bounded (existing operations use 100 rows). Transactions are short. Table locking
   DDL starts with `SET LOCAL lock_timeout = '2s'`.
6. **One writable store.** Cross store logic is two operations.
7. **Respect deployments.** Filter every data query with `($1 = '' OR DEPLOYMENT_ID = $1)`, derive the
   deployment from the target, and scope uniqueness checks the same way.
8. **Provider explicit SQL.** Provide PostgreSQL text for every query. No SQL built from untrusted input.
9. **No business logic dependencies by default.** Reach services only through an injected narrow
   interface.
10. **Errors carry context.** Wrap with `fmt.Errorf("<action>: %w", err)` naming what failed.

Conventions for ids, files and SQL style: [conventions.md](conventions.md).

## Step 4: Register

In the release package's `release.go`:

1. Construct the operation (and its rollback) in `migrationPlan`.
2. Add it to the correct phase list at the position the plan gave. Add the rollback to `Rollback`.
3. Update the manifest if this is the store's first operation (store becomes `changed`).
4. Update the release test that pins plan sizes and order.

Details: [release-and-manifest](../migration-plan/release-and-manifest.md).

## Step 5: Test

Follow [migration-testing](../migration-testing/SKILL.md). An operation is not finished without unit
tests for definition, verifiers, readiness and SQL, an integration test against a real PostgreSQL, and a
check that it survives interruption.

## Step 6: Review yourself

Run the checklist in [migration-review](../migration-review/SKILL.md) before asking for review.

## Common mistakes

- Verifier returns `invalid` for the already migrated state, so a resumed or re-applied run fails.
- `QueryRow.String` used on a nullable column. It errors on NULL and on empty strings. Use the typed
  accessors only for columns that are guaranteed non empty, and read nullable ones another way.
- Using `SET LOCAL` in the `Execute` of an online operation. It is in autocommit and has no effect.
  Use it in `Finalize` or in atomic operations.
- Data query without the deployment filter, so a selective run migrates other deployments.
- Checkpoint that is not strictly ahead of the batch's rows, so a resume repeats or skips rows.
- Unique id reused across two query definitions. A test fails, but only after you have wasted time.
- A new unique constraint or NOT NULL in Expand that the old binary can violate.
