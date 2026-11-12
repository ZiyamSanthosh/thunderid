# Compatibility analysis

The migration engine guarantees order and recovery. It cannot make unsafe SQL safe. Compatibility is
the planner's job. Work through this file for every step of the plan.

## The two binaries and the windows

| Window | Serving | Consequence for the plan |
|---|---|---|
| Expand running | Old binary only | Result must be invisible to old code |
| Expanded (handoff) | Old and new together | Both read and write the same rows. Both representations coexist |
| Migrate running and after | New binary only | New code must tolerate half converted data |
| Contract ready until contract | New binary only | Legacy data still exists. Rollback is still possible |
| After contract | New binary only | Legacy representation is gone |

## Obligations for the product change

Write these into the plan so the developer implements them in the same release.

1. **Dual read.** From `expanded` until contract completes, every read of a changed representation must
   accept both shapes. Example: look up an entity type by handle, fall back to its legacy name.
2. **Single write.** New code writes only the new shape, in places it is allowed to write at all.
3. **No reliance on unmigrated data.** New code must not assume a backfill finished before contract.
4. **Restricted mode.** While the new binary serves a store that is not `complete`, the server should be
   in restricted mode. Check which write paths the change touches and confirm each is fenced. If the
   change adds a new mutating feature that writes legacy shaped data, add it to the feature gate.
5. **Declarative resources.** Data defined in resource files has no row. If a conversion or uniqueness
   check must consider it, plan a service-backed input (see
   [migration-operation cross-store and services](../migration-operation/cross-store-and-services.md)).

## Old binary safety checklist for Expand steps

For each Expand step, answer yes to all:

- [ ] Every statement the old binary runs on the touched tables still succeeds.
- [ ] No `ON CONFLICT` target of the old binary loses its unique index. If a key changes, keep a legacy
      unique index on the old columns until contract.
- [ ] A new constraint cannot be violated by a row the old binary can write. NULL in a unique column is
      safe. A new NOT NULL or a CHECK is not.
- [ ] Added columns are nullable or have a constant default.
- [ ] Column types only widen.
- [ ] Locks are short: `lock_timeout` set, long work online or batched.
- [ ] Foreign keys added to existing tables are created `NOT VALID`, then validated, so the add does not
      scan under a strong lock.

## Catch-up

A data step that runs in Expand converts the rows that exist at that moment. The old binary keeps
serving until it is drained, so it can create or change rows afterwards in the old representation.

Plan a catch-up when all of these are true:

- the step runs in Expand;
- the old binary can still create or modify the affected rows afterwards;
- a later step or contract needs every row converted.

A catch-up is the same operation registered again at the start of Migrate under a new id. It skips
itself when nothing is left, because its pre-verification returns `target`. It needs no rollback of its
own when the Expand rollback clears everything the forward step produces.

Do not plan a catch-up when the new binary, in restricted mode, cannot create such rows and the old
binary is guaranteed drained before Migrate. Say so in the plan.

## Ambiguity and conflicts

Conversions can hit data that cannot be converted safely: a value matching two possible meanings, a
generated key colliding with an existing one, a legacy value of an unknown shape.

- The operation must **not guess**. It classifies the situation as `invalid` with a finding that names
  the row and the reason, so readiness is `blocked` and the operator resolves it.
- Provide a remediation message in the readiness finding.
- Plan an explicit operator action only if the developer wants one. Do not add silent skips.

## Deployment scoped data

ThunderID can serve several deployments from one database. Rows carry a `DEPLOYMENT_ID`.

- Data steps resolve to one target per selected deployment, or one store wide target when no
  deployment is selected. Queries filter with `($1 = '' OR DEPLOYMENT_ID = $1)`.
- Schema steps are always store wide, regardless of a deployment selection.
- Uniqueness and generated keys are per deployment. Collision checks must scope the same way.
- A selective migrate finishes each store at the `expanded` boundary and records the deployment in
  `MIGRATION_DEPLOYMENT_STATE`. Contract needs the complete store.

## Cross store changes

- One operation writes one store. A change spanning two stores is two operations, in a deliberate order.
- An operation may read other stores it lists in `ReadStoreIDs`. Use this for lookups, for example
  reading config rows to decide what to write in the entity store.
- There is no distributed transaction. Each operation must be correct if the other did not run, and
  verifiers must recognise each store's own state.

## Reversibility

Decide per step, and write the answer in the plan. See [rollback-design.md](rollback-design.md).
Rule of thumb: additive schema changes need no rollback because the old binary ignores them. Data
changes the old binary would misread need a rollback operation. Contract steps are irreversible
unless a compensating rollback operation exists.
