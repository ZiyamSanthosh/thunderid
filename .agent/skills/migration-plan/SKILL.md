---
name: migration-plan
description: Turn a developer's data or schema change into a ThunderID migration design before any code is written. Use when a feature changes a database table, column, index, constraint, stored attribute, JSON payload or data location and needs an upgrade path. Decides which store, which phase (expand, migrate, contract), which operation kind and interruption mode (atomic, online, resumable), what the new binary must still read, whether a catch-up or rollback operation is needed, and what the release manifest must say. Produces a migration plan the developer confirms.
---

# Planning a Migration

The output of this skill is a **migration plan**: a short document listing every operation with its
store, phase, kind, mode, verification, rollback and compatibility note. The developer confirms it
before [migration-operation](../migration-operation/SKILL.md) writes any code.

Background first: [migration-system](../migration-system/SKILL.md), especially
[lifecycle.md](../migration-system/lifecycle.md).

## Step 1: Understand the change

Do not guess. Ask the developer (or read the product change) until each answer is known. Skip
questions the change already answers.

1. **What changes in storage?** Tables, columns, indexes, constraints, JSON shape, attribute names,
   where data lives. Which store owns each (`configdb`, `entitydb`, `runtime_transient`,
   `runtime_persistent`)?
2. **What does the old binary do with that storage?** Reads, writes, `ON CONFLICT` on which columns,
   constraints it relies on. The old binary keeps serving during Expand and the handoff.
3. **What does the new binary do?** Which representation does it write? Does it still read the old one?
   (It must, until contract.)
4. **Is existing data affected?** How many rows can there be? Is the data deployment scoped
   (`DEPLOYMENT_ID` column)? Can old rows be converted deterministically, or can conversion fail or be
   ambiguous?
5. **Can the old binary create more of the affected data after the migration step ran?** If yes, a
   catch-up is needed.
6. **Is anything declared outside the database** (declarative resource files) that the conversion must
   consult? That is a service-backed input.
7. **Is the change reversible?** What happens if the operator wants to go back before contract?
8. **Does another store depend on it?** Cross-store reads force ordering, writes force one operation per
   store.

## Step 2: Break the change into steps

A single feature is usually several operations. Split by store, by phase, and by schema versus data.
One operation changes one store, in one phase, in one way. Use the catalog in
[phase-selection.md](phase-selection.md) to map each step to a pattern.

Common decompositions:

| Feature | Steps |
|---|---|
| New required column | Expand: add nullable column. Expand: backfill. Migrate: catch-up backfill. Contract: set NOT NULL |
| Rename an attribute or value | Expand: add new representation. Migrate: convert data. Contract: remove old representation |
| Change a key or uniqueness scope | Expand: build new unique index, swap key, keep a compatibility index. Contract: drop compatibility index |
| New feature table | Expand: create table and indexes. Nothing else |
| Remove an unused column | Contract only, after the new binary stopped using it |

## Step 3: Decide per step

For each step answer, in order. Details and the full table are in
[phase-selection.md](phase-selection.md).

1. **Phase.** Can the old binary run unchanged against the result? Expand. Does only the new binary
   understand the result? Migrate. Does it remove something old or new code may still read? Contract.
2. **Kind.** Structure is `schema_change`. Rows are `data_change`.
3. **Interruption mode.**
   - fits one short transaction: `atomic`
   - needs non-transactional DDL or builds an index: `online` (schema only)
   - many rows, long running, per deployment, or restartable by checkpoint: `resumable` (data only)
4. **Targets.** Whole store, or one target per deployment?
5. **Readiness.** What can be checked up front? What depends on an earlier operation (`deferred`)?
6. **Verification.** Exactly what do source, target and invalid look like?
7. **Rollback.** See [rollback-design.md](rollback-design.md).
8. **Catch-up.** See [compatibility.md](compatibility.md).

## Step 4: Check compatibility and product obligations

Walk [compatibility.md](compatibility.md). The result is a list of obligations for the product change:
dual reads, feature fencing, restricted mode, anything the new binary must not write yet.

## Step 5: Check release and manifest impact

Read [release-and-manifest.md](release-and-manifest.md). Decide whether the work goes into an existing
unreleased release or needs a new release package, whether a store becomes `changed`, and where each
operation is numbered and registered.

## Step 6: Write the plan

Use this template. Keep it factual and short.

```markdown
# Migration plan: <feature>

## Summary
<One paragraph. What changes in storage and why.>

## Compatibility
- Old binary during expand: <what it does with the touched storage>
- New binary until contract: <what it reads and writes>
- Restricted mode needed: <yes/no and why>

## Operations
| # | ID | Store | Phase | Kind | Mode | Targets | Rollback |
|---|----|-------|-------|------|------|---------|----------|
| 1 | <release>/<store>/<NN>_<name> | configdb | expand | schema_change | atomic | store | none, additive |

### <ID>
- Purpose:
- Source state:
- Target state:
- Invalid when:
- Readiness: ready when ... / deferred until <ID> / blocked when ...
- Execution:
- Idempotency and restart:
- Rollback: <operation id or "none", with the reason>
- Depends on:

## Catch-up
<Operations repeated in migrate for rows the old binary may still create, or "none".>

## Release and manifest
<Release version, stores becoming changed, files to add or update.>

## Risks and open questions
<Lock impact, row counts, ambiguous data, declarative inputs, anything unresolved.>
```

## Step 7: Confirm

Show the plan to the developer. Resolve every open question. Only then continue to
[migration-operation](../migration-operation/SKILL.md). If the developer changes the product change
later, update the plan first.

## Rules

- Never plan a destructive step (drop, rename, narrow, NOT NULL, new violating constraint) in Expand or
  Migrate.
- Never plan an operation that writes two stores. Split it.
- Never plan to edit a shipped release. Plan a new release.
- If the data conversion can be ambiguous, plan an operation that **fails with a clear finding** rather
  than guessing. Ambiguity is a blocker for the operator to resolve, not a decision for the code.
- If rows could be added by the old binary after the operation ran, plan the catch-up now.
- Prefer several small operations over one large one. Each is independently verified and recoverable.
