# SaaS continuous-deployment migration model

## Status

This document records a proposed migration model for the ThunderID SaaS deployment workflow. It is
not implemented yet and does not replace the current release-based migration model.

The immediate priority remains implementing and qualifying realistic migration operations. Revisit
this proposal after the operation model and integrated PostgreSQL ZDM workflow are stable.

## Context

The planned SaaS environment builds the ThunderID `main` branch once per day, creates a timestamped
container image and deploys that image to the cloud environment. These deployments do not depend on
published ThunderID releases.

Any persisted-data change merged into `main` must include the migration operations required to move
the live databases safely. Most daily builds will not contain a database change and must not create
an artificial database migration merely because a new image was built.

## Decision direction

Separate artifact identity from database compatibility and migration identity.

| Identity | Example | Advances when |
|---|---|---|
| Artifact version | `main-20261013T020000Z-a1b2c3d` | Every daily build |
| Database contract | `c000007` | A persisted representation changes |
| Migration revision | `m000007` | Operations are added to reach a new contract |

The artifact version should contain both the build timestamp and Git revision. It is useful for
deployment tracking and audit, but it must not be used as the database migration level.

Multiple artifacts can target the same database contract:

```text
Monday artifact     main-20261012T020000Z-abc123   contract c000006
Tuesday artifact    main-20261013T020000Z-def456   contract c000006
Wednesday artifact  main-20261014T020000Z-789abc   contract c000007
Thursday artifact   main-20261015T020000Z-234def   contract c000007
```

Only the Wednesday artifact introduces a database transition.

## Why timestamps must not be migration levels

A daily build timestamp does not describe a database change:

- most builds will use the existing database representation;
- the same commit may be rebuilt with another timestamp;
- two artifacts can differ without requiring migration;
- artifact rollback does not always require database rollback; and
- clock-based ordering does not describe migration dependencies or compatibility.

The timestamp and Git revision identify the artifact. Stable contract and migration identifiers
describe the database.

## Proposed repository model

Migration ownership should move from product-release packages to immutable database transitions:

```text
backend/internal/system/migration/
  migrations/
    m000001/
      manifest.json
      plan.go
      operations/
    m000002/
      manifest.json
      plan.go
      operations/
```

The exact folder layout can be finalized during implementation. The important property is that an
existing migration revision becomes immutable after it is merged and deployed.

A migration manifest would describe a database transition rather than a product release:

```json
{
  "manifestVersion": 1,
  "migrationID": "m000007",
  "sourceContract": "c000006",
  "targetContract": "c000007",
  "migrationMetadataVersion": 2,
  "stores": [
    {
      "id": "configdb",
      "acceptedSourceLevel": "cfg000004",
      "requiredLevel": "cfg000005",
      "changed": true
    }
  ]
}
```

The target artifact would separately expose its build identity and required database contract:

```json
{
  "artifactVersion": "main-20261013T020000Z-a1b2c3d",
  "gitRevision": "a1b2c3d",
  "databaseContract": "c000007"
}
```

Per-store levels remain useful because one migration revision may change only a subset of the four
logical stores.

## Daily deployment workflow

For every daily image, the SaaS deployment workflow should:

1. Build an immutable image tagged with timestamp and Git revision.
2. Read the database contract compiled into that image.
3. Compare the target contract with the contract recorded in each database.
4. Perform an ordinary rolling deployment when no database transition is required.
5. Resolve pending migration revisions from the compiled catalog when a database is behind.
6. Enable restricted mode on serving source instances when the migration requires write fencing.
7. Run readiness checks and Expand from the target image.
8. Start target instances in restricted mode after the required stores reach `expanded`.
9. Shift traffic and drain source instances.
10. Run Migrate while target instances serve with the feature-specific legacy-read behavior.
11. Run Contract explicitly after the rollback boundary is accepted.

The normal startup compatibility gate remains mandatory. Timestamped artifact deployment must not
bypass migration simply because the product was not assigned a public release version.

## Pull-request contract

A change to a persisted representation should include:

- a new immutable migration revision;
- source and target store levels;
- expand, migrate and contract operations as applicable;
- operation readiness checks;
- pre-verification and post-verification;
- rollback operations or an explicit rollback boundary;
- PostgreSQL integration coverage;
- mixed-version compatibility analysis; and
- lock and resource-impact analysis.

CI should enforce at least these rules:

- an existing migration revision cannot be modified;
- a schema-script change requires a database-contract advancement;
- every changed store has a migration plan;
- the artifact contract points to the migration catalog head;
- every forward operation has readiness and verification;
- every supported provider has an operation implementation; and
- the complete catalog contains a valid route to the target contract.

Ordinary application changes do not require a migration revision.

## Required engine changes

The current implementation couples the migration release version to changed-store levels:

```text
releaseVersion = changed store requiredLevel
```

It also verifies that `version.txt` matches the selected migration release. Supporting daily SaaS
artifacts requires that coupling to be removed.

The implementation will need to:

1. Replace or reinterpret `ReleaseVersion` as a migration or contract identifier.
2. Stop requiring changed-store levels to equal the product artifact version.
3. Store the timestamped artifact version only as execution and deployment audit information.
4. Make `current` declare the artifact's target database contract independently.
5. Compile every supported immutable migration transition into an ordered catalog.
6. Resolve routes using migration identifiers rather than public product releases.
7. Load the executable plan associated with each pending migration identifier.
8. Replace semantic-version validation for database levels with canonical contract-identifier
   validation.

Existing metadata level columns are strings, so identifiers such as `c000007` and `cfg000005` may
not require a database type change. This must be verified during implementation.

## Pending-transition constraint

The current coordinator is safest when it handles one adjacent database-contract transition at a
time. Multiple pending transitions may each contain contract work that cannot run while the original
source artifact is still serving.

The initial SaaS rule should therefore be:

> Only one uncontracted database-contract transition may be present in the deployment train.

Under this rule:

- ordinary non-database changes can continue merging;
- one database-contract transition can be pending deployment;
- another database-contract transition waits until the previous transition is contracted; and
- one transition may still contain multiple operations across multiple logical stores.

CI or the deployment controller must enforce this rule.

If this becomes too restrictive, a future migration-train capability could compose pending
revisions by phase:

```text
expand m000007
expand m000008
start restricted target
drain source
migrate m000007
migrate m000008
contract m000008
contract m000007
```

This requires additional dependency validation, compatibility rules, durable per-revision state and
reverse-order rollback. The current coordinator must not be assumed to support it.

## Artifact rollback

Artifact rollback and database rollback are separate decisions:

- artifacts targeting the same database contract can normally be exchanged through a rolling
  deployment;
- before contract, an older artifact may remain compatible and explicit migration rollback may be
  available;
- after contract, an artifact requiring the older representation must be rejected by the startup
  compatibility gate; and
- recovery after contract requires a compatible forward artifact or an explicitly supported
  database rollback path.

## Initial implementation recommendation

When this proposal is implemented:

1. Use timestamp plus Git revision for artifact identity.
2. Introduce stable monotonically increasing database-contract identifiers.
3. Add immutable migration revisions only for persisted-data changes.
4. Enforce one uncontracted transition at a time.
5. Use the same target image for standalone migration and later request serving.
6. Add CI enforcement that database changes and their migration operations merge together.
7. Let the deployment controller select ordinary rollout or migration workflow by comparing
   database contracts.

## Deferred work

No implementation change is authorized by this document. The current priority is completing and
qualifying realistic migration operations. The artifact and contract decoupling described here
should be scheduled as a separate cross-cutting change after that work.
