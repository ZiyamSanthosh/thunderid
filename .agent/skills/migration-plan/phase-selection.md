# Phase, kind and mode selection

## Decision order

```text
1. Does the old binary keep working against the result?
     yes -> EXPAND
     no  -> 2
2. Does the new binary work on a mix of old and new representations while rows are converted?
     yes, and nothing old is removed -> MIGRATE
     the step removes or tightens something -> CONTRACT
3. Rows or structure?        rows -> data_change      structure -> schema_change
4. Short, one transaction?   yes  -> atomic
5. Needs DDL outside a transaction, or builds an index/constraint without a long lock?
                             yes  -> online     (schema_change only)
6. Many rows, per deployment, or must survive a restart mid way?
                             yes  -> resumable  (data_change only)
```

If a step is both structure and rows (for example add a column and fill it), split it into a schema
change and a data change. The plan validator rejects mixing the concepts anyway: resumable needs a
data change, online needs a schema change.

## What the old binary tolerates

The old binary writes and reads with the SQL of the previous release. Expand is safe only if all of
this holds after the step:

- every column it selects still exists with a compatible type and length;
- every `INSERT` it issues still succeeds (new columns are nullable or have a constant default);
- every `ON CONFLICT (cols)` it issues still finds a unique index on exactly `cols`;
- no new constraint can be violated by rows the old binary writes (a new unique constraint over a column
  the old binary leaves NULL is fine, because NULLs do not conflict);
- foreign keys it relies on still exist;
- its transactions are not blocked for long (see lock rules below).

If any item fails, the step belongs after the old binary is retired (Migrate or Contract), or needs a
compatibility construct such as a legacy unique index kept until contract.

## Catalog of change types

| Change | Phase | Kind | Mode | Rollback | Notes |
|---|---|---|---|---|---|
| New table, with its indexes | Expand | schema | atomic | none | `CREATE TABLE` and `CREATE INDEX` in one transaction. Verify the whole table shape |
| New nullable column | Expand | schema | atomic | none | `SET LOCAL lock_timeout`, then `ALTER TABLE ... ADD COLUMN` |
| New column with a constant default | Expand | schema | atomic | none | Metadata only on PostgreSQL 11 or later. Never a volatile default, it rewrites the table |
| Widen a column (longer VARCHAR) | Expand | schema | atomic | none | Increasing the limit does not rewrite. Never narrow in Expand |
| New non unique index on an existing table | Expand | schema | online | none | `CREATE INDEX CONCURRENTLY`. A failed build leaves an invalid index, handle it |
| New unique constraint, no risky old writes | Expand | schema | online | none | Build unique index concurrently, then `ADD CONSTRAINT ... UNIQUE USING INDEX` in a short transaction |
| Change primary key or add a scope column | Expand | schema | online | none | Build the new index concurrently, swap the key in a short transaction, keep a **legacy unique index** on the old columns for the old binary's `ON CONFLICT`, add or re-create foreign keys `NOT VALID` then validate |
| Fill a new column from old data | Expand | data | resumable | clear it, if old binary would misread it | Per deployment, batched. Plan a catch-up if the old binary can still write rows |
| Fill a new column from a cheap expression | Expand | data | atomic | as above | Only if one statement is fast on the largest table |
| Create links or derived rows from existing data | Expand | data | resumable | remove what it created | Rollback must only undo what the forward step wrote |
| Catch-up of an Expand data step | Migrate | data | resumable | covered by the Expand rollback | Same implementation, different operation id. Skips itself when nothing is left |
| Rewrite a stored representation (names to ids, JSON paths, attribute moves) | Migrate | data | resumable | restore the previous representation | New binary reads both shapes while this runs |
| Small one-row data fix in Migrate | Migrate | data | atomic | restore | Per deployment target |
| Data copy into another store | Expand or Migrate | data | resumable | delete the copy | One operation per store, reading the source store through `ReadStoreIDs` |
| Make a column NOT NULL | Contract | schema | atomic | usually none | Table scan under an exclusive lock, so keep `lock_timeout`. Readiness must block if NULLs remain |
| Drop a legacy index, constraint or column | Contract | schema | atomic or online | none | Irreversible, closes the rollback window |
| Remove legacy data (attribute, row) | Contract | data | resumable | none | Only after dual-read code is gone from the supported source versions |
| Tighten uniqueness to the new scope | Contract | schema | online | none | Drop the legacy compatibility index |
| Store changes only structurally in Expand | Expand | schema | atomic | none | Contract is added implicitly as a metadata-only step |

## Things a migration must never do

- Rename a table or column directly. Add the new name, backfill, dual read, then remove the old in
  Contract.
- Drop, narrow or tighten in Expand or Migrate.
- Change the meaning of an existing column in place.
- Write to a store other than the operation's own. Split across operations.
- Depend on a network call, a running service or application caches.
- Hold a long transaction or a long table lock.
- Guess when conversion is ambiguous.

## Locking rules

- Statements that need a table lock start with `SET LOCAL lock_timeout = '2s'` inside the transaction.
  A busy table then fails fast and the operation is retried, instead of queueing behind traffic and
  blocking everyone behind it.
- Anything heavy runs in `online` mode so it is not inside a transaction.
- Short constraint attach steps run in `Finalize`, which is a transaction, so `SET LOCAL` applies.
- `SET LOCAL` has no effect in `Execute` of an online operation, because that runs in autocommit.

## Ordering inside a phase

List order is execution order. Express real dependencies two ways:

1. Put the dependency earlier in the same list.
2. Make the later operation return `deferred` readiness naming the earlier one, so `check` can still
   pass before the earlier operation has run.

A deferred reference must point to an operation that **precedes** the current one in the whole plan.
Across phases the order is expand, migrate, contract.

Operations of different stores in one phase run in list order, one after another. Stores finalize
independently, each after its own last operation of the phase.

## Per store phase shapes

- A store with operations in all three phases moves complete, expanded, contract_ready, complete.
- A store with only Expand operations gets an implicit metadata-only contract.
- A store whose first operation is in Migrate starts its Migrate from a clean store. This works but the
  `migration apply` default (expand only) cannot reach a serving boundary for it, so the safe workflow
  rejects such a plan. Prefer at least one Expand operation for every changed store.

## Examples from release 2.0.0

| Step | Phase, kind, mode |
|---|---|
| Add a nullable handle column to the entity type table | Expand, schema, atomic |
| Create a new feature table and its index | Expand, schema, atomic |
| Widen a reference column | Expand, schema, atomic |
| Unique index on (handle, category, deployment), attached as constraint | Expand, schema, online |
| Deployment scoped primary keys, with compatibility indexes | Expand, schema, online |
| Backfill handles from legacy names | Expand, data, resumable, with rollback |
| Repeat the backfill for rows created after it | Migrate, data, resumable |
| Rewrite entity type references in several tables | Migrate, data, resumable, with rollback |
| Make the handle column NOT NULL and drop name uniqueness | Contract, schema, atomic |
| Drop the compatibility indexes | Contract, schema, online |
