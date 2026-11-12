# Upgrade lifecycle

## Timeline

```text
 Phase      Expand            Handoff            Migrate          Rollback window     Contract
 Operator   initialize        start new binary   apply migrate    observe, verify     apply contract
            apply (expand)    shift traffic,     (optionally per                       (explicit)
                              drain old binary   deployment)
 Store      expanding ->      expanded           migrating ->     contract_ready      contracting ->
            expanded                             contract_ready                       complete
 Old binary serving           may serve          refused          refused             refused
 New binary refused           serving            serving          serving             serving
```

- During Expand only the old binary serves. The new binary is refused (`target_not_ready`).
- At `expanded` the gate admits both. This is the handoff window.
- From `migrating` on, the old binary is refused (`source_retired`).
- Between `contract_ready` and the contract command is the rollback window. Complete rollback is still
  allowed. Starting contract closes it for any contract operation without a rollback operation.

## What each phase may do

| Phase | Who serves while it runs | Allowed | Not allowed |
|---|---|---|---|
| Expand | Old binary | Changes the old binary cannot notice: new tables, new nullable columns, widening a column, new non-conflicting indexes, backfilling new columns | Dropping, renaming, narrowing, NOT NULL without a default, new constraints old writes can violate, rewriting data old code reads |
| Migrate | New binary only | Rewriting data into the new representation. The new binary reads legacy and new shapes because rewriting is incremental | Anything the new binary cannot read mid way |
| Contract | New binary only, after the rollback window | Removing what nothing needs: legacy indexes, columns, attributes, tightening constraints | Anything not already proven unused |

Detailed placement rules: [migration-plan phase selection](../migration-plan/phase-selection.md).

## Product obligations

The migration only moves data. The product code of the new release must also:

- read every legacy representation that can exist from the `expanded` boundary until contract completes
  (**dual read**);
- run in **restricted mode** while the new binary serves a store that is not yet at `complete`, so it
  does not write legacy shaped data or rely on unmigrated data;
- write the new representation only.

See [migration-plan compatibility](../migration-plan/compatibility.md).

## Restricted operating mode

Set with `operating_mode: restricted`. It is configuration, not derived from migration state.

- HTTP: a path allow list stays open (OAuth2 and OIDC endpoints, flow execution, discovery, Console,
  health). Read only methods stay open on management resources. Everything else returns 503.
- Services: user registration, account recovery, administrative flows, entity provisioning, organization
  unit creation, account linking, user attribute update, credential update and passkey sign count update
  are fenced before they write.
- The startup gate refuses to start a binary that serves a store being migrated to its release unless
  restricted mode is on. This check runs only when `migration.startup_compatibility_check` is true
  (default false).

## Commands

All run as a one shot process without starting the HTTP server.

| Command | Effect |
|---|---|
| `thunderid migration initialize` | Create metadata tables and one state row per store |
| `thunderid migration status` | Compatibility report with the migration route. Exit non-zero if incompatible |
| `thunderid migration check [expand\|migrate\|contract]` | Read only readiness. No phase means expand then migrate |
| `thunderid migration apply` | Expand only, then stop |
| `thunderid migration apply expand\|migrate` | One phase. Migrate accepts repeatable `--deployment-id` |
| `thunderid migration apply contract` | The only path into contract |
| `thunderid migration resume` | Continue a failed or abandoned forward run |
| `thunderid migration rollback` | Roll back the failed operation |
| `thunderid migration rollback --phase <p>` | Roll back the failed phase |
| `thunderid migration rollback --revert-all` | Return changed stores to their accepted source level, before contract |

## Failure and recovery

| Situation | Durable state | Next step |
|---|---|---|
| Operation returns an error | Stores `failed`, run row names the operation and target | `resume`, or a rollback |
| Process killed | Stores `in_progress`, progress and checkpoints intact | `resume` |
| Preflight blocker | Nothing reserved | Fix data, run `check` again |
| Runner already active | Nothing | Retry later |
| Rollback interrupted | Run mode is rollback | Re-run `rollback`. Forward `resume` is refused |

Recovery relies entirely on verifiers. A resumed or re-applied operation must classify what it finds,
so every operation must handle partial and already-done states. See
[migration-operation verifiers](../migration-operation/verifiers-and-readiness.md).
