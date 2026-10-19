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
- **Atomic** operations run as one transaction. **Online** schema operations execute restartable
  autocommit DDL and use durable progress plus catalog verification for recovery. **Resumable**
  operations process bounded batches and persist checkpoints.
- Contract operations are irreversible boundaries. They run only after source instances are
  drained and rollback authorization has ended.
- For this release, source instances may serve only through Expand. Stop source traffic before
  Migrate because operations 43 through 49 replace entity-type and agent-name representations and
  the source and target binaries do not provide dual-read behavior. Keep the service offline
  through Contract, then start the target binary after final verification.
- The current migration implementation supports PostgreSQL only.

## Release plan at a glance

| Phase | No. | Store | Operation | Mode | Rollback | Current status |
| --- | ---: | --- | --- | --- | --- | --- |
| Expand | 01 | `configdb` | Create authorization PDP connection table | Schema, atomic | None | Implemented |
| Expand | 02 | `configdb` | Add nullable entity-type handle | Schema, atomic | Schema retained | Implemented |
| Expand | 03 | `entitydb` | Expand `ENTITY.TYPE` to 100 characters | Schema, atomic | None | Implemented |
| Expand | 07 | `entitydb` | Add `VALUE` to the `ENTITY_IDENTIFIER` primary key | Schema, online | None | Expand only implemented |
| Expand | 08 | `runtime_persistent` | Create `ENTITY_RUNTIME_DATA` | Schema, atomic | None | Implemented |
| Expand | 09 | `entitydb` | Add temporary `ACTIVE` default to `ENTITY.STATE` | Schema, atomic | Not required | Implemented |
| Expand | 11 | `runtime_persistent` | Add nullable SSO session properties | Schema, atomic | None | Implemented |
| Expand | 12 | `configdb` | Create notification template table | Schema, atomic | None | Implemented |
| Expand | 13 | `configdb` | Create variable table | Schema, atomic | None | Implemented |
| Expand | 14 | `configdb` | Create secret table | Schema, atomic | None | Implemented |
| Expand | 15 | `configdb` | Create gateway table | Schema, atomic | None | Implemented |
| Expand | 16 | `configdb` | Create configuration version table | Schema, atomic | None | Implemented |
| Expand | 17 | `configdb` | Create gateway applied-version table | Schema, atomic | None | Implemented |
| Expand | 18 | `configdb` | Create gateway excluded-resource table | Schema, atomic | None | Implemented |
| Expand | 19 | `configdb` | Create resource sharing policy table | Schema, atomic | None | Implemented |
| Expand | 20 | `configdb` | Create resource sharing policy unit table | Schema, atomic | None | Implemented |
| Expand | 21 | `configdb` | Create resource sharing policy overlay-rule table | Schema, atomic | None | Implemented |
| Expand | 22 | `configdb` | Create resource overlay value table | Schema, atomic | None | Implemented |
| Expand | 23 | `configdb` | Scope `ENTITY_TYPES` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 24 | `configdb` | Scope `ROLE` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 25 | `configdb` | Scope `THEME` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 26 | `configdb` | Scope `LAYOUT` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 27 | `configdb` | Scope `INBOUND_CLIENT` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 28 | `configdb` | Scope `IDP` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 29 | `configdb` | Scope `NOTIFICATION_SENDER` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 30 | `configdb` | Scope `CERTIFICATE` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 31 | `configdb` | Scope `RESOURCE_SERVER` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 32 | `configdb` | Scope `RESOURCE` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 33 | `configdb` | Scope `ACTION` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 34 | `configdb` | Scope `FLOW` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 35 | `configdb` | Scope `PRESENTATION_DEFINITION` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 36 | `configdb` | Scope `CREDENTIAL_CONFIGURATION` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 37 | `entitydb` | Scope `ORGANIZATION_UNIT` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 38 | `entitydb` | Scope `ENTITY` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 39 | `entitydb` | Scope `GROUP` primary key by deployment | Schema, online | Conditional | Implemented |
| Expand | 40 | `configdb` | Expand `CERTIFICATE.REF_ID` to 2048 characters | Schema, atomic | None | Implemented |
| Expand | 41 | `runtime_persistent` | Expand revocation criterion values to 2048 characters | Schema, atomic | None | Implemented |
| Expand | 42 | `runtime_persistent` | Add SSO participant application lookup index | Schema, online | None | Implemented |
| Migrate | 04 | `configdb` | Backfill entity-type handles | Data, resumable | Operation 04 rollback | Implemented |
| Migrate | 05 | `configdb` | Add entity-type handle uniqueness | Schema, online | None | Implemented |
| Migrate | 43 | `entitydb` | Replace entity record type names with handles | Data, resumable | Operation 43 rollback | Implemented |
| Migrate | 44 | `configdb` | Replace inbound-client entity-type names with handles | Data, resumable | Operation 44 rollback | Implemented |
| Migrate | 45 | `configdb` | Replace identity-provider entity-type names with handles | Data, resumable | Operation 45 rollback | Implemented |
| Migrate | 46 | `configdb` | Replace authorization PDP entity-type names with handles | Data, resumable | Operation 46 rollback | Implemented |
| Migrate | 47 | `configdb` | Replace flow entity-type names with handles | Data, resumable | Operation 47 rollback | Implemented |
| Migrate | 48 | `configdb` | Add name to the default agent type schema | Data, atomic | Operation 48 rollback | Implemented |
| Migrate | 49 | `entitydb` | Move agent names into schema attributes | Data, resumable | Operation 49 rollback | Implemented |
| Contract | 06 | `configdb` | Finalize entity-type handles | Schema, atomic | None | Implemented |
| Contract | 10 | `entitydb` | Remove legacy `ENTITY.STATE` | Schema, atomic | None | Implemented |

Operation numbers are contiguous identifiers, but they do not define phase ordering. The release
plan in [`release.go`](backend/internal/system/migration/releases/v2_0_0/release.go) defines execution
order.

## Operation records

### 01: Create authorization PDP connection table

- **ID:** `2.0.0/configdb/01_create_authorization_pdp_connection`
- **Definition:**
  [`01_create_authorization_pdp_connection.go`](backend/internal/system/migration/releases/v2_0_0/01_create_authorization_pdp_connection.go)
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

### 02: Add entity-type handle column

- **ID:** `2.0.0/configdb/02_add_entity_type_handle`
- **Definition:**
  [`02_add_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/02_add_entity_type_handle.go)
- **Purpose:** Introduce the nullable `ENTITY_TYPES.HANDLE VARCHAR(100)` compatibility column needed
  before handles are populated.
- **Source state:** `ENTITY_TYPES.NAME` is the accepted non-null `VARCHAR(100)` column and `HANDLE`
  is absent.
- **Readiness:** Ready when the source state exists or the exact nullable handle column already
  exists. An unexpected name or handle definition blocks execution.
- **Execution:** Set a two-second local lock timeout, then add the nullable column.
- **Verification:** Target requires the accepted name column and nullable `HANDLE VARCHAR(100)`.
- **Compatibility:** Additive. Source instances ignore the column.
- **Rollback:** The schema column is retained. The paired operation 04 rollback removes handle
  values when returning to the source version.
- **Dependencies:** Operation 04 depends on this column.
- **Pending concerns:** Entity-type mutations must follow the migration's applicable write controls
  while operation 04 establishes a stable backfill result.

### 03: Expand entity type length

- **ID:** `2.0.0/entitydb/03_expand_entity_type_length`
- **Definition:**
  [`03_expand_entity_type_length.go`](backend/internal/system/migration/releases/v2_0_0/03_expand_entity_type_length.go)
- **Purpose:** Increase `ENTITY.TYPE` from `VARCHAR(50)` to `VARCHAR(100)`.
- **Source state:** Exactly one non-null `VARCHAR(50)` `ENTITY.TYPE` column.
- **Readiness:** Ready for the exact source or target definition. Missing, nullable, non-varchar, or
  any other length blocks execution.
- **Execution:** Set a two-second local lock timeout, then alter the column type.
- **Verification:** Target requires non-null `VARCHAR(100)`.
- **Compatibility:** The wider column accepts every source value.
- **Rollback:** None. Shrinking could reject target-written values.
- **Pending concerns:** None specific to this operation.

### 04: Backfill entity-type handles

- **ID:** `2.0.0/configdb/04_backfill_entity_type_handle`
- **Definition:**
  [`04_backfill_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/04_backfill_entity_type_handle.go)
- **Purpose:** Create valid, unique handles from legacy entity-type names.
- **Source state:** The exact name and nullable handle columns exist. Handles may be entirely absent
  or partially populated, but every populated handle must already be valid and unique within its
  deployment and category.
- **Readiness:** Deferred until operation 02 when `HANDLE` is absent. Ready when the accepted schema
  exists and current data is valid. Invalid or duplicate populated handles block execution.
- **Execution:** Process at most 100 null-handle rows per batch. Generate normalized handles,
  resolve collisions deterministically, update rows, and checkpoint the last processed ID. The
  operation can run for selected deployments or for all deployments.
- **Verification:** Target requires every entity type in scope to have a valid unique handle. An
  empty scope is already target. A partial valid backfill remains source and is resumable.
- **Compatibility:** The legacy name representation and uniqueness remain in place through the
  mixed-version window.
- **Rollback:** Paired with `04_backfill_entity_type_handle_rollback`.
- **Dependencies:** Operation 02 must provide the column. Operation 05 follows the completed
  backfill.
- **Pending concerns:** Batch size and pacing are fixed. Production resource limits and configurable
  pacing remain migration-engine backlog items. Relevant entity-type writes must not race the
  backfill.

### 04 rollback: Remove entity-type handle representation

- **ID:** `2.0.0/configdb/04_backfill_entity_type_handle_rollback`
- **Definition:**
  [`04_backfill_entity_type_handle_rollback.go`](backend/internal/system/migration/releases/v2_0_0/04_backfill_entity_type_handle_rollback.go)
- **Purpose:** Return entity-type data to the source representation before source rollback.
- **Source state for rollback:** One or more entity types in scope have a non-null handle.
- **Execution:** Clear handles in batches of at most 100 rows and checkpoint the last processed ID.
- **Verification:** Rollback target requires every handle in scope to be null.
- **Scope:** Clears the complete handle representation, including values not originally generated
  by the forward backfill. This is intentional because the source representation has no handles.
- **Compatibility:** The nullable column and target uniqueness constraint can remain because null
  handles do not prevent the source schema from serving.
- **Availability:** Allowed only before contract authorization. It is not a path back from operation
  06.
- **Pending concerns:** Applicable entity-type writers must remain controlled during rollback so
  they cannot repopulate handles concurrently.

### 05: Add entity-type handle uniqueness

- **ID:** `2.0.0/configdb/05_add_entity_type_handle_uniqueness`
- **Definition:**
  [`05_add_entity_type_handle_uniqueness.go`](backend/internal/system/migration/releases/v2_0_0/05_add_entity_type_handle_uniqueness.go)
- **Purpose:** Enforce unique `(HANDLE, CATEGORY, DEPLOYMENT_ID)` values while retaining legacy
  `(NAME, CATEGORY, DEPLOYMENT_ID)` uniqueness.
- **Source state:** Exact name and nullable handle columns, legacy name uniqueness present, target
  handle uniqueness absent, and the handle backfill complete.
- **Readiness:** Deferred until operations 02 and 04 when their results are not yet available.
  Ready when both uniqueness constraints already coexist. Unexpected columns or constraints block
  execution.
- **Execution:** Build the target unique index concurrently. An invalid index left by an interrupted
  build is dropped concurrently and rebuilt. Finalization uses a two-second lock timeout to attach
  the prepared index as the target unique constraint in a short transaction.
- **Verification:** Target requires both legacy name uniqueness and target handle uniqueness with
  no leftover migration index.
- **Compatibility:** Both source and target invariants coexist during mixed-version serving.
- **Rollback:** No separate schema rollback. Clearing handles through operation 04 rollback leaves
  the nullable target uniqueness constraint harmless to the source.
- **Pending concerns:** Serving-impact qualification must measure the final constraint-attachment
  lock under representative load.

### 06: Finalize entity-type handles

- **ID:** `2.0.0/configdb/06_finalize_entity_type_handle`
- **Definition:**
  [`06_finalize_entity_type_handle.go`](backend/internal/system/migration/releases/v2_0_0/06_finalize_entity_type_handle.go)
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

### 07: Expand entity identifier primary key

- **ID:** `2.0.0/entitydb/07_expand_entity_identifier_primary_key`
- **Definition:**
  [`07_expand_entity_identifier_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/07_expand_entity_identifier_primary_key.go)
- **Purpose:** Allow more than one value for the same entity identifier name by changing the primary
  key from `(ENTITY_ID, DEPLOYMENT_ID, NAME)` to `(ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)`.
- **Source state:** Exact accepted `ENTITY_IDENTIFIER` columns and the exact three-column legacy
  primary key. The dedicated migration index may be absent, valid, or an invalid interrupted build
  with the expected target columns.
- **Readiness:** Ready for the exact legacy, recoverable prepared, or target key. Unexpected column
  definitions, key order, relation type, or a conflicting index definition blocks execution.
- **Execution:** Build the unique four-column target index concurrently outside the engine
  transaction. An invalid interrupted build is dropped concurrently and rebuilt. Finalization uses
  a two-second lock timeout in a short transaction to replace the primary key using the prepared
  index.
- **Verification:** Target requires the exact four-column primary key and no leftover migration
  index.
- **Compatibility:** Existing rows remain valid. Source writes that produce one value per name are
  still accepted by the expanded key.
- **Rollback:** None. Reverting to the legacy key may be impossible after multiple values exist.
- **Pending concerns:** This operation changes only the schema. Array-aware JSON fallback lookup is
  still pending. A bulk identifier resynchronization operation was considered and deliberately
  excluded from the current migration plan. The decision and former design are recorded in
  [`entity-identifier-multi-value-migration-follow-up.md`](entity-identifier-multi-value-migration-follow-up.md).

### 08: Create entity runtime data table

- **ID:** `2.0.0/runtime_persistent/08_create_entity_runtime_data`
- **Definition:**
  [`08_create_entity_runtime_data.go`](backend/internal/system/migration/releases/v2_0_0/08_create_entity_runtime_data.go)
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
  operations 09 and 10 verify. State-changing target capabilities must not become available while
  source instances still serve. Readiness does not currently preflight a collision with the
  PostgreSQL-generated primary-key index name, so that conflict would be reported during execution.
  Operations 08 through 10 are implemented but intentionally disabled in `release.go` until the
  entity runtime-data product change and database scripts are merged.

### 09: Add temporary entity state default

- **ID:** `2.0.0/entitydb/09_add_entity_state_default`
- **Definition:**
  [`09_add_entity_state_default.go`](backend/internal/system/migration/releases/v2_0_0/09_add_entity_state_default.go)
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
  10 removes the complete column at contract.
- **Dependencies:** Operation 10 requires this compatibility state and operation 08's runtime table.
- **Pending concerns:** Target features that persist non-active runtime state must remain unavailable
  until the source is drained. Operations 08 through 10 are implemented but intentionally disabled
  in `release.go` until the entity runtime-data product change and database scripts are merged.

### 10: Remove legacy entity state

- **ID:** `2.0.0/entitydb/10_remove_entity_state`
- **Definition:**
  [`10_remove_entity_state.go`](backend/internal/system/migration/releases/v2_0_0/10_remove_entity_state.go)
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
  Operations 08 through 10 are implemented but intentionally disabled in `release.go` until the
  entity runtime-data product change and database scripts are merged.

### 11: Add SSO session properties

- **ID:** `2.0.0/runtime_persistent/11_add_sso_session_properties`
- **Definition:**
  [`11_add_sso_session_properties.go`](backend/internal/system/migration/releases/v2_0_0/11_add_sso_session_properties.go)
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
  serving. The operation is implemented but intentionally disabled in `release.go` until the SSO
  session properties product change and database scripts are merged.

### 12: Create notification template table

- **ID:** `2.0.0/configdb/12_create_notification_template`
- **Definition:**
  [`12_create_notification_template.go`](backend/internal/system/migration/releases/v2_0_0/12_create_notification_template.go)
- **Purpose:** Add deployment-scoped storage for channel-specific notification templates.
- **Source state:** `NOTIFICATION_TEMPLATE` is absent and the PostgreSQL-generated primary-key and
  unique-index relation names are unused.
- **Readiness:** Ready for the absent source state or the exact target table. A partial table,
  non-table relation, conflicting generated name, column or default mismatch, or key mismatch blocks
  execution.
- **Execution:** Create the eleven-column table, including `OU_ID`, with JSONB content and design, primary key
  `(DEPLOYMENT_ID, ID)`, and uniqueness on `(DEPLOYMENT_ID, CHANNEL, HANDLE)`.
- **Verification:** Checks relation kind, ordered column definitions and defaults, the exact primary
  key, and the exact unique constraint.
- **Compatibility:** Additive. Source instances do not depend on the new table.
- **Rollback:** None.
- **Pending concerns:** The supplied target includes `OU_ID`, while the current fresh-install script
  in this branch still has the earlier ten-column shape. The product change must bring that script
  to the same target before release validation is finalized.

### 13: Create variable table

- **ID:** `2.0.0/configdb/13_create_variable`
- **Definition:**
  [`13_create_variable.go`](backend/internal/system/migration/releases/v2_0_0/13_create_variable.go)
- **Purpose:** Add deployment-scoped storage for readable configuration variables.
- **Source state:** `VARIABLE` is absent and its explicit index name is unused.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, key, or index blocks execution.
- **Execution:** Create the six-column table and its deployment/name index.
- **Verification:** Checks relation kind, ordered columns and defaults, primary key, and index.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 14: Create secret table

- **ID:** `2.0.0/configdb/14_create_secret`
- **Definition:**
  [`14_create_secret.go`](backend/internal/system/migration/releases/v2_0_0/14_create_secret.go)
- **Purpose:** Add deployment-scoped storage for encrypted configuration secrets.
- **Source state:** `SECRET` is absent and its explicit index name is unused.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, key, or index blocks execution.
- **Execution:** Create the six-column table and its deployment/name index.
- **Verification:** Checks relation kind, ordered columns and defaults, primary key, and index.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 15: Create gateway table

- **ID:** `2.0.0/configdb/15_create_gateway`
- **Definition:**
  [`15_create_gateway.go`](backend/internal/system/migration/releases/v2_0_0/15_create_gateway.go)
- **Purpose:** Store gateways administered by the control plane and identify one default gateway per
  deployment.
- **Source state:** `GATEWAY` is absent and the default-gateway index name is unused.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, key, or index blocks execution.
- **Execution:** Create the table, its name and URL uniqueness constraints, and the partial unique
  default-gateway index.
- **Verification:** Checks all columns and defaults, primary and unique keys, and the partial index.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 16: Create configuration version table

- **ID:** `2.0.0/configdb/16_create_configuration_version`
- **Definition:**
  [`16_create_configuration_version.go`](backend/internal/system/migration/releases/v2_0_0/16_create_configuration_version.go)
- **Purpose:** Store captured configuration versions.
- **Source state:** `CONFIGURATION_VERSION` is absent.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, or key blocks execution.
- **Execution:** Create the nine-column table with its primary and version uniqueness keys.
- **Verification:** Checks relation kind, ordered columns and defaults, primary key, and unique keys.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 17: Create gateway applied-version table

- **ID:** `2.0.0/configdb/17_create_gateway_applied_version`
- **Definition:**
  [`17_create_gateway_applied_version.go`](backend/internal/system/migration/releases/v2_0_0/17_create_gateway_applied_version.go)
- **Purpose:** Store the current and previous configuration version applied to each gateway.
- **Source state:** `GATEWAY_APPLIED_VERSION` is absent.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, or key blocks execution.
- **Execution:** Create the five-column table with its composite primary key.
- **Verification:** Checks relation kind, ordered columns and defaults, and the primary key.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None specific to this operation.

### 18: Create gateway excluded-resource table

- **ID:** `2.0.0/configdb/18_create_gateway_excluded_resource`
- **Definition:**
  [`18_create_gateway_excluded_resource.go`](backend/internal/system/migration/releases/v2_0_0/18_create_gateway_excluded_resource.go)
- **Purpose:** Persist resources that each gateway must leave unchanged during configuration apply.
- **Source state:** `GATEWAY_EXCLUDED_RESOURCE` is absent.
- **Readiness:** Ready for the absent source or exact target table. Any conflicting relation or
  schema mismatch blocks execution.
- **Execution:** Create the six-column table keyed by deployment, gateway, and resource key.
- **Verification:** Checks relation kind, ordered columns, and the composite primary key.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** The current fresh-install script in this branch still has the earlier
  excluded-resource shape. The owning product change must align it before release validation.

### 19: Create resource sharing policy table

- **ID:** `2.0.0/configdb/19_create_resource_sharing_policy`
- **Definition:**
  [`19_create_resource_sharing_policy.go`](backend/internal/system/migration/releases/v2_0_0/19_create_resource_sharing_policy.go)
- **Purpose:** Store the root policy representation for resource sharing.
- **Source state:** `RESOURCE_SHARING_POLICY` is absent and its explicit index name is unused.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, schema, check, key, or index blocks execution.
- **Execution:** Create the policy table with primary key `(DEPLOYMENT_ID, ID)`, the deployment-scoped
  resource uniqueness rule, policy-stage check, and parent index `(DEPLOYMENT_ID, PARENT_POLICY_ID)`.
- **Verification:** Checks ordered columns and defaults, keys, check semantics, and index definition.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None. The operation matches the current PostgreSQL fresh-install schema.

### 20: Create resource sharing policy unit table

- **ID:** `2.0.0/configdb/20_create_resource_sharing_policy_unit`
- **Definition:**
  [`20_create_resource_sharing_policy_unit.go`](backend/internal/system/migration/releases/v2_0_0/20_create_resource_sharing_policy_unit.go)
- **Purpose:** Store organizational-unit reach rules belonging to a resource sharing policy.
- **Source state:** `RESOURCE_SHARING_POLICY_UNIT` is absent and its explicit index name is unused.
- **Readiness:** Deferred to operation 19 when the policy table is absent. Otherwise ready for the
  absent source or exact target table. Any partial or conflicting schema blocks execution.
- **Execution:** Create the policy-unit table with primary key `(DEPLOYMENT_ID, POLICY_ID, OU_ID)`,
  cascading foreign key `(DEPLOYMENT_ID, POLICY_ID)` to the policy table, checks, and deployment-scoped
  OU index.
- **Verification:** Checks ordered columns, key and cascade definitions, checks, and index.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Dependencies:** Operation 19 creates the referenced policy table.
- **Pending concerns:** None. The operation matches the current PostgreSQL fresh-install schema.

### 21: Create resource sharing policy overlay-rule table

- **ID:** `2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule`
- **Definition:**
  [`21_create_resource_sharing_policy_overlay_rule.go`](backend/internal/system/migration/releases/v2_0_0/21_create_resource_sharing_policy_overlay_rule.go)
- **Purpose:** Store field-level overlay rules belonging to a resource sharing policy.
- **Source state:** `RESOURCE_SHARING_POLICY_OVERLAY_RULE` is absent and its index names are unused.
- **Readiness:** Deferred to operation 19 when the policy table is absent. Otherwise ready for the
  absent source or exact target table. Any partial or conflicting schema blocks execution.
- **Execution:** Create the overlay-rule table with primary key `(DEPLOYMENT_ID, ID)`, cascading
  deployment-scoped policy foreign key, deployment-scoped uniqueness constraint and policy index,
  section check, and deployment-scoped blanket-rule partial unique index.
- **Verification:** Checks ordered columns, keys and cascade action, check semantics, and indexes.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Dependencies:** Operation 19 creates the referenced policy table.
- **Pending concerns:** None. The operation matches the current PostgreSQL fresh-install schema.

### 22: Create resource overlay value table

- **ID:** `2.0.0/configdb/22_create_resource_overlay_value`
- **Definition:**
  [`22_create_resource_overlay_value.go`](backend/internal/system/migration/releases/v2_0_0/22_create_resource_overlay_value.go)
- **Purpose:** Store resolved overlay values per deployment, resource, organizational unit, and field.
- **Source state:** `RESOURCE_OVERLAY_VALUE` is absent.
- **Readiness:** Ready for the absent source or exact target table. Any partial or conflicting
  relation, column, or key blocks execution.
- **Execution:** Create the eight-column table with its five-column primary key.
- **Verification:** Checks relation kind, ordered columns and defaults, and the primary key.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** None. The operation matches the current PostgreSQL fresh-install schema.

### 23: Scope ENTITY_TYPES primary key by deployment

- **ID:** `2.0.0/configdb/23_expand_entity_types_primary_key`
- **Definition:**
  [`23_expand_entity_types_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/23_expand_entity_types_primary_key.go)
- **Purpose:** Replace the globally unique `ENTITY_TYPES.ID` primary key with
  `(DEPLOYMENT_ID, ID)`.
- **Source state:** `ENTITY_TYPES` is an ordinary table with non-null `DEPLOYMENT_ID` and `ID`
  columns and a primary key on `ID`. The dedicated migration index is either absent, valid with the
  target columns, or an invalid interrupted concurrent build with the target columns.
- **Target state:** After Expand, the primary key is `(DEPLOYMENT_ID, ID)`, the temporary target
  index has been attached as that key, and a separate unique `ID` compatibility index preserves the
  source invariant. Contract removes the compatibility index after source drain.
- **Readiness:** Accepts the exact source, recoverable interrupted-index, or target state. Missing
  columns, nullable key columns, unexpected primary-key columns, or a conflicting index definition
  block execution.
- **Execution:** Build both the unique `(DEPLOYMENT_ID, ID)` target index and unique `ID`
  compatibility index concurrently outside a transaction. Invalid interrupted builds are dropped
  concurrently and rebuilt. Finalization uses a two-second lock timeout in a short transaction to
  replace the primary key while leaving global `ID` uniqueness active for mixed-version serving.
- **Verification:** Re-inspects the table, required non-null columns, ordered primary-key columns,
  and temporary index state.
- **Compatibility:** Existing source and target queries continue supplying UUID identifiers and
  deployment scope. Global `ID` uniqueness remains enforced until Contract, so target writes cannot
  introduce an identity shape that the source did not previously accept.
- **Rollback:** Restoring the global `ID` key is possible only while `ID` remains globally unique.
  No automatic rollback operation is currently registered.
- **Pending concerns:** Serving-impact qualification must measure the final primary-key cutover lock.

Operations 07 and 23 through 39 use the same online primary-key preparation and bounded cutover.
Operations 23 through 39 additionally build a unique compatibility index over the legacy key before
the cutover. Where a parent key has dependent foreign keys, the operation first adds deployment-scoped
foreign keys as `NOT VALID`, validates them without blocking ordinary reads and writes, and removes
the legacy foreign keys only in the final short transaction. The compatibility index continues to
enforce the source uniqueness invariant throughout canary serving. Separate Contract operations,
with IDs such as `23_finalize_entity_types_primary_key`, drop those indexes concurrently after source
drain. Operation 07 intentionally does not retain its old uniqueness because allowing multiple values
is the purpose of that key change. The shared implementations are
[`online_primary_key_operation.go`](backend/internal/system/migration/releases/v2_0_0/online_primary_key_operation.go)
and [`legacy_primary_key_contract_operation.go`](backend/internal/system/migration/releases/v2_0_0/legacy_primary_key_contract_operation.go).

### 24: Scope ROLE primary key by deployment

- **ID:** `2.0.0/configdb/24_expand_role_primary_key`
- **Definition:** [`24_expand_role_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/24_expand_role_primary_key.go)
- **Purpose:** Change `ROLE` from primary key `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns, the legacy key, and the legacy
  `ROLE_PERMISSION(ROLE_ID)` foreign key. Exact target and recoverable prepared states are accepted.
- **Execution and verification:** Concurrently prepares the target unique index and validates
  `ROLE_PERMISSION(DEPLOYMENT_ID, ROLE_ID)` before the transactional key and foreign-key cutover.
- **Compatibility:** Both binaries continue using deployment-scoped UUID role identifiers.
- **Rollback:** Conditional on `ID` remaining globally unique. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 25: Scope THEME primary key by deployment

- **ID:** `2.0.0/configdb/25_expand_theme_primary_key`
- **Definition:** [`25_expand_theme_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/25_expand_theme_primary_key.go)
- **Purpose:** Change `THEME` from primary key `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the ordinary table, non-null key columns, the legacy key,
  and no conflicting migration index. Exact target and recoverable interrupted-index states pass.
- **Execution and verification:** Concurrently prepares the target index. During the bounded
  transactional key swap it discovers and drops the legacy
  `INBOUND_CLIENT(THEME_ID) -> THEME(ID)` foreign key because the target schema has no replacement,
  then verifies the ordered target key. PostgreSQL integration coverage also verifies that the exact
  legacy reference is absent and existing values are preserved.
- **Compatibility:** Ordinary source reads and writes remain valid. Theme deletion no longer obtains
  the source schema's database-level `SET NULL` side effect after the cutover.
- **Rollback:** Conditional on `ID` remaining globally unique. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load. If source serving
  can delete themes during Expand, drain or restrict that management action around the cutover unless
  the source application is confirmed not to rely on database-level `SET NULL`.

### 26: Scope LAYOUT primary key by deployment

- **ID:** `2.0.0/configdb/26_expand_layout_primary_key`
- **Definition:** [`26_expand_layout_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/26_expand_layout_primary_key.go)
- **Purpose:** Change `LAYOUT` from primary key `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and the exact legacy or target key;
  an invalid interrupted target index is recoverable, while other schema shapes are blocked.
- **Execution and verification:** Uses concurrent index preparation. During the short transactional
  key swap it discovers and drops the legacy `INBOUND_CLIENT(LAYOUT_ID) -> LAYOUT(ID)` foreign key
  because the target schema has no replacement, then performs exact key verification. PostgreSQL
  integration coverage also verifies that the legacy reference is absent and values are preserved.
- **Compatibility:** Ordinary deployment-scoped source reads and writes remain valid. Layout deletion
  no longer obtains the source schema's database-level `SET NULL` side effect after the cutover.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load. If source serving
  can delete layouts during Expand, drain or restrict that management action around the cutover
  unless the source application is confirmed not to rely on database-level `SET NULL`.

### 27: Scope INBOUND_CLIENT primary key by deployment

- **ID:** `2.0.0/configdb/27_expand_inbound_client_primary_key`
- **Definition:** [`27_expand_inbound_client_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/27_expand_inbound_client_primary_key.go)
- **Purpose:** Change `INBOUND_CLIENT` from `(ENTITY_ID)` to `(DEPLOYMENT_ID, ENTITY_ID)`.
- **Source state and readiness:** Requires both non-null key columns and the legacy
  `OAUTH_INBOUND_PROFILE(ENTITY_ID)` foreign key, or the exact completed target.
- **Execution and verification:** Prepares the parent index concurrently, adds and validates the
  composite profile foreign key, then swaps both constraints in the bounded final transaction.
- **Compatibility:** Existing identifiers remain valid and deployment-scoped queries are unchanged.
- **Rollback:** Conditional on global `ENTITY_ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 28: Scope IDP primary key by deployment

- **ID:** `2.0.0/configdb/28_expand_idp_primary_key`
- **Definition:** [`28_expand_idp_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/28_expand_idp_primary_key.go)
- **Purpose:** Change `IDP` from primary key `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Accepts only the exact legacy, prepared, or target catalog state
  with non-null key columns.
- **Execution and verification:** Concurrent index preparation and a short transactional key swap,
  followed by exact key and temporary-index verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 29: Scope NOTIFICATION_SENDER primary key by deployment

- **ID:** `2.0.0/configdb/29_expand_notification_sender_primary_key`
- **Definition:** [`29_expand_notification_sender_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/29_expand_notification_sender_primary_key.go)
- **Purpose:** Change `NOTIFICATION_SENDER` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the exact table, non-null key columns, and recognized key
  and migration-index states.
- **Execution and verification:** Concurrent index preparation, bounded key cutover, and exact
  catalog verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 30: Scope CERTIFICATE primary key by deployment

- **ID:** `2.0.0/configdb/30_expand_certificate_primary_key`
- **Definition:** [`30_expand_certificate_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/30_expand_certificate_primary_key.go)
- **Purpose:** Change `CERTIFICATE` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrently prepares the target index and verifies the final key
  after a bounded transactional cutover.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 31: Scope RESOURCE_SERVER primary key by deployment

- **ID:** `2.0.0/configdb/31_expand_resource_server_primary_key`
- **Definition:** [`31_expand_resource_server_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/31_expand_resource_server_primary_key.go)
- **Purpose:** Change `RESOURCE_SERVER` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the legacy key and the legacy `RESOURCE` and `ACTION`
  foreign keys, or an exact recoverable prepared or completed state.
- **Execution and verification:** Prepares the parent index concurrently, validates composite
  foreign keys from both child tables, and swaps the constraints in the bounded final transaction.
- **Compatibility:** The legacy foreign keys remain enforced until their validated replacements
  are ready. Existing deployment-scoped application behavior remains valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the multi-constraint final cutover lock under representative load.

### 32: Scope RESOURCE primary key by deployment

- **ID:** `2.0.0/configdb/32_expand_resource_primary_key`
- **Definition:** [`32_expand_resource_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/32_expand_resource_primary_key.go)
- **Purpose:** Change `RESOURCE` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the legacy self-reference and `ACTION` foreign key, or an
  exact recoverable prepared or completed state.
- **Execution and verification:** Prepares the parent index, validates deployment-scoped self and
  action foreign keys, and performs their final transactional cutover with the primary key.
- **Compatibility:** The legacy global `ID` compatibility index remains through mixed-version
  serving. Runtime and imported nested resources receive service-generated UUIDv7 IDs; declarative
  nested resources remain in the file store with synthetic IDs.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the multi-constraint final cutover lock under representative load.

### 33: Scope ACTION primary key by deployment

- **ID:** `2.0.0/configdb/33_expand_action_primary_key`
- **Definition:** [`33_expand_action_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/33_expand_action_primary_key.go)
- **Purpose:** Change `ACTION` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrent target-index construction, bounded key swap, and exact
  catalog verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 34: Scope FLOW primary key by deployment

- **ID:** `2.0.0/configdb/34_expand_flow_primary_key`
- **Definition:** [`34_expand_flow_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/34_expand_flow_primary_key.go)
- **Purpose:** Change `FLOW` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the legacy key and `FLOW_VERSION(FLOW_ID)` foreign key,
  or an exact recoverable prepared or target state.
- **Execution and verification:** Prepares the parent index, validates the composite flow-version
  foreign key, then swaps the constraints in the bounded transaction and verifies the target.
- **Compatibility:** The legacy foreign key remains active during online preparation.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 35: Scope PRESENTATION_DEFINITION primary key by deployment

- **ID:** `2.0.0/configdb/35_expand_presentation_definition_primary_key`
- **Definition:** [`35_expand_presentation_definition_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/35_expand_presentation_definition_primary_key.go)
- **Purpose:** Change `PRESENTATION_DEFINITION` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrent index preparation, bounded transactional cutover, and
  exact catalog verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 36: Scope CREDENTIAL_CONFIGURATION primary key by deployment

- **ID:** `2.0.0/configdb/36_expand_credential_configuration_primary_key`
- **Definition:** [`36_expand_credential_configuration_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/36_expand_credential_configuration_primary_key.go)
- **Purpose:** Change `CREDENTIAL_CONFIGURATION` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrent index preparation, bounded transactional cutover, and
  exact catalog verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 37: Scope ORGANIZATION_UNIT primary key by deployment

- **ID:** `2.0.0/entitydb/37_expand_organization_unit_primary_key`
- **Definition:** [`37_expand_organization_unit_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/37_expand_organization_unit_primary_key.go)
- **Purpose:** Change `ORGANIZATION_UNIT` from `(OU_ID)` to `(DEPLOYMENT_ID, OU_ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrent index preparation, bounded transactional cutover, and
  exact catalog verification.
- **Compatibility:** Existing deployment-scoped organizational-unit behavior remains valid.
- **Rollback:** Conditional on global `OU_ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 38: Scope ENTITY primary key by deployment

- **ID:** `2.0.0/entitydb/38_expand_entity_primary_key`
- **Definition:** [`38_expand_entity_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/38_expand_entity_primary_key.go)
- **Purpose:** Change `ENTITY` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires the legacy key and
  `ENTITY_IDENTIFIER(ENTITY_ID)` foreign key, or an exact recoverable prepared or target state.
- **Execution and verification:** Prepares the parent index, validates the composite identifier
  foreign key, and performs the foreign-key and primary-key cutover in the bounded transaction.
- **Compatibility:** The legacy identifier foreign key remains enforced during online preparation.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 39: Scope GROUP primary key by deployment

- **ID:** `2.0.0/entitydb/39_expand_group_primary_key`
- **Definition:** [`39_expand_group_primary_key.go`](backend/internal/system/migration/releases/v2_0_0/39_expand_group_primary_key.go)
- **Purpose:** Change `GROUP` from `(ID)` to `(DEPLOYMENT_ID, ID)`.
- **Source state and readiness:** Requires non-null key columns and an exact legacy, prepared, or
  target catalog state.
- **Execution and verification:** Concurrent index preparation, bounded transactional cutover, and
  exact catalog verification.
- **Compatibility:** Existing deployment-scoped reads and writes remain valid.
- **Rollback:** Conditional on global `ID` uniqueness. No automatic rollback is registered.
- **Pending concerns:** Measure the final cutover lock under representative load.

### 40: Expand certificate reference ID length

- **ID:** `2.0.0/configdb/40_expand_certificate_reference_id`
- **Definition:**
  [`40_expand_certificate_reference_id.go`](backend/internal/system/migration/releases/v2_0_0/40_expand_certificate_reference_id.go)
- **Purpose:** Increase `CERTIFICATE.REF_ID` from `VARCHAR(36)` to `VARCHAR(2048)` so references such
  as metadata document URLs can be stored.
- **Source state:** Exactly one non-null `VARCHAR(36)` `CERTIFICATE.REF_ID` column.
- **Readiness:** Ready for the exact source or target definition. A missing, nullable, non-varchar,
  or differently sized column blocks execution.
- **Execution:** Set a two-second local lock timeout, then widen the column to `VARCHAR(2048)`.
- **Verification:** Target requires a non-null `VARCHAR(2048)` column and preserves existing values.
- **Compatibility:** The wider column accepts every source value and remains readable by source
  instances.
- **Rollback:** None. Shrinking could reject target-written references longer than 36 characters.
- **Pending concerns:** None specific to this operation.

### 41: Expand revocation criterion value length

- **ID:** `2.0.0/runtime_persistent/41_expand_revocation_criterion_value`
- **Definition:**
  [`41_expand_revocation_criterion_value.go`](backend/internal/system/migration/releases/v2_0_0/41_expand_revocation_criterion_value.go)
- **Purpose:** Increase `REVOCATION_CRITERIA.CRITERION_VALUE` from `VARCHAR(255)` to
  `VARCHAR(2048)` so longer revocation identifiers can be stored.
- **Source state:** Exactly one non-null `VARCHAR(255)` `REVOCATION_CRITERIA.CRITERION_VALUE`
  column.
- **Readiness:** Ready for the exact source or target definition. A missing, nullable, non-varchar,
  or differently sized column blocks execution.
- **Execution:** Set a two-second local lock timeout, then widen the column to `VARCHAR(2048)`.
- **Verification:** Target requires a non-null `VARCHAR(2048)` column.
- **Compatibility:** The wider column accepts every source value and remains readable by source
  instances.
- **Rollback:** None. Shrinking could reject target-written criterion values longer than 255
  characters.
- **Pending concerns:** None specific to this operation. PostgreSQL integration coverage verifies
  the exact target column, preservation of a source value, a 2048-character insert, and completed
  operation progress.

### 42: Add SSO participant application lookup index

- **ID:** `2.0.0/runtime_persistent/42_add_sso_session_participant_application_index`
- **Definition:**
  [`42_add_sso_session_participant_application_index.go`](backend/internal/system/migration/releases/v2_0_0/42_add_sso_session_participant_application_index.go)
- **Purpose:** Add the `idx_sso_session_participant_app` non-unique index on
  `SSO_SESSION_PARTICIPANT (DEPLOYMENT_ID, APP_ID)` for application-participation lookups.
- **Source state:** The required non-null columns exist and the named index is absent, or an
  interrupted concurrent build left an invalid index with the expected definition.
- **Readiness:** Ready for the exact source, recoverable interrupted, or target state. Missing
  columns or a conflicting index definition block execution.
- **Execution:** Drop a recoverable invalid index when present, then build the index with
  `CREATE INDEX CONCURRENTLY` outside a transaction.
- **Verification:** Target requires an exact non-unique, valid, ready index on
  `(DEPLOYMENT_ID, APP_ID)`.
- **Compatibility:** The index is additive and is ignored safely by source instances.
- **Rollback:** None required. Retaining the index does not change stored data or query semantics.
- **Pending concerns:** None specific to this operation. PostgreSQL integration coverage verifies
  the exact valid, ready, non-unique B-tree index without a predicate, preservation of a source row,
  and completed operation progress.

### 43: Migrate entity record type references

- **ID:** `2.0.0/entitydb/43_migrate_entity_type_references`
- **Definition:**
  [`43_migrate_entity_type_references.go`](backend/internal/system/migration/releases/v2_0_0/43_migrate_entity_type_references.go)
- **Purpose:** Replace legacy entity-type names in `ENTITY.TYPE` with the exact handles persisted by
  operation 04.
- **Source state:** Every non-empty type value resolves by deployment and `ENTITY.CATEGORY` to an
  `ENTITY_TYPES.NAME` or is already a valid persisted handle.
- **Readiness:** Deferred until the handle column and complete backfill exist. Unknown or malformed
  references block execution.
- **Execution:** Read the persisted `(DEPLOYMENT_ID, CATEGORY, NAME, HANDLE)` mapping from `configdb`,
  update `entitydb` in batches of 100 ordered by deployment and entity ID, and persist a cursor.
- **Verification:** The target requires every entity type value to resolve as a persisted handle.
- **Compatibility:** Requires downtime. The source binary must not serve after this operation
  begins, and the target starts only after the complete migration is verified.
- **Rollback:** The paired rollback restores names before operation 04 clears the handle column
  values.
- **Pending concerns:** None specific to this operation.

### 44: Migrate inbound-client entity-type references

- **ID:** `2.0.0/configdb/44_migrate_inbound_client_entity_type_references`
- **Definition:**
  [`44_migrate_inbound_client_entity_type_references.go`](backend/internal/system/migration/releases/v2_0_0/44_migrate_inbound_client_entity_type_references.go)
- **Purpose:** Replace names in `INBOUND_CLIENT.PROPERTIES.allowedUserTypes`, `allowedAgentTypes`,
  and the keys of `subjectAttribute` with persisted handles.
- **Readiness:** Deferred until operation 04 has produced a complete mapping. Invalid JSON,
  unexpected field shapes, unknown names, and translated duplicate map keys block execution.
- **Execution:** Rewrite only the affected JSON fields in batches of 100 and checkpoint by
  deployment and entity ID.
- **Verification:** Every affected value and map key must resolve as a persisted handle.
- **Compatibility:** Requires the same downtime boundary as operation 43.
- **Rollback:** Restores legacy names using the persisted reverse mapping.
- **Pending concerns:** None specific to this operation.

### 45: Migrate identity-provider entity-type references

- **ID:** `2.0.0/configdb/45_migrate_idp_entity_type_references`
- **Definition:**
  [`45_migrate_idp_entity_type_references.go`](backend/internal/system/migration/releases/v2_0_0/45_migrate_idp_entity_type_references.go)
- **Purpose:** Replace names in `IDP.ATTRIBUTE_CONFIGURATION`, including
  `userTypeResolution.default`, every `userTypeResolution.valueMapping` value, and every
  `userTypeAttributeMappings[].userType` value.
- **Readiness:** Deferred until operation 04. Invalid JSON, unexpected structures, or unknown user
  types block execution.
- **Execution:** Rewrite affected JSON fields in resumable batches of 100.
- **Verification:** Every affected value must resolve as a persisted user-type handle.
- **Compatibility:** Requires the same downtime boundary as operation 43.
- **Rollback:** Restores legacy user-type names from the persisted mapping.
- **Pending concerns:** None specific to this operation.

### 46: Migrate authorization PDP entity-type references

- **ID:** `2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references`
- **Definition:**
  [`46_migrate_authorization_pdp_entity_type_references.go`](backend/internal/system/migration/releases/v2_0_0/46_migrate_authorization_pdp_entity_type_references.go)
- **Purpose:** Replace every
  `AUTHORIZATION_PDP_CONNECTION.PROPERTIES.subjectAttributeMappings[].entityType` name with its
  persisted handle.
- **Readiness:** Deferred until operation 04. Invalid JSON, unexpected structures, unknown names,
  or a reference that resolves differently in user and agent categories blocks execution.
- **Execution:** Rewrite affected JSON values in resumable batches of 100.
- **Verification:** Every affected value must resolve as one unambiguous persisted handle.
- **Compatibility:** Requires the same downtime boundary as operation 43.
- **Rollback:** Restores the corresponding legacy names.
- **Pending concerns:** References do not carry a category. A value shared by user and agent
  categories is accepted only when both categories translate to the same value.

### 47: Migrate flow entity-type references

- **ID:** `2.0.0/configdb/47_migrate_flow_entity_type_references`
- **Definition:**
  [`47_migrate_flow_entity_type_references.go`](backend/internal/system/migration/releases/v2_0_0/47_migrate_flow_entity_type_references.go)
- **Purpose:** Rewrite every stored flow version, replacing `allowedUserTypes` on
  `UserTypeResolver` nodes and `allowedAgentTypes` on `AgentTypeResolver` nodes with persisted
  handles.
- **Readiness:** Deferred until operation 04. Invalid node JSON, unexpected affected field shapes,
  or unknown entity types block execution.
- **Execution:** Process every `FLOW_VERSION`, including inactive versions, in batches of 100 and
  checkpoint by deployment, flow ID, and version.
- **Verification:** Every affected resolver value must resolve as a persisted handle.
- **Compatibility:** Requires the same downtime boundary as operation 43.
- **Rollback:** Restores legacy names in every affected flow version.
- **Pending concerns:** None specific to this operation.

### 48: Migrate the default agent name schema

- **ID:** `2.0.0/configdb/48_migrate_default_agent_name_schema`
- **Definition:**
  [`48_migrate_default_agent_name_schema.go`](backend/internal/system/migration/releases/v2_0_0/48_migrate_default_agent_name_schema.go)
- **Purpose:** Add the required, unique string `name` property to the default agent type and select
  it as the display attribute for an existing deployment.
- **Source state:** The deployment's default agent type has no `name` schema property and no
  configured display attribute.
- **Readiness:** Deferred until entity-type handles are available. A conflicting `name` property or
  display attribute blocks execution.
- **Execution:** Update the default agent type identified by deployment, agent category, and
  `default` handle in one transaction while preserving every unrelated schema field.
- **Verification:** The target requires `name` to be a required, unique string with display name
  `Name`, and requires `systemAttributes.display` to equal `name`.
- **Compatibility:** Requires the release Migrate downtime boundary. The source agent API does not
  write the name into schema attributes.
- **Rollback:** Remove only the name property and display setting introduced by the operation.
- **Pending concerns:** Custom agent types are not modified automatically. Operation 49 blocks if
  an existing agent uses a custom type that does not declare a string `name` attribute.

### 49: Migrate agent names to schema attributes

- **ID:** `2.0.0/entitydb/49_migrate_agent_names_to_attributes`
- **Definition:**
  [`49_migrate_agent_names_to_attributes.go`](backend/internal/system/migration/releases/v2_0_0/49_migrate_agent_names_to_attributes.go)
- **Purpose:** Move every existing agent name from `SYSTEM_ATTRIBUTES.name` to `ATTRIBUTES.name` and
  align its identifier projection with the target representation.
- **Source state:** Each agent has one non-empty string name in system attributes, no name in schema
  attributes, and no unexpected name identifier values.
- **Readiness:** Deferred until operation 48. Database-backed and declarative agent types must
  declare `name` as a string. Conflicting or missing names, incompatible custom agent types,
  unexpected identifier rows, and name collisions involving at least one agent block execution.
  Duplicate names belonging only to non-agent entities do not block this agent-only migration.
- **Execution:** Process agents in batches of 100 ordered by deployment and entity ID. Preserve all
  unrelated JSON fields, move the name, and upsert the matching identifier with source `attribute`
  in the same batch transaction.
- **Verification:** Every agent has one schema-attribute name, no system-attribute name, and exactly
  one matching attribute-sourced name identifier.
- **Compatibility:** Requires downtime. The target generic uniqueness lookup is deployment-scoped
  but not category-scoped, so readiness enforces the target's deployment-wide collision behavior.
- **Rollback:** Move names back to system attributes and change their identifier source back to
  `system` before operation 48 restores the default schema.
- **Pending concerns:** The agent schema declares `name` unique, but the product's generic uniqueness
  lookup does not retain the entity category supplied by schema validation. Until that product-level
  scope is clarified or corrected, an agent name must not match any entity name in the same
  deployment. The readiness guard prevents migration from creating data the target cannot validate.

## Cross-operation pending work

1. Implement and validate the operation 07 array-aware fallback lookup. Identifier
   resynchronization is deliberately not part of the current migration plan.
2. Complete production controls for resumable data work, including configurable batch sizing,
   pacing, resource limits, and operational observability.
3. Enforce the release-level downtime procedure for operations 43 through 49. Source serving ends
   before Migrate, no application writes are allowed through Migrate and Contract, and the target
   starts only after final verification. Migration-specific capability restrictions remain useful
   for future releases that retain a mixed-version serving window.
4. Keep the daily main-branch deployment mapping described in
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
