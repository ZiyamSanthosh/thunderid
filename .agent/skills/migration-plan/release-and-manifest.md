# Release package, manifest and registration

## What a release package is

One directory per release under `backend/internal/system/migration/releases/`, named after the
version with underscores, for example `v2_0_0`. It is a Go package that holds:

| File | Purpose |
|---|---|
| `release-manifest.json` | Embedded manifest: release version, metadata version, per store levels |
| `release.go` | `Dependencies`, `Definition()`, `DefinitionWithDependencies()` and `migrationPlan()` that builds the ordered plan |
| `NN_name.go` | One operation per file. `NN_name_rollback.go` for its rollback operation |
| Helper files | Shared builders: table specs, primary key operations, JSON rewriting, declarative sources |
| `*_test.go` | Unit tests next to each file |

`current/current.go` selects the release compiled into the binary and exposes the catalog of manifests.

## Existing release or a new one?

A release is **immutable once shipped**. Operators may already have run it.

- The release is still in development (no published tag or release for that version): add the new
  operations to its package.
- The release has shipped: create a new release package. Never edit a shipped plan or manifest.

Ask the developer which case applies. Look at `version.txt`, git tags and the release notes if unsure.
A test compares the manifest's release version with `version.txt`, so the two move together.

## Manifest rules

```json
{
  "manifestVersion": 1,
  "releaseVersion": "2.1.0",
  "migrationMetadataVersion": 2,
  "migrationDocumentation": "https://thunderid.dev/docs/migrations",
  "stores": [
    {"id": "configdb", "acceptedSourceLevel": "2.0.0", "requiredLevel": "2.1.0", "changed": true},
    {"id": "entitydb", "acceptedSourceLevel": "2.0.0", "requiredLevel": "2.0.0", "changed": false},
    {"id": "runtime_transient", "acceptedSourceLevel": "1.0.0", "requiredLevel": "1.0.0", "changed": false},
    {"id": "runtime_persistent", "acceptedSourceLevel": "2.0.0", "requiredLevel": "2.0.0", "changed": false}
  ]
}
```

- All four stores must be present, once each.
- `releaseVersion` and every level are canonical semantic versions (pre-release and build parts allowed).
- **Changed store:** `acceptedSourceLevel` differs from `requiredLevel`, and `requiredLevel` equals
  `releaseVersion`. It needs at least one operation.
- **Unchanged store:** `acceptedSourceLevel` equals `requiredLevel`. It may not have operations.
  Its level stays at whatever it was, which can be older than the release version.
- `acceptedSourceLevel` of a changed store equals the `requiredLevel` the previous release left that
  store at. This chain is how the route resolver finds the path from an old database to this binary.
- Bump `migrationMetadataVersion` only when the four metadata tables themselves change. Doing so is an
  engine change, see [migration-engine](../migration-engine/SKILL.md).
- Unknown JSON fields are rejected.

A store becomes `changed` the moment any operation targets it. Flip the manifest in the same change
that adds its first operation, and remove the flag again if its last operation is removed.

## Plan registration

In `release.go`:

1. Construct each operation in `migrationPlan`, returning on error.
2. Add it to exactly one of `Expand`, `Migrate`, `Contract`. List order is execution order.
3. Add each rollback operation to `Rollback`. Its `ForwardOperationID()` must name an operation in the
   plan, in the same store and of the same kind.
4. Pass dependencies through the constructor, never through globals.
5. `engine.NewReleaseDefinition(manifestData, plan)` validates the manifest and the plan together.
   Construction fails loudly, so run the unit tests after every registration.

Dependencies for service backed operations are added as narrowly typed fields on the release's
`Dependencies` struct only when an operation uses them. The seam runs from `release.go` through
`current` to `cmd/server`. See [migration-service-operation](../migration-service-operation/SKILL.md).

## Numbering and naming

| Item | Convention | Example |
|---|---|---|
| Operation file | `NN_snake_name.go`, `NN` is a two digit authoring number unique in the release | `02_add_entity_type_handle.go` |
| Rollback file | same name plus `_rollback` | `04_backfill_entity_type_handle_rollback.go` |
| Operation id | `<release>/<store>/<NN>_<snake_name>` | `2.0.0/configdb/02_add_entity_type_handle` |
| Rollback id | forward id plus `_rollback` | `.../04_backfill_entity_type_handle_rollback` |
| Catch-up id | forward id plus `_catch_up`, or its own number | `.../04_backfill_entity_type_handle_catch_up` |
| Query id | `MIG-<STORE>_MGT-<n>`, unique across the whole release package | `MIG-CFG_MGT-05` |

Store prefixes in query ids are `CFG` (configdb), `ENT` (entitydb) and `RTP` (runtime persistent). Follow
the same pattern for runtime transient if it ever gets an operation. Numbers need not be contiguous but must never repeat. A test fails on
duplicates. Choose the next unused number by listing the existing ids.

The authoring number is not the execution order. Execution order is the list order in the plan.

## Current selection and catalog

`current.Definition()` returns the release this binary ships. `current.Catalog()` returns the manifests
the binary knows, ordered oldest first. When you add a release:

1. Import the new release package in `current/current.go`.
2. Return **every** supported manifest from `Catalog()`, old releases first, so the compatibility gate
   can name the next release to install for any supported starting point.
3. Keep the old release package in the tree. It is part of the catalog and of history.

## Documentation and tests to touch

- Release unit tests: plan size per phase, ids and order (`release_test.go` style), manifest levels.
- A query id uniqueness test already covers duplicate ids.
- If the repository keeps an operation catalog document, add an entry with purpose, source and target
  state, readiness, execution, verification, rollback and dependencies.
- Integration suite registration, see [migration-testing](../migration-testing/SKILL.md).

## Holding an operation back

Existing practice keeps a finished operation out of the plan with a comment until the matching product
change merges. Use it only when unavoidable. An operation in the plan runs for every operator on
upgrade, so merge the operation and the product change together whenever possible.
