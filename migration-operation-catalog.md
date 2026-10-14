# Migration operation catalog

This catalog records the purpose and operational contract of every migration operation currently
registered for ThunderID 2.0.0. The Go definitions remain the executable source of truth. Update
this file in the same change whenever an operation is added or its readiness, execution,
verification, rollback, or compatibility behavior changes.

## Reading the catalog

- **Source state** is the accepted database state before an operation runs.
- **Target state** is the database state the operation must produce.
- **Readiness** decides whether the operation may run, must wait for a dependency, or must be
  blocked for manual reconciliation.
- **Verification** is used before and after execution. A source result means work remains, a target
  result means the operation is already satisfied, and an invalid result means the schema or data
  is outside the accepted transition.
- **Atomic** operations run as one transaction. **Resumable** operations process bounded batches
  and persist checkpoints.
- Contract operations are irreversible boundaries. They run only after source instances are
  drained and rollback authorization has ended.
- The current migration implementation supports PostgreSQL only.

## Release plan at a glance

| Phase | No. | Store | Operation | Mode | Rollback | Current status |
| --- | ---: | --- | --- | --- | --- | --- |
| Expand | 01 | `runtime_persistent` | Remove revoked-token reason check | Schema, atomic | None | Implemented |
| Expand | 02 | `configdb` | Create authorization PDP connection table | Schema, atomic | None | Implemented |
| Expand | 03 | `configdb` | Add nullable entity-type handle | Schema, atomic | Schema retained | Implemented |
| Expand | 04 | `entitydb` | Expand `ENTITY.TYPE` to 100 characters | Schema, atomic | None | Implemented |
| Expand | 08 | `entitydb` | Add `VALUE` to the `ENTITY_IDENTIFIER` primary key | Schema, atomic | None | Expand only implemented |
| Expand | 09 | `runtime_persistent` | Create `ENTITY_RUNTIME_DATA` | Schema, atomic | None | Implemented |
| Expand | 10 | `entitydb` | Add temporary `ACTIVE` default to `ENTITY.STATE` | Schema, atomic | Not required | Implemented |
| Expand | 12 | `runtime_persistent` | Add nullable SSO session properties | Schema, atomic | None | Implemented |
| Expand | 13 | `configdb` | Create notification template table | Schema, atomic | None | Implemented |
| Migrate | 05 | `configdb` | Backfill entity-type handles | Data, resumable | Operation 05 rollback | Implemented |
| Migrate | 06 | `configdb` | Add entity-type handle uniqueness | Schema, atomic | None | Implemented |
| Contract | 07 | `configdb` | Finalize entity-type handles | Schema, atomic | None | Implemented |
| Contract | 11 | `entitydb` | Remove legacy `ENTITY.STATE` | Schema, atomic | None | Implemented |

Numeric identifiers are append-only identifiers, not phase ordering. The release plan in
[`release.go`](backend/internal/system/migration/releases/v2_0_0/release.go) defines execution order.

## Operation records

### 01: Remove revoked-token reason check

- **ID:** `2.0.0/runtime_persistent/01_remove_revocation_reason_check`
- **Definition:**
  [`01_remove_revocation_reason_check.go`](backend/internal/system/migration/releases/v2_0_0/01_remove_revocation_reason_check.go)
- **Purpose:** Remove the fixed check constraint from `REVOKED_TOKEN.REVOCATION_REASON` so the
  target can use reason values that are not encoded in the old database constraint.
- **Source state:** Exactly one non-null `VARCHAR(30)` column and exactly one check constraint named
  `REVOKED_TOKEN_revocation_reason_check` on that column.
- **Readiness:** Ready when the exact source state exists or when the constraint is already absent.
  Any unexpected column definition, constraint name, or constraint count blocks execution.
- **Execution:** Set a two-second local lock timeout, then drop the discovered legacy constraint.
- **Verification:** Target requires the exact column definition with no check constraint.
- **Compatibility:** This is a loosening change. Source instances can continue writing their known
  reason values while the target may accept a wider vocabulary.
- **Rollback:** None. Reintroducing the old constraint could fail after newer reason values exist.
- **Pending concerns:** Confirm that any reason values written before source drain remain readable
  by the source application, even though the database accepts them.

### 02: Create authorization PDP connection table

- **ID:** `2.0.0/configdb/02_create_authorization_pdp_connection`
- **Definition:**
  [`02_create_authorization_pdp_connection.go`](backend/internal/system/migration/releases/v2_0_0/02_create_authorization_pdp_connection.go)
- **Purpose:** Add storage for authorization PDP connection configurations.
- **Source state:** `AUTHORIZATION_PDP_CONNECTION` is absent and the intended index name is unused.
- **Readiness:** Ready for the absent source state or an exact target table. A partial table, a
  non-table relation, a conflicting index name, or any schema mismatch blocks execution.
- **Execution:** Create the eight-column table and the unique index on
  `(DEPLOYMENT_ID, TYPE, NAME)`.
- **Verification:** Checks relation kind, ordered column definitions and defaults, the primary key
  on `ID`, and the exact unique index definition.
- **Compatibility:** Additive. Source instances do not depend on the new table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 03: Add entity-type handle column

- **ID:** `2.0.0/configdb/03_add_entity_type_handle`
- **Definition:**
  [`03_add_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/03_add_entity_type_handle.go)
- **Purpose:** Introduce the nullable `ENTITY_TYPES.HANDLE VARCHAR(100)` compatibility column needed
  before handles are populated.
- **Source state:** `ENTITY_TYPES.NAME` is the accepted non-null `VARCHAR(100)` column and `HANDLE`
  is absent.
- **Readiness:** Ready when the source state exists or the exact nullable handle column already
  exists. An unexpected name or handle definition blocks execution.
- **Execution:** Set a two-second local lock timeout, then add the nullable column.
- **Verification:** Target requires the accepted name column and nullable `HANDLE VARCHAR(100)`.
- **Compatibility:** Additive. Source instances ignore the column.
- **Rollback:** The schema column is retained. The paired operation 05 rollback removes handle
  values when returning to the source version.
- **Dependencies:** Operation 05 depends on this column.
- **Pending concerns:** Entity-type mutations must follow the migration's applicable write controls
  while operation 05 establishes a stable backfill result.

### 04: Expand entity type length

- **ID:** `2.0.0/entitydb/04_expand_entity_type_length`
- **Definition:**
  [`04_expand_entity_type_length.go`](backend/internal/system/migration/releases/v2_0_0/04_expand_entity_type_length.go)
- **Purpose:** Increase `ENTITY.TYPE` from `VARCHAR(50)` to `VARCHAR(100)`.
- **Source state:** Exactly one non-null `VARCHAR(50)` `ENTITY.TYPE` column.
- **Readiness:** Ready for the exact source or target definition. Missing, nullable, non-varchar, or
  any other length blocks execution.
- **Execution:** Set a two-second local lock timeout, then alter the column type.
- **Verification:** Target requires non-null `VARCHAR(100)`.
- **Compatibility:** The wider column accepts every source value.
- **Rollback:** None. Shrinking could reject target-written values.
- **Pending concerns:** None specific to this operation.

### 05: Backfill entity-type handles

- **ID:** `2.0.0/configdb/05_backfill_entity_type_handle`
- **Definition:**
  [`05_backfill_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle.go)
- **Purpose:** Create valid, unique handles from legacy entity-type names.
- **Source state:** The exact name and nullable handle columns exist. Handles may be entirely absent
  or partially populated, but every populated handle must already be valid and unique within its
  deployment and category.
- **Readiness:** Deferred until operation 03 when `HANDLE` is absent. Ready when the accepted schema
  exists and current data is valid. Invalid or duplicate populated handles block execution.
- **Execution:** Process at most 100 null-handle rows per batch. Generate normalized handles,
  resolve collisions deterministically, update rows, and checkpoint the last processed ID. The
  operation can run for selected deployments or for all deployments.
- **Verification:** Target requires every entity type in scope to have a valid unique handle. An
  empty scope is already target. A partial valid backfill remains source and is resumable.
- **Compatibility:** The legacy name representation and uniqueness remain in place through the
  mixed-version window.
- **Rollback:** Paired with `05_backfill_entity_type_handle_rollback`.
- **Dependencies:** Operation 03 must provide the column. Operation 06 follows the completed
  backfill.
- **Pending concerns:** Batch size and pacing are fixed. Production resource limits and configurable
  pacing remain migration-engine backlog items. Relevant entity-type writes must not race the
  backfill.

### 05 rollback: Remove entity-type handle representation

- **ID:** `2.0.0/configdb/05_backfill_entity_type_handle_rollback`
- **Definition:**
  [`05_backfill_entity_type_handle_rollback.go`](backend/internal/system/migration/releases/v2_0_0/05_backfill_entity_type_handle_rollback.go)
- **Purpose:** Return entity-type data to the source representation before source rollback.
- **Source state for rollback:** One or more entity types in scope have a non-null handle.
- **Execution:** Clear handles in batches of at most 100 rows and checkpoint the last processed ID.
- **Verification:** Rollback target requires every handle in scope to be null.
- **Scope:** Clears the complete handle representation, including values not originally generated
  by the forward backfill. This is intentional because the source representation has no handles.
- **Compatibility:** The nullable column and target uniqueness constraint can remain because null
  handles do not prevent the source schema from serving.
- **Availability:** Allowed only before contract authorization. It is not a path back from operation
  07.
- **Pending concerns:** Applicable entity-type writers must remain controlled during rollback so
  they cannot repopulate handles concurrently.

### 06: Add entity-type handle uniqueness

- **ID:** `2.0.0/configdb/06_add_entity_type_handle_uniqueness`
- **Definition:**
  [`06_add_entity_type_handle_uniqueness.go`](backend/internal/system/migration/releases/v2_0_0/06_add_entity_type_handle_uniqueness.go)
- **Purpose:** Enforce unique `(HANDLE, CATEGORY, DEPLOYMENT_ID)` values while retaining legacy
  `(NAME, CATEGORY, DEPLOYMENT_ID)` uniqueness.
- **Source state:** Exact name and nullable handle columns, legacy name uniqueness present, target
  handle uniqueness absent, and the handle backfill complete.
- **Readiness:** Deferred until operations 03 and 05 when their results are not yet available.
  Ready when both uniqueness constraints already coexist. Unexpected columns or constraints block
  execution.
- **Execution:** Set a two-second local lock timeout, then add the target unique constraint.
- **Verification:** Target requires both legacy name uniqueness and target handle uniqueness.
- **Compatibility:** Both source and target invariants coexist during mixed-version serving.
- **Rollback:** No separate schema rollback. Clearing handles through operation 05 rollback leaves
  the nullable target uniqueness constraint harmless to the source.
- **Pending concerns:** Adding the constraint validates existing data and needs a table lock within
  the configured timeout.

### 07: Finalize entity-type handles

- **ID:** `2.0.0/configdb/07_finalize_entity_type_handle`
- **Definition:**
  [`07_finalize_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/07_finalize_entity_type_handle.go)
- **Purpose:** Make handles mandatory and remove legacy name uniqueness after the source version is
  drained.
- **Source state:** Exact name and nullable handle columns, no null handles, and both legacy name and
  target handle uniqueness constraints present.
- **Readiness:** Ready for the exact contract source state or exact finalized target state. Null
  handles or unexpected constraint combinations block execution.
- **Execution:** Set a two-second local lock timeout, make `HANDLE` non-null, then drop the legacy
  name uniqueness constraint.
- **Verification:** Target requires non-null handles, target handle uniqueness, and no legacy name
  uniqueness.
- **Compatibility:** Contract boundary. Source instances must already be drained.
- **Rollback:** None.
- **Pending concerns:** Operational runbooks must treat contract authorization as the explicit end
  of source rollback eligibility.

### 08: Expand entity identifier primary key

- **ID:** `2.0.0/entitydb/08_expand_entity_identifier_primary_key`
- **Definition:**
  [`08_expand_entity_identifier_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/08_expand_entity_identifier_primary_key.go)
- **Purpose:** Allow more than one value for the same entity identifier name by changing the primary
  key from `(ENTITY_ID, DEPLOYMENT_ID, NAME)` to `(ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)`.
- **Source state:** Exact accepted `ENTITY_IDENTIFIER` columns and the exact three-column legacy
  primary key. The temporary target index name must be unused.
- **Readiness:** Ready for the exact legacy or target key. Unexpected columns, key order, relation
  type, or conflicting index name blocks execution.
- **Execution:** Set a two-second local lock timeout, create a unique target-key index, drop the old
  primary key, and attach the new primary key using that index.
- **Verification:** Target requires the exact four-column primary key and no leftover temporary
  index conflict.
- **Compatibility:** Existing rows remain valid. Source writes that produce one value per name are
  still accepted by the expanded key.
- **Rollback:** None. Reverting to the legacy key may be impossible after multiple values exist.
- **Pending concerns:** This operation changes only the schema. Array-aware JSON fallback lookup and
  the deployment-scoped identifier resynchronization operation are still pending. Their behavior,
  mixed-version controls, and validation requirements are recorded in
  [`entity-identifier-multi-value-migration-follow-up.md`](entity-identifier-multi-value-migration-follow-up.md).

### 09: Create entity runtime data table

- **ID:** `2.0.0/runtime_persistent/09_create_entity_runtime_data`
- **Definition:**
  [`09_create_entity_runtime_data.go`](backend/internal/system/migration/releases/v2_0_0/09_create_entity_runtime_data.go)
- **Purpose:** Add `ENTITY_RUNTIME_DATA` for entity state, runtime attributes, revision, and audit
  timestamps in `runtime_persistent`.
- **Source state:** The table is absent.
- **Readiness:** Ready for the absent source state or the exact target table. A partial table,
  non-table relation, wrong column, primary key, check constraint, or conflicting constraint name
  blocks execution.
- **Execution:** Create the seven-column table with primary key `(DEPLOYMENT_ID, ENTITY_ID)`, default
  empty JSON object and revision values, and an object-type JSON check.
- **Verification:** Checks relation kind, ordered column definitions and defaults, primary key, and
  exact JSON check constraint.
- **Compatibility:** Additive. Source instances ignore the table.
- **Data migration:** None. Existing persisted entities are expected to have the legacy `ACTIVE`
  state. The target treats a missing runtime row as active and creates runtime state lazily when a
  supported mutation requires persistence.
- **Rollback:** None.
- **Pending concerns:** The no-backfill design depends on the accepted legacy-state invariant, which
  operations 10 and 11 verify. State-changing target capabilities must not become available while
  source instances still serve. Readiness does not currently preflight a collision with the
  PostgreSQL-generated primary-key index name, so that conflict would be reported during execution.

### 10: Add temporary entity state default

- **ID:** `2.0.0/entitydb/10_add_entity_state_default`
- **Definition:**
  [`10_add_entity_state_default.go`](backend/internal/system/migration/releases/v2_0_0/10_add_entity_state_default.go)
- **Purpose:** Keep source-schema entity creation compatible when the target stops supplying the
  legacy `ENTITY.STATE` column.
- **Source state:** Exact non-null `VARCHAR(50)` state column, no default, and every persisted value
  is `ACTIVE`.
- **Readiness:** Ready for the source state or the exact `ACTIVE` default target state. A missing or
  unexpected column, wrong default, or any non-active persisted state blocks execution.
- **Execution:** Set a two-second local lock timeout, then add the `ACTIVE` default.
- **Verification:** Target requires the exact column and default, with all rows still active.
- **Compatibility:** Both binaries can create entities during mixed-version serving. Source writes
  `ACTIVE`; target may omit the column and receive the database default.
- **Rollback:** No separate rollback is needed because the default is source-compatible. Operation
  11 removes the complete column at contract.
- **Dependencies:** Operation 11 requires this compatibility state and operation 09's runtime table.
- **Pending concerns:** Target features that persist non-active runtime state must remain unavailable
  until the source is drained.

### 11: Remove legacy entity state

- **ID:** `2.0.0/entitydb/11_remove_entity_state`
- **Definition:**
  [`11_remove_entity_state.go`](backend/internal/system/migration/releases/v2_0_0/11_remove_entity_state.go)
- **Purpose:** Remove `ENTITY.STATE` after entity runtime state has moved to
  `runtime_persistent.ENTITY_RUNTIME_DATA`.
- **Source state:** The runtime-data table exactly matches its target schema. `ENTITY.STATE` is the
  exact non-null `VARCHAR(50)` column with the `ACTIVE` default and every value is active.
- **Readiness:** Ready for the exact source state or when the column is already absent. A missing or
  malformed runtime table, wrong state default, or non-active legacy value blocks execution.
- **Execution:** Set a two-second local lock timeout, then drop `ENTITY.STATE`.
- **Verification:** Target requires the exact runtime-data table and an absent legacy state column.
- **Compatibility:** Contract boundary. Source instances cannot use the entity schema after this
  operation and must already be drained.
- **Rollback:** None.
- **Pending concerns:** Enable target-only state management only after contract completes.

### 12: Add SSO session properties

- **ID:** `2.0.0/runtime_persistent/12_add_sso_session_properties`
- **Definition:**
  [`12_add_sso_session_properties.go`](backend/internal/system/migration/releases/v2_0_0/12_add_sso_session_properties.go)
- **Purpose:** Add optional structured properties to persisted SSO sessions.
- **Source state:** `SSO_SESSION` is an ordinary table with primary key
  `(SESSION_ID, DEPLOYMENT_ID)`, and `PROPERTIES` is absent.
- **Readiness:** Ready for the exact source state or when a nullable PostgreSQL `JSONB` column with
  no default already exists. A missing or non-table relation, unexpected primary key, or unexpected
  column definition blocks execution.
- **Execution:** Set a two-second local lock timeout, then add nullable `PROPERTIES JSONB`.
- **Verification:** Target requires one nullable `JSONB` properties column with no default.
- **Compatibility:** Additive. Existing rows and source-created rows receive `NULL`, and source
  instances ignore the new column.
- **Rollback:** None. The nullable column can remain when returning to the source version.
- **Pending concerns:** None specific to the schema operation. Any future target feature using the
  properties must tolerate `NULL` for existing and source-created sessions during mixed-version
  serving.

### 13: Create notification template table

- **ID:** `2.0.0/configdb/13_create_notification_template`
- **Definition:**
  [`13_create_notification_template.go`](backend/internal/system/migration/releases/v2_0_0/13_create_notification_template.go)
- **Purpose:** Add deployment-scoped storage for channel-specific notification templates.
- **Source state:** `NOTIFICATION_TEMPLATE` is absent and the PostgreSQL-generated primary-key and
  unique-index relation names are unused.
- **Readiness:** Ready for the absent source state or the exact target table. A partial table,
  non-table relation, conflicting generated name, column or default mismatch, or key mismatch blocks
  execution.
- **Execution:** Create the ten-column table with JSONB content and design, primary key
  `(DEPLOYMENT_ID, ID)`, and uniqueness on `(DEPLOYMENT_ID, CHANNEL, HANDLE)`.
- **Verification:** Checks relation kind, ordered column definitions and defaults, the exact primary
  key, and the exact unique constraint.
- **Compatibility:** Additive. Source instances do not depend on the new table.
- **Rollback:** None.
- **Pending concerns:** The product change must update the fresh-install database scripts when it is
  merged. This migration change intentionally does not modify them in advance.

## Cross-operation pending work

1. Implement and validate the operation 08 follow-up: array-aware fallback lookup and resumable
   identifier resynchronization after source drain.
2. Complete production controls for resumable data work, including configurable batch sizing,
   pacing, resource limits, and operational observability.
3. Keep capability restrictions migration-specific. Entity-type writers, identifier-projection
   writers, and runtime-state management need only the controls required by the operation that is
   active. Authentication and token issuance should remain available when they do not mutate the
   affected representation.
4. Reconcile the synthetic `TestPostgresResumableBackfill` integration expectation with the
   engine's explicit contract boundary. The relevant ThunderID 2.0.0 separate-store and shared-store
   migration workflows pass, but the full migration integration package still reports that
   unrelated fixture mismatch.
5. Keep the daily main-branch deployment mapping described in
   [`saas-continuous-deployment-migration-model.md`](saas-continuous-deployment-migration-model.md)
   aligned with immutable operation IDs and release-plan ordering.

## Validation expectations for every operation

Each operation should retain:

- unit tests for source, target, invalid, and readiness states;
- idempotency coverage for an already satisfied target;
- execution and post-verification coverage;
- dependency or deferred-readiness coverage where applicable;
- PostgreSQL integration coverage in separate-store and shared-store layouts;
- rollback and resume coverage for every resumable rollback-capable operation; and
- a documented mixed-version compatibility statement and any required capability controls.
