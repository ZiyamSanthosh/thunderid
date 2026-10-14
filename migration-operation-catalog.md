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
| Expand | 01 | `configdb` | Create authorization PDP connection table | Schema, atomic | None | Implemented |
| Expand | 02 | `configdb` | Add nullable entity-type handle | Schema, atomic | Schema retained | Implemented |
| Expand | 03 | `entitydb` | Expand `ENTITY.TYPE` to 100 characters | Schema, atomic | None | Implemented |
| Expand | 07 | `entitydb` | Add `VALUE` to the `ENTITY_IDENTIFIER` primary key | Schema, atomic | None | Expand only implemented |
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
| Migrate | 04 | `configdb` | Backfill entity-type handles | Data, resumable | Operation 04 rollback | Implemented |
| Migrate | 05 | `configdb` | Add entity-type handle uniqueness | Schema, atomic | None | Implemented |
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
- **Execution:** Set a two-second local lock timeout, then add the target unique constraint.
- **Verification:** Target requires both legacy name uniqueness and target handle uniqueness.
- **Compatibility:** Both source and target invariants coexist during mixed-version serving.
- **Rollback:** No separate schema rollback. Clearing handles through operation 04 rollback leaves
  the nullable target uniqueness constraint harmless to the source.
- **Pending concerns:** Adding the constraint validates existing data and needs a table lock within
  the configured timeout.

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
  until the source is drained.

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
  serving.

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
- **Execution:** Create the policy table, policy-stage check, uniqueness rule, and parent index.
- **Verification:** Checks ordered columns and defaults, keys, check semantics, and index definition.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Pending concerns:** The current fresh-install script still uses an earlier resource-sharing
  model. The owning product change must align it before release validation.

### 20: Create resource sharing policy unit table

- **ID:** `2.0.0/configdb/20_create_resource_sharing_policy_unit`
- **Definition:**
  [`20_create_resource_sharing_policy_unit.go`](backend/internal/system/migration/releases/v2_0_0/20_create_resource_sharing_policy_unit.go)
- **Purpose:** Store organizational-unit reach rules belonging to a resource sharing policy.
- **Source state:** `RESOURCE_SHARING_POLICY_UNIT` is absent and its explicit index name is unused.
- **Readiness:** Deferred to operation 19 when the policy table is absent. Otherwise ready for the
  absent source or exact target table. Any partial or conflicting schema blocks execution.
- **Execution:** Create the policy-unit table, cascading policy foreign key, checks, and OU index.
- **Verification:** Checks ordered columns, key and cascade definitions, checks, and index.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Dependencies:** Operation 19 creates the referenced policy table.
- **Pending concerns:** The fresh-install script alignment noted for operation 19 also applies.

### 21: Create resource sharing policy overlay-rule table

- **ID:** `2.0.0/configdb/21_create_resource_sharing_policy_overlay_rule`
- **Definition:**
  [`21_create_resource_sharing_policy_overlay_rule.go`](backend/internal/system/migration/releases/v2_0_0/21_create_resource_sharing_policy_overlay_rule.go)
- **Purpose:** Store field-level overlay rules belonging to a resource sharing policy.
- **Source state:** `RESOURCE_SHARING_POLICY_OVERLAY_RULE` is absent and its index names are unused.
- **Readiness:** Deferred to operation 19 when the policy table is absent. Otherwise ready for the
  absent source or exact target table. Any partial or conflicting schema blocks execution.
- **Execution:** Create the overlay-rule table, cascading policy foreign key, section check,
  uniqueness constraint, policy index, and blanket-rule partial unique index.
- **Verification:** Checks ordered columns, keys and cascade action, check semantics, and indexes.
- **Compatibility:** Additive. Source instances ignore the table.
- **Rollback:** None.
- **Dependencies:** Operation 19 creates the referenced policy table.
- **Pending concerns:** The fresh-install script alignment noted for operation 19 also applies.

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
- **Pending concerns:** The fresh-install script alignment noted for operation 19 also applies.

## Cross-operation pending work

1. Implement and validate the operation 07 follow-up: array-aware fallback lookup and resumable
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
