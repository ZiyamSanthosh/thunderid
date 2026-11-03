# Migration test run: change log and findings

Running record of every change made, and every finding uncovered, while testing the 1.0.0 -> 2.0.0
migration against real 1.0.0 PostgreSQL dumps. Written so another engineer or model can understand
what was changed, how, and why, without reading the conversation. Newest entries are appended at the
bottom of each section. Status tags: **DONE**, **IN PROGRESS**, **PLANNED / OPEN**.

## 0. Context

- Goal: take PostgreSQL dumps of a real 1.0.0 deployment (four stores: `configdb`, `entitydb`,
  `runtime_persistentdb`, `runtime_transientdb`), restore them into a Postgres 18 container, and run
  `thunderid migration initialize / status / check / apply` phase by phase (`expand`, `migrate`,
  `contract`) using the 2.0.0 distribution pack, then compare the migrated schema with the schema the
  pack ships (`dbscripts/*/postgres.sql`).
- The dumps are the 1.0.0 schema (`configdb` has 20 tables before migration).
- Test harness (outside this repo): `/Users/ziyamsanthosh/Desktop/wso2-cloud/dump/`
  - `restore-dbs.sh` / `drop-dbs.sh` / `common.sh`: create or drop the DBs from the dumps (container
    `thunderid-postgres`, host port 5433, user `platform_idp_db_user` / `postgres`).
  - `generate-pack-keys.sh`: creates the certs, `crypto.key` and `direct_auth_secret` a pack needs to
    load its config, copying the openssl logic of `setup.sh` WITHOUT running `setup.sh` (which also
    bootstraps unwanted resources).
  - `configure-pack-db.sh`: rewrites the pack's `deployment.yaml` `database:` block from SQLite to the
    four Postgres connections.
  - `compare-schema.sh`: builds reference DBs from the pack's `postgres.sql`, normalises both schemas
    (columns, constraints, indexes; column order ignored) and prints the differences. Always exits 0.
  - `run-migration.sh`: orchestrates restore -> unpack pack -> keys -> deployment.yaml -> initialize ->
    status -> per phase `check` + `apply` + `status` -> final status/check -> schema comparison. Output
    of each command is saved under `dump/.work/logs/<timestamp>/`.
- The pack is built with `make build_backend OS=darwin ARCH=arm64`, which overwrites
  `target/dist/thunderid-2.0.0-macos-arm64.zip`. The original zip (before any of my changes) was saved
  as `<scratchpad>/thunderid-2.0.0-macos-arm64.orig.zip`.

## 1. Code changes in this repo

### 1.1 Operations 25 and 26: drop the legacy `INBOUND_CLIENT` foreign keys  (DONE)

Files (all under `backend/internal/system/migration/releases/v2_0_0/`):

| File | Change |
|---|---|
| `online_primary_key_operation.go` | New config field `droppedReferences []droppedPrimaryKeyReference`, new `droppedReferenceQuery`, new method `dropLegacyReferences`, called from `Finalize` just before the primary key is replaced. Config validation for the new field. |
| `25_expand_theme_primary_key.go` | Declares `droppedReferences: {INBOUND_CLIENT, [THEME_ID]}` |
| `26_expand_layout_primary_key.go` | Declares `droppedReferences: {INBOUND_CLIENT, [LAYOUT_ID]}` |
| `dropped_primary_key_reference_test.go` (new) | Unit tests for the drop behaviour |

**Symptom.** `apply expand` failed on `2.0.0/configdb/25_expand_theme_primary_key` (operations 1 to 24
had already completed):

```
replace THEME primary key: pq: cannot drop constraint THEME_pkey on table "THEME" because other objects depend on it
```

**Root cause.** In 1.0.0, `INBOUND_CLIENT` has two foreign keys onto the primary keys of other
tables, on columns that are NOT part of `INBOUND_CLIENT`'s own key:

```sql
-- 1.0.0 schema (from the dump)
"INBOUND_CLIENT_theme_id_fkey"  FOREIGN KEY (theme_id)  REFERENCES "THEME"(id)  ON DELETE SET NULL
"INBOUND_CLIENT_layout_id_fkey" FOREIGN KEY (layout_id) REFERENCES "LAYOUT"(id) ON DELETE SET NULL
```

The online primary-key operations replace `PRIMARY KEY (id)` with `PRIMARY KEY (deployment_id, id)` by
creating a unique index concurrently and then, in `Finalize`, running
`ALTER TABLE .. DROP CONSTRAINT <pkey>` followed by `ADD CONSTRAINT .. PRIMARY KEY USING INDEX`.
Postgres refuses the `DROP CONSTRAINT` while a foreign key depends on that primary key.

Operations that have inbound foreign keys already handle them through `references:` (for example 24
`ROLE` re-creates `ROLE_PERMISSION`'s key as a deployment-scoped composite key before dropping the old
one). Operations 25 (`THEME`) and 26 (`LAYOUT`) declared no `references`, so the foreign keys from
`INBOUND_CLIENT` were never handled.

**Why reordering would not help.** The dependency is `INBOUND_CLIENT.theme_id -> THEME(id)`: a
non-key column of `INBOUND_CLIENT`. Operation 27 changes `INBOUND_CLIENT`'s own key (`ENTITY_ID`) and
does not touch `theme_id`/`layout_id`, so running 27 first changes nothing.

**Why "drop" and not "re-create" (the `references:` approach).**
1. The 2.0.0 `backend/dbscripts/configdb/postgres.sql` defines no foreign key on
   `INBOUND_CLIENT.theme_id` or `layout_id` (only plain nullable columns plus an index). A fresh 2.0.0
   install has no such key, so the migrated schema should match.
2. `references:` re-creates a composite `(deployment_id, theme_id)` key and the helper only supports
   `NO ACTION`, `RESTRICT` and `CASCADE`. The old keys are `ON DELETE SET NULL`; a composite key with
   plain `SET NULL` would also try to null `deployment_id` (`NOT NULL`) and fail.

**How.** `dropLegacyReferences` looks up foreign keys from the declared child table to the operation's
table, keeps only those whose child columns and parent columns match exactly, and drops them by their
discovered name. It is idempotent: an already-missing key is skipped, so an interrupted `Finalize` can
be retried.

```go
// 25_expand_theme_primary_key.go
// 2.0.0 defines no foreign key from INBOUND_CLIENT.THEME_ID, so the legacy one is dropped.
droppedReferences: []droppedPrimaryKeyReference{{table: "INBOUND_CLIENT", columns: []string{"THEME_ID"}}},
```

Effect on the data model: after migration, deleting a theme or layout no longer nulls
`INBOUND_CLIENT.theme_id` / `layout_id` at the database level. This is consistent with the shipped
2.0.0 schema, but the application should not rely on the database to clear those references. This is
a product decision worth confirming with the migration owners.

**Verification.** `go build ./...` clean, `go test ./internal/system/migration/...` passes. In the
full run `apply expand` then completed all 35 operations.

### 1.2 Operation 49 disabled in `release.go`  (HISTORICAL: enabled again by the project owner, see log item 14)

Files: `release.go`, `release_test.go` (same directory).

Operation `49_migrate_agent_names_to_attributes` and its rollback are commented out of the plan
(constructors, `Migrate` list, `Rollback` list), following the existing "Enable ... after ..." comment
style. Tests updated to match: `Migrate` 9 -> 8 entries, `Rollback` 8 -> 7, and the operation 49
assertions are commented out.

**Why.** Operation 49 is still in development (untracked file in git). Its preflight blocked the
`migrate` phase:

```
2.0.0/entitydb/49_migrate_agent_names_to_attributes: One or more deployments contain entities with colliding name values
```

**Finding to fix before re-enabling.** `queryAgentNameCollisions` (`MIG-ENT_MGT-16`) has no
`CATEGORY = 'agent'` filter, while the operation only migrates agents
(`queryAgentNames` filters on `CATEGORY = 'agent'`). In the test data there are 0 agents; the 39
"collisions" are ordinary `user` entities of type `Customer` that share a display name, for example:

```
deployment_id | name              | count | category
platform-idp  | Hasini Samarathunga | 4   | user
platform-idp  | Isuru Vithanage     | 4   | user
```

Restricting the query to agents (or to groups containing at least one agent) removes the false
positive. Not changed; to be done with the operation's owner.

To re-enable 49: uncomment the four lines in `release.go` and the matching lines in `release_test.go`
(restore counts to 9 and 8), rebuild the pack.

### 1.3 Declarative entity types injected into operations 43 to 47 through the entity type service  (DONE)

Decision record: sections 3.1 below. Final decisions: the real `EntityTypeService` is used (not a new
reader), built over the declarative store only; no deployment binding (the file store has no
deployment concept, so declarative types apply to every deployment being migrated); only the
declarative half is injected, database types are still read by the operations' own SQL.

| File | Change |
|---|---|
| `internal/entitytype/init.go` | New `InitializeDeclarativeService(ouService, authzService)`: builds the service over `newEntityTypeFileBasedStore()` and loads the resource files. No routes, no MCP, no database reads, store-mode config ignored. |
| `internal/ou/init.go` | New `InitializeService(cacheManager, authzService)`: the OU service alone (needed only to resolve `ouHandle` in resource files). |
| `releases/v2_0_0/declarative_entity_types.go` (new) | Exported `DeclarativeEntityType`, `DeclarativeEntityTypes` interface, and the lazy, cached `declarativeEntityTypeSource`. |
| `releases/v2_0_0/release.go` | `Dependencies.DeclarativeEntityTypes func() (DeclarativeEntityTypes, error)`; `migrationPlan` builds one source and passes it to the 10 operation constructors (forward and rollback of 43 to 47). |
| `releases/v2_0_0/entity_type_reference_operation.go` | Mapping gets `declaredByName` / `declaredByHandle` (keyed by category + value, no deployment). `readMapping` merges the declarative types; `translate` consults database then declarative, and fails on a conflict. |
| `releases/v2_0_0/43..47_*.go` | Constructors take the source and wrap their config with `withDeclaredEntityTypes`. |
| `internal/system/migration/current/current.go` | Forwards the two new types (alias). |
| `cmd/server/migration_declarative_entity_types.go` (new) | `newDeclarativeEntityTypesProvider(cfg)`: lazily builds sysauthz -> cache manager -> OU service -> declarative entity type service and adapts `GetEntityTypeList` (all categories, paged, runtime security context) to the narrow interface. |
| `cmd/server/migration_cmd.go` | Passes the provider in `current.Dependencies`. |
| tests | `declarative_entity_types_test.go`, `cmd/server/migration_declarative_entity_types_test.go`, constructor calls in existing tests pass `nil`. |

Example, the case that failed before (`ENTITY.TYPE = 'Customer'`, no `Customer` row in `ENTITY_TYPES`,
`customer.yaml` in `config/resources/user_types`):

```
database mapping:      PAUMStaff -> paumstaff
declarative mapping:   Customer  -> customer ; Person -> person
ENTITY.TYPE 'Customer' (user)  => 'customer'      (196 rows)
ENTITY.TYPE 'PAUMStaff' (user) => 'paumstaff'      (3 rows)
```

Rules implemented in `translate`: database mapping first; a value that is already a handle is accepted
unchanged (re-runs are safe); a name defined differently in the database and in the files is an error
naming both handles (the migration never guesses); category scoped.

Details worth knowing:
- `GetEntityTypeList` only sets `IsReadOnly` in the COMPOSITE store. The declarative-only store leaves
  it `false`, so the lister must not filter on it (a first attempt did, and returned nothing).
- `getAccessibleResources` allows everything when called with `security.WithRuntimeContext`.
- When `Dependencies.DeclarativeEntityTypes` is nil (tests, other callers), operations map from the
  database alone, as before. The injection guide suggests an error for a missing provider; a nil
  provider is treated as "no declarative types" so existing callers keep working. `cmd/server` always
  supplies it.
- The test deployment's files are `customer.yaml` and `person.yaml` (`ouId` set directly, schema as a JSON
  string); the harness copies `user_types` and `organization_units` into the pack's `config/resources`.

### 1.4 Operation 43: leave non-user/agent entities alone  (DONE)

File: `releases/v2_0_0/43_migrate_entity_type_references.go` (+ test in `declarative_entity_types_test.go`).

Symptom after 1.3: `entity-type reference "application" has no forward mapping ... for category "app"`
(92 rows). The application service always writes app entities with the fixed `TYPE = 'application'`
(`internal/application/service.go`, `Category: EntityCategoryApp, Type: "application"`), which is not an
entity type reference; only `user` and `agent` entities have entity types. The transform now returns
the value unchanged for any other category and only translates `user` and `agent`.

```
before: ENTITY app/'application'  -> error, migration stops
after:  ENTITY app/'application'  -> unchanged
        ENTITY user/'Customer'    -> 'customer'
```

### 1.5 Operation 04: reserve declarative handles when generating handles  (DONE)

Files: `releases/v2_0_0/04_backfill_entity_type_handle.go`, `release.go`, tests in
`04_backfill_entity_type_handle_test.go` and `declarative_entity_types_test.go`.

Why: the 2.0.0 server refuses to start when a database entity type has the same handle as a
declarative one in the same category (`validateUniqueDeclarativeHandles`: "handle already used in the
database store"). Operation 04 generates handles from names and only avoided other database handles,
so a database type named `customer` next to the declarative `Customer`/`customer` would get the clashing
handle `customer`.

How: the constructor takes the declarative source. `reservedHandles` returns the declarative handles
keyed by `category + handle` (not by deployment, because declarative types apply to every deployment).
`nextAvailableEntityTypeHandle` treats a reserved handle as taken, so the existing `_1`, `_2` suffixing
applies. `verifyEntityTypeHandleBackfill` now also reports a database row whose handle equals a
declarative handle as invalid, so a database that already has the clash (for example from an earlier
partial run) is flagged instead of passing.

```
database type name 'customer', declarative handle 'customer' (user):
before: handle 'customer'    -> 2.0.0 server fails to start
after:  handle 'customer_1'  -> operations 43 to 47 map the stored name 'customer' to 'customer_1'
```

Operation 05 (unique index on `HANDLE, CATEGORY, DEPLOYMENT_ID`) is purely about database rows and was
not changed. Operation 48 needs no change (section 3.3).

## 2. Findings that are not code changes

### 2.1 Entity types from declarative resources have no row in `ENTITY_TYPES`  (RESOLVED by 1.3)

`apply migrate` fails on operation 43:

```
2.0.0/entitydb/43_migrate_entity_type_references: pre-verification is invalid:
entity-type reference "Customer" has no forward mapping in deployment "platform-idp" for category "user"
```

Data in the dumps:

```
entitydb.ENTITY:        user/Customer 196 rows, user/PAUMStaff 3 rows, app/application 92 rows
configdb.ENTITY_TYPES:  only  platform-idp | user | PAUMStaff | paumstaff
```

Explanation (confirmed by the dump owner): the entity types `Customer` and `Person` are defined as
**declarative (file-based, read-only) resources**, so they are never stored in `ENTITY_TYPES`. The
operations (04 backfill handles, 43 migrate references) only read the database table, so a reference
to a declarative type cannot be mapped.

How declarative entity types are defined (relevant code: `backend/internal/entitytype/declarative_resource.go`,
`backend/internal/system/declarative_resource/loader.go` and `manager.go`):
- YAML documents with `resource_type: user_type` (or `agent_type`), loaded from
  `<serverHome>/config/resources/` (multi-document `*.yaml`/`*.yml` in the root, or per-type
  subdirectories), and from the `-resources` flag if given.
- 1.0.0 files identify the type by `name:`. 2.0.0 files use an explicit `handle:` plus `displayName:`:

```yaml
# 1.0.0 (pack bootstrap/01-default-resources.yaml)     # 2.0.0
resource_type: user_type                                resource_type: user_type
id: 01900000-0000-7000-8000-000000000010                id: 01900000-0000-7000-8000-000000000010
category: user                                          category: user
name: Person                                            handle: person
                                                        displayName: Person
```

So the name -> handle mapping for declarative types exists on disk, in the deployment's resource
files, and the engine could read it. See 3.1 for the options.

### 2.2 Test run result with all changes (operation 49 still disabled)  (DONE)

Full run from the 1.0.0 dumps: `initialize`, `status`, then for `expand`, `migrate`, `contract`:
`check` -> `apply` -> `status`, all succeeded. Final state:

```
STORE               COMPLETED  REQUIRED  STAGE     STATUS  COMPATIBLE
configdb            2.0.0      2.0.0     complete  clean   YES
entitydb            2.0.0      2.0.0     complete  clean   YES
runtime_transient   1.0.0      1.0.0     complete  clean   YES
runtime_persistent  2.0.0      2.0.0     complete  clean   YES
```

### 2.3 Schema comparison: migrated databases vs the 2.0.0 pack's `postgres.sql`  (OPEN findings)

All tables and almost all columns, constraints and indexes match. The remaining differences:

| Store | Difference | Likely cause |
|---|---|---|
| configdb | `RESOURCE_SERVER`: migrated DB has an extra nullable `handle` column (+ partial unique index) and a NULLABLE `identifier` with a partial unique index; 2.0.0 expects no `handle` and `identifier NOT NULL` with a plain unique index | The dump's schema is not the official 1.0.0 schema (official 1.0.0 `RESOURCE_SERVER` has `IDENTIFIER NOT NULL` and no `HANDLE`). No 2.0.0 operation reconciles it. |
| entitydb | `ORGANIZATION_UNIT` has extra `layout_id` and `theme_id` columns | Same: official 1.0.0 and 2.0.0 have neither; nothing removes them. |
| runtime_persistent | extra index `SSO_SESSION (subject_id, flow_id, deployment_id)` | Same kind of drift; not in 2.0.0. |
| runtime_transient | The dump has 9 separate tables (`AUTHORIZATION_CODE`, `AUTHORIZATION_REQUEST`, `CIBA_AUTH_REQUEST`, `JTI_RECORD`, `OPENID4VCI_*`, `OPENID4VP_REQUEST_STATE`, `PAR_REQUEST`, `WEBAUTHN_SESSION`) that are not in 2.0.0, which (like the official 1.0.0) uses a partitioned `RUNTIME_STORE` | The dump was taken from a development build older than the official 1.0.0. The manifest marks this store unchanged, so no operation converts it. |

Conclusion: the migration engine behaves correctly for the official 1.0.0 baseline; these differences
come from the dump being a pre-1.0.0 development schema (platform-idp). To be decided with the dump
owner: whether such databases must be supported (then add operations for the four items above) or
whether the dump should first be normalised to official 1.0.0. Cosmetic differences in how Postgres
prints `CHECK (... IN (...))` are normalised away by `compare-schema.sh`.

Harness fixes made while getting here (all in `dump/`): `compare-schema.sh` cast `contype` to text, now
refuses to report "MATCH" when a schema query fails (it once printed a false match for 0 objects), and
no longer excludes `MIGRATION_*` tables (the pack's own scripts define them). `run-migration.sh` copies
the declarative `user_types` and `organization_units` into the pack (`--resources <dir>` /
`RESOURCES_DIR`).

## 3. Planned / open work

### 3.1 Resolve entity-type references for declarative entity types  (DONE, see 1.3; this section keeps the design record)

**Decision direction (from the project owner).** Do NOT add bespoke YAML reading to the migration
(declarative files are delivered through GitHub/deployment tooling and the server already owns the
loading rules), and do not patch the dump (not scalable). Use the service-injection seam described in
`migration-service-injection-guide.md`: start only the entity type service before the migration, inject
a narrow interface into the operations, and let the service's get methods return the entity types from
the database and the declarative files. The guide says to add a `Dependencies` field only when a real
migration needs one; this is that migration (it is the first service-backed operation).

Clarification on the YAML format: in declarative files both `name` (1.0.0) and `displayName` (2.0.0)
carry the value stored in `ENTITY_TYPES.NAME`; `handle` is the new 2.0.0 field. In the Go model
(`entitytype.EntityType` / `EntityTypeListItem`) `DisplayName` is that name and `Handle` is the handle,
and `IsReadOnly` marks declarative entries. So the lookup the operations need is
`DisplayName -> Handle` per category, which `GetEntityTypeList` already returns.

**What exists and what is missing (code review findings):**

| Item | Finding |
|---|---|
| Seam | `current.DefinitionWithDependencies(current.Dependencies{})` in `cmd/server/migration_cmd.go` already forwards an (empty) dependency value into release plan construction. No engine change needed. |
| Service constructor | `entitytype.Initialize(mux, mcpServer, cacheManager, ouService, authzService)` registers HTTP routes and MCP tools. The guide forbids that for migration. A new exported constructor is needed that only does: `getEntityTypeStoreMode` -> `initializeStore` -> `newEntityTypeService` -> `loadDeclarativeResources`. |
| Service dependencies | `newEntityTypeService` needs an OU service and an authz service. `loadDeclarativeResources` validates each file through the service (`ResolveEntityTypeHandles`), which resolves `ouHandle` (e.g. `default`) through the OU service, whose own `default` OU is also declarative. So the minimal graph is entity type service -> OU service (+ its store and declarative load) -> authz. Cache manager is also required by the stores. Not yet verified how light each can be made. |
| Store mode | Declarative entries only appear when `entity_type.store` is `composite` or `declarative` (or the global `declarative_resources.enabled` fallback). The migration command must read the same configuration as the server. |
| Declarative file format | The 2.0.0 loader requires `handle` and `displayName`. If the declarative files are still 1.0.0 format when the migration runs, the service-based load will fail. The release guidance must say the files are updated to the 2.0.0 format BEFORE running the migration (the design docs already say file changes follow the release's prescribed ordering). |

**Risks to design around:**

1. **Mid-migration database state.** The service's database store reads `HANDLE`, `NAME`, etc. with
   deployment scoping, through the global DB provider, not through the migration's `ReadAccess` or
   transaction. Before operation 04 has run, database rows have `HANDLE IS NULL`; during expand/migrate
   the table is in an intermediate shape. The "combined" list is therefore only trustworthy for the
   declarative half until the database half is fully migrated. Recommendation: the injected interface
   exposes ONLY the declarative (read-only, `IsReadOnly`) entity types (or the service is built in
   declarative-only store mode, which touches no database), and the operations keep reading database
   rows with their own SQL and merge the two sets. This also keeps the guide's guarantee that
   migration writes/reads go through the migration context.
2. **Deployment scoping.** Database rows are per `DEPLOYMENT_ID` (and `--deployment-id` can restrict
   a run); declarative files have no deployment id in the file. The merge must apply the declarative
   set to every deployment being migrated, or to the configured deployment, and this needs an explicit
   rule.
3. **Handle collisions.** Operations 04/05 assign and enforce unique handles for database rows. They
   must also reject a database handle that equals a declarative handle in the same category
   (`validateUniqueDeclarativeHandles` already enforces this at server start; the migration should not
   produce a state that fails it).
4. **Lifecycle.** Services may need closing at command end; the guide asks for lazy providers so
   SQL-only phases do not start the services.

**Proposed implementation (not started):**

1. `entitytype`: add an exported migration-oriented constructor (no routes, no MCP) returning a small
   reader.
2. `releases/v2_0_0`: declare a narrow interface next to the operations, for example
   `declarativeEntityTypes interface{ ListDeclarative(ctx, category) ([]declarativeEntityType, error) }`
   with `DisplayName`, `Handle`, `Category`. Add `Dependencies.EntityTypes func() (declarativeEntityTypes, error)`.
3. Operations 04 (handle collision check), 05, 43 to 47 and their rollbacks (`readEntityTypeReferenceMapping`
   in `entity_type_reference_operation.go`, `readEntityTypeHandleRecords` in
   `04_backfill_entity_type_handle.go`): merge declarative records into the mapping (name -> handle and
   handle -> name per category). Return a clear error if the plan needs the provider and it is missing.
4. `cmd/server/migration_cmd.go`: construct the provider lazily from the server configuration and pass
   it in `current.Dependencies`.
5. Tests per the guide: narrow fake for the operations, release construction with and without the
   provider, and a PostgreSQL integration test using the `Customer`/`Person` data (migrated `ENTITY.TYPE`
   values and durable migration state).
6. Update `migration-service-injection-guide.md` (the dependency field now exists) and this log.

**Rejected alternatives:** (a) insert declarative types into `ENTITY_TYPES` in the dump: changes test
data and hides the real behaviour, not scalable; (b) make operation 43 skip unmapped types: silently
leaves stale type names; (c) parse the YAML inside the migration: duplicates the server's loading and
validation rules.

**Open questions:** where the OU service/authz dependencies can be stubbed for a migration-only graph;
the rule for applying declarative types across deployments; whether declarative `agent_type` files need
the same treatment (operations 48/49).

**Decisions and refinements after reviewing the real declarative files** (source of test data:
`/Users/ziyamsanthosh/Documents/wso2-cloud/platform-idp-resources/config/resources/user_types/`,
files `customer.yaml` and `person.yaml`; `organization_units/` holds the OUs they reference):

- Scope agreed by the project owner: the injected interface exposes only the declarative entity types;
  database rows continue to be read by the operations' own SQL and merged with them.
- Deployment rule (proposed): every stored row has a `DEPLOYMENT_ID` (`platform-idp` in the dump).
  Declarative files belong to exactly one deployment, the one the process is configured with
  (`server.identifier`, see `internal/system/deployment`). The declarative types are applied only to the
  migration of that deployment. Other deployments in the same database map from their database rows.
  Consequence for the harness: the pack ships `server.identifier: default-deployment`
  (`config/default.json`), so `configure-pack-db.sh` must set `server.identifier: platform-idp`.
- Agent types: the server loads declarative entity types from `config/resources/user_types` only
  (`entitytype/declarative_resource.go`, `DirectoryName: "user_types"`); the category comes from the
  file's `category:` field and defaults to `user`. A category-agnostic reader therefore covers agent
  types automatically if a deployment ever declares one. The test deployment declares none.
  Operations 48/49 (agent name schema/attributes) do not need this.
- Implementation refinement: build the injected reader at store level inside the `entitytype` package
  (same file-based store and loader the server uses), not the full service. The full service needs the
  OU and authz services and, in composite mode, reads the database; the loader's validator accepts a
  nil service and then skips OU resolution. The migration-time validation is relaxed to the fields the
  migration depends on (valid handle, display name, id). The files use `ouId` directly and carry the
  schema as a JSON string; both parse with the existing parser.

### 3.2 Remaining steps of the test run  (DONE except where noted in 2.3)

After 3.1: `apply migrate`, `check contract` + `apply contract`, final `status`/`check`, then the
schema comparison (`compare-schema.sh`). Expected schema differences to review at that point:
migration metadata tables (`MIGRATION_*`), and anything operation 25/26's "drop" decision affects.

### 3.3 Not yet verified  (OPEN)

- Operation 04 now reserves declarative handles (section 1.5).
- Operation 05 only adds the `(HANDLE, CATEGORY, DEPLOYMENT_ID)` uniqueness index on `ENTITY_TYPES`. It
  is purely about database rows, so declarative handles are out of its scope; no change needed.
- Operation 48 needs no change. It updates the database row of the default agent type
  (`ENTITY_TYPES WHERE CATEGORY='agent' AND HANDLE='default'`) and only calls the mapping reader to
  learn whether every database row has a handle. A declarative agent type is a read-only file already
  in 2.0.0 format; there is no row to migrate and nothing to translate.
- Operation 49 was implemented and re-enabled by the project owner (log item 14).
- No PostgreSQL integration test yet for the declarative mapping (the guide asks for one); the full
  harness run above is the end-to-end evidence.
- Operations 08, 09, 10 and 11 are intentionally disabled in `release.go` (existing comments, awaiting
  other product changes), so they are not exercised by this run. The expand phase in this build has
  35 operations.

## 4. Chronological log

1. Restored the four dumps into a `postgres:18` container (`thunderid-postgres`, port 5433) and wrote
   `restore-dbs.sh` / `drop-dbs.sh` / `common.sh`.
2. Inspected the 2.0.0 pack: migration is a subcommand of the `thunderid` binary
   (`thunderid migration <initialize|status|check|apply|resume|rollback> [expand|migrate|contract]`).
3. Wrote `generate-pack-keys.sh`, `configure-pack-db.sh`, `compare-schema.sh`, `run-migration.sh`.
4. First full run: `initialize` + `status` OK; `check expand` OK; `apply expand` failed on operation 25
   (section 1.1).
5. Implemented 1.1, added tests, rebuilt the pack. `apply expand` then completed 35 operations.
6. `check migrate` blocked by operation 49 (section 1.2). Disabled 49 on request (it is in development).
7. Re-ran: `check migrate` Ready, `apply migrate` failed on operation 43 (section 2.1). Cause found:
   declarative entity types are not in the database.
8. Project owner chose the service-injection approach over reading YAML or patching the dump. Reviewed
   the entity type service, the injection guide and the migration command; evaluation and proposed
   implementation recorded in section 3.1. No code changed for this yet.
9. Implemented the injection (1.3): `InitializeDeclarativeService`, `ou.InitializeService`, the
   `DeclarativeEntityTypes` dependency, the lazy provider in `cmd/server`, and the mapping merge in
   operations 43 to 47. First attempt filtered on `IsReadOnly` and found nothing (declarative-only
   store never sets it); removed the filter.
10. Run stopped on `app/application` entities; fixed operation 43 to translate only user/agent (1.4).
11. Full run succeeded: all four stores at their required level (2.2).
12. Fixed `compare-schema.sh` (type cast, false-match guard, no metadata exclusion, CHECK
    normalisation). Comparison shows only drift that comes from the dump itself (2.3).
13. Implemented 1.5 (operation 04 reserves declarative handles) after confirming the clash is fatal at
    server start. Full harness run repeated: all four stores at their required level.

## 5. Codex analysis and follow-up validation (2026-10-19)

> Added by Codex after reviewing the implementation described above. Earlier sections are retained as
> a chronological record and can describe temporary states, such as operation 49 being disabled, that
> no longer match the current release plan.

### 5.1 Operation 49 agent-name collision guard

Related change: [PR #5642, Move the agent name into the agent type schema](https://github.com/thunder-id/thunderid/pull/5642).

**Behavior before PR #5642.** An agent name was a top-level agent API field stored in
`ENTITY.SYSTEM_ATTRIBUTES.name`, outside the agent type schema. The agent service owned dedicated
name validation. Its uniqueness check used the generic entity lookup, but after a single match it
loaded the entity and treated the value as a conflict only when the matched entity was also an agent.
The intended rule was therefore agent-name uniqueness, not universal name uniqueness across every
entity category. There was already an edge case where multiple matches made the generic lookup
ambiguous and were reported as an agent-name conflict before the category could be checked.

**Behavior introduced by PR #5642.** `attributes.name` became the only agent-name representation.
The default agent type now declares `name` as a required, unique string and as its display attribute.
The dedicated agent-name validation was removed. Agent writes now use the generic entity-type schema
validation path, and `name` is projected into `ENTITY_IDENTIFIER` as an indexed schema attribute.

**Resulting discrepancy.** The generic uniqueness path uses the agent category and type to load the
correct schema, but its callback checks whether the attribute value exists by calling
`IdentifyEntity({"name": value})`. That lookup is scoped by deployment, but not by entity category or
entity type. A user and an agent named `Calendar Agent` therefore conflict under the new path even
though the old agent service intended to reject only another agent with that name. Two users sharing
a name are still valid when their own schema does not declare `name` unique. If schema uniqueness is
intended to be category-scoped, this is a product bug. If it is intended to be deployment-wide for a
given attribute name, PR #5642 introduced a stricter rule that must be documented as a breaking data
constraint.

**Migration impact.** Existing data can be valid for the source binary but invalid for the target:
an existing agent name in system attributes may equal a user name. Operation 49 moves that value into
the target agent schema attribute and changes its identifier projection from `system` to `attribute`.
The migration cannot decide the intended product uniqueness scope, and it must not silently rename or
drop data. Its readiness check therefore blocks only duplicate-name groups containing at least one
agent. Duplicate groups containing only non-agent entities do not block this agent-only migration.
PostgreSQL integration coverage proves both cases and resumes successfully after the agent-involved
collision is resolved.

### 5.2 Declarative entity-type deployment scope

Related change: [PR #5635, Introduce an immutable handle to entity types](https://github.com/thunder-id/thunderid/pull/5635).

**Behavior before PR #5635.** A user or agent type's `name` was unique and acted as both its display
value and its identifier. Entities, applications, identity providers, AuthZEN connections, flows and
other configuration stored that name when referring to the type. Database-backed type rows were
deployment-scoped. Declarative types lived only in the configured server's resource files, had no
`ENTITY_TYPES` row and carried no deployment identifier in their file model. During ordinary server
operation this was not ambiguous because a server loaded the resource set belonging to its configured
deployment.

**Behavior introduced by PR #5635.** An entity type now has an immutable, unique `handle` and a
mutable, non-unique display name. All product references changed from the old name to the handle. For
example, a stored entity type value changes from `Customer` to `customer`. The PR explicitly leaves
existing databases unmigrated, so a release migration must generate handles for database types and
convert every existing name-based reference.

**Resulting discrepancy.** The name-to-handle conversion must combine two sources. Database types
can be mapped as `(DEPLOYMENT_ID, category, name) -> handle` because their rows contain deployment
ownership. Declarative types must be loaded from the target resource files because they have no
database row. However, the declarative entity-type model and the migration's narrow service result
contain category, display name and handle, but no deployment identifier. A migration command can
process several deployment IDs from the same physical database, so it cannot determine from the
loaded declarative object which deployment owns that mapping. This ambiguity did not affect the
normal single-configured-deployment runtime path. It appears because PR #5635 requires an offline
conversion over deployment-scoped database data while the declarative source has no matching scope.
This is a migration integration contract gap, rather than evidence that normal product runtime reads
are globally scoped.

**Migration impact.** The current implementation applies the one loaded declarative type set to every
deployment selected for migration. Operation 04 reserves those handles while generating database
handles, and operations 43 through 47 use them to translate stored references. In a single-deployment
database this is deterministic. In a multi-deployment database it can reserve a handle or translate a
reference in deployment B using declarative metadata belonging to configured deployment A. The
migration cannot safely infer a different owner because the files provide no deployment identity.
The behavior is intentionally unchanged pending a team decision. The integration fixture binds the
server to `migration-deployment` and verifies the real service path, but it does not claim that the
multi-deployment ambiguity is resolved.

### 5.3 Operations 25 and 26 foreign-key cutover

The source schema has `INBOUND_CLIENT.THEME_ID -> THEME.ID` and
`INBOUND_CLIENT.LAYOUT_ID -> LAYOUT.ID` foreign keys. PostgreSQL cannot replace either referenced
single-column primary key while those foreign keys depend on it. The target schema deliberately has
no replacement foreign keys, so operations 25 and 26 now discover and drop only those exact legacy
references immediately before the short primary-key cutover transaction.

This preserves the online preparation step and keeps the blocking work in the existing short
finalization transaction. Integration coverage now starts from the real legacy foreign keys, confirms
the cutover succeeds, confirms the references are gone, and confirms the theme and layout values are
not modified. The remaining product-level effect is intentional target behavior: deleting a theme or
layout no longer performs database-level `SET NULL` on an inbound client. Ordinary source reads and
writes remain schema-compatible, but source-side theme or layout deletion during the Expand overlap
has different database side effects. The rollout should drain or restrict those management actions
around the cutover unless the source application is confirmed not to rely on `SET NULL`.

### 5.4 Added PostgreSQL integration coverage

- Operation 04 reserves a declarative handle obtained through the real migration service provider.
- Operations 43 through 47 translate references for both database-backed and declarative entity
  types, including a post-Expand authorization PDP row.
- Operations 25 and 26 cut over with their actual source foreign-key dependencies present.
- Operation 49 rejects a collision involving an agent without rejecting duplicate non-agent names.
14. The project owner finished operation 49 and re-enabled it (it now takes the declarative source as a
    dependency). Rebuilt the pack and ran the full harness: `check migrate` Ready, `apply migrate` ran 9
    operations (was 8), contract and final status all compatible. This test data has no agent entities
    and no default agent type, so operations 48 and 49 complete as no-ops here; the 39 user-name
    "collisions" that blocked the earlier version of 49 no longer block. Schema comparison is unchanged
    (section 2.3). Agent-specific behaviour of 48/49 is not exercised by this dump.
