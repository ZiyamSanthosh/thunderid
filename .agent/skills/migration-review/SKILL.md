---
name: migration-review
description: Review a ThunderID migration change before it merges, or audit an existing operation, plan or release. Use when reviewing a pull request that adds or changes migration operations, release manifests, rollback operations or catch-ups, or when asked whether a migration is safe for zero-downtime upgrade. Gives a layered checklist (plan, compatibility, operation, SQL, rollback, tests, release) and the defect patterns that have actually occurred in this codebase.
---

# Reviewing a Migration

Review in layers. A wrong phase makes a perfect operation unsafe, so start at the plan and work down.
Report each finding with severity, the file and line, a **concrete failing scenario** (state and inputs
leading to a wrong result), and the fix. Drop findings you cannot justify from the code.

Background: [migration-system](../migration-system/SKILL.md). Rules being checked come from
[migration-plan](../migration-plan/SKILL.md), [migration-operation](../migration-operation/SKILL.md)
and [migration-testing](../migration-testing/SKILL.md).

## Layer 1: Plan and phase

- [ ] Each step is in the right phase. Expand is invisible to the old binary. Migrate needs only the new
      binary. Contract removes or tightens only things nothing uses.
- [ ] No drop, rename, narrow, NOT NULL or new violating constraint in Expand or Migrate.
- [ ] Each operation writes exactly one store. Cross store changes are separate operations.
- [ ] Kind and mode are valid for the work: resumable is data only, online is schema only.
- [ ] Heavy work is not atomic. A statement that scans or rewrites a large table is online or batched.
- [ ] A catch-up exists where the old binary can still create rows after an Expand data step, or the
      reason it cannot is stated.
- [ ] Every changed store has at least one Expand operation, so the safe workflow can reach a serving
      boundary.
- [ ] Ordering dependencies are explicit: earlier in the list, or deferred readiness naming a preceding
      operation.

## Layer 2: Product compatibility

- [ ] The product change reads both representations from `expanded` until contract (dual read).
- [ ] The new binary writes only the new representation.
- [ ] Every write path that could create legacy shaped data is fenced in restricted mode, or the change
      explains why it is safe.
- [ ] No code path assumes a backfill is finished before contract.
- [ ] Declarative resources, which have no rows, are considered in conversions and uniqueness checks.
- [ ] Old binary SQL still works after each Expand step: columns it reads, inserts it issues,
      `ON CONFLICT` targets, foreign keys.

## Layer 3: Operation correctness

- [ ] `PreVerify`, `PostVerify` and readiness share one classifier.
- [ ] The already migrated state returns `target` and is skipped, never an error.
- [ ] Partial states left by a crash between steps are recognised.
- [ ] Ambiguity returns `invalid` with a finding. The code never guesses.
- [ ] Every `invalid` result has a finding. Every `blocked` readiness has a finding with remediation.
      Every `deferred` readiness names operations that precede it.
- [ ] Reads of nullable columns do not use `QueryRow.String`, which errors on NULL and empty strings.
- [ ] Data queries filter by deployment and take it from the target. Uniqueness is per deployment.
- [ ] `ResolveTargets` returns store wide for schema changes and one target per deployment otherwise.
      Target ids are bounded in length.
- [ ] Resumable: keyset cursor, strictly advancing checkpoint, bounded batch, `Completed` exactly when
      nothing remains, per target checkpoint.
- [ ] Online: heavy work in `Execute`, short attach in `Finalize`, `SET LOCAL lock_timeout` where it
      takes effect (inside a transaction).
- [ ] Writes only through the execution access of the operation's own store.
- [ ] No logging, network, time, randomness or global state in the operation.

## Layer 4: SQL

- [ ] PostgreSQL text for every query. Parameters bound, never concatenated from input.
- [ ] Catalogue queries are scoped to `CURRENT_SCHEMA()` and compare names case insensitively.
- [ ] Table locking DDL begins with `SET LOCAL lock_timeout = '2s'` in a transaction.
- [ ] Index builds use `CONCURRENTLY` outside a transaction. Invalid leftovers are handled.
- [ ] Constraints on large tables are added `NOT VALID` then validated, or attached `USING INDEX`.
- [ ] No volatile default on an added column. Column types only widen in Expand.
- [ ] Legacy unique indexes needed by the old binary's `ON CONFLICT` remain until contract.
- [ ] Query ids are unique across the release package.

## Layer 5: Rollback

- [ ] Each operation's rollback status is decided and written down: operation, or none with the reason.
- [ ] A rollback operation shares store and kind with its forward operation and names the right id.
- [ ] It undoes only what the forward step produced and leaves independently created data alone.
- [ ] The restore is exact, or the limit is documented (NULL versus empty object, number precision).
- [ ] It is safe to run twice and after a partial rollback.
- [ ] Contract steps without rollback are intended to block revert-all, and the plan says so.

## Layer 6: Tests

- [ ] Definition, verifier (source, target, invalid), readiness and statement or helper unit tests exist.
- [ ] The release plan test pins sizes and order, and was updated.
- [ ] Integration fixture is in the **source** schema, covers edge rows, several deployments, and more
      than one batch for resumable operations.
- [ ] An old binary SQL compatibility check exists for Expand steps.
- [ ] Interruption, resume and rollback scenarios from
      [failure-scenarios](../migration-testing/failure-scenarios.md) are covered for the type.
- [ ] Separate and shared database layouts both pass. If Docker was not available the author says so.
- [ ] Failing tests were fixed, not weakened.

## Layer 7: Release and process

- [ ] A shipped release is untouched. New work is in a new release package.
- [ ] Manifest: changed stores advance and equal the release version, unchanged stores keep their level,
      accepted source level matches the previous release's required level.
- [ ] `current` returns every supported manifest from `Catalog()`.
- [ ] Operation catalog documentation updated if the repository keeps one.
- [ ] Commit message is a short imperative sentence, one commit per pull request.

## Defect patterns in this codebase

Look for these specifically.

Found and fixed during review of the engine and release 2.0.0:

| Pattern | What went wrong |
|---|---|
| Catch-up missing | A backfill in Expand left rows created by the old binary with no value. Later steps and contract would then fail. The fix repeats the backfill in Migrate |
| Resume ignored a state | A selective migrate finishes some stores at `expanded`. Resume rejected that state although rollback knew it |
| Abandoned versus failed | A guard checked only failed stores, so an interrupted rollback that left stores in progress slipped through |
| "At target" by elimination | A store at an unexpected level was reported at target only because it had no operations in the requested phase |
| Wrong target id | Schema operations in a selective run recorded the deployment id as their target, so failure and resume lookups never matched |
| Fixed boundary | Phase rollback returned every store to a fixed boundary instead of the one before its own first phase |
| Skipped failed store | Revert-all looked only for stores with progress rows and ignored a store that failed before writing any |
| Duplicate query ids | Copy paste reused ids across files. Harmless at run time, wrong in logs and mocks |

Risks flagged in audits that remain design decisions. Raise them when a change touches the area:

| Pattern | Risk |
|---|---|
| Conversion that blocks on any unrecognised legacy value with no operator escape | One odd row stops the whole upgrade |
| Classifying by shape alone | A numeric legacy value assigned to a provider by length could attach the wrong identity |
| Inexact rollback | A restore that turns NULL into an empty object, or removes data the product created itself |

Generic mistakes to check on every operation: a verifier that rejects the already migrated state, a
nullable column read with a non nullable accessor, and a data query without the deployment filter.

## Output format

```markdown
## Migration review: <change>

### Blocking
1. <file:line> <what is wrong>
   Scenario: <state and inputs, then the wrong result>
   Fix: <specific change>

### Should fix
...

### Questions
...

### Covered and fine
<short list of layers verified, so the author knows what was checked>
```

If a layer could not be checked (no Docker, no access to the old binary) say so rather than passing it.
