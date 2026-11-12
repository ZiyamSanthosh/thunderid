# Login-only mode current-state assessment

## Status

This document takes the current ThunderID behavior as the baseline for introducing a login-only
operating mode. It records existing implementation requirements and is not a final design decision.

Scope: OAuth 2.0 and OpenID Connect login, including SSO, passkeys, consent and token issuance.

The analysis should be reviewed when authentication, flow execution, session management or database
schemas change.

## Current conclusion

A feature-complete login-only mode cannot run every database except `configdb` as read-only.
The current safe access model is:

| Logical store | Required access | Reason |
|---|---|---|
| `config` | Read-only while serving | Login resolves applications, clients, flows, certificates, server configuration and related policy data. |
| `entity` | Read-write for full login support | Authentication reads identities and credentials. Passkey authentication updates the stored authenticator sign counter. Provisioning-capable flows can create or update identities. |
| `runtime_transient` | Read-write | The authorization and flow protocols create, consume and clear short-lived state. This is required even when identity data is otherwise read-only. |
| `runtime_persistent` | Read-write when SSO, consent or revocation behavior is enabled | Login creates and updates SSO sessions, checkpoint contexts and application participants. Consent and revocation behavior also persist state here. |

Therefore, the initial operating model should be:

```text
config              read-only
entity              read-write
runtime_transient   read-write
runtime_persistent  read-write
```

This conclusion concerns request serving. Process startup must also avoid configuration
reconciliation, declarative imports or any other initialization behavior that writes to `configdb`.

## Database lock behavior during migration

Lock granularity and migration concurrency differ across supported and planned database providers.
The following comparison describes common behavior under the default transactional storage engine.
Exact behavior still depends on the statement, indexes, isolation level and database version.

| Database | Normal data-change locking | Migration and schema-change concern | Effect on login-time writes |
|---|---|---|---|
| PostgreSQL | `INSERT`, `UPDATE` and `DELETE` take row locks for affected rows and a compatible table-level `ROW EXCLUSIVE` lock. MVCC normally lets ordinary reads continue. | Operations such as `ALTER TABLE` and `TRUNCATE` can take `ACCESS EXCLUSIVE` locks that block reads and writes. A large transaction can retain many row locks until commit. | Writes to an unrelated table can normally continue. Writes to the same rows, foreign-key-related rows or a table under a conflicting schema lock can wait or fail. |
| SQLite | Locking is coordinated at the database-file level. WAL mode permits concurrent readers, but only one writer can operate at a time. | A migration write transaction competes with all other writers in the same database file, even when they update different tables. | A long entity migration can delay or reject lifecycle writes. SQLite is not considered a production zero-downtime migration target. |
| MySQL with InnoDB | InnoDB normally uses index-record locks with table-level intention locks. Range operations can also take gap or next-key locks. | DDL uses metadata locks. Even online DDL can require an exclusive metadata lock during some phases. Other MySQL storage engines can have different locking behavior. | Unrelated-table writes generally continue, but overlapping records, ranges, foreign keys and metadata locks can block them. |
| Microsoft SQL Server | The engine can choose row, key, page or table locks. A large operation can escalate lower-level locks into a table lock. | DDL takes schema-modification locks that conflict with schema-stability locks acquired by normal queries. Row-versioned isolation does not remove schema-lock conflicts. | Contention can broaden from rows to pages or the whole table, so migration behavior must be tested with realistic data volume and execution plans. |
| Oracle Database | DML locks modified rows and takes a compatible table-level DML lock. MVCC normally lets readers continue, and Oracle does not normally escalate row locks into table locks. | DDL can require exclusive locks on the affected schema object. Parent-key and unindexed foreign-key operations can introduce wider contention. | Writes to unrelated rows and tables generally continue, but same-row writes, foreign-key interactions and conflicting DDL can block them. |

These locks matter when migration and request serving use the same physical database. They do not
synchronize a source database with a migrated copy. If traffic continues writing to the source
after a snapshot, those committed changes still require replication or change-data capture before
cutover, even when the destination table itself needs no schema transformation.

Migration scripts should avoid relying on a single cross-database locking assumption. Each provider
needs an execution plan that identifies the actual lock mode, bounds transaction size and duration,
sets appropriate lock timeouts, and verifies that security-critical login writes can continue.

Official locking references:

- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html)
- [SQLite write-ahead log concurrency](https://www.sqlite.org/wal.html)
- [MySQL InnoDB locking](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking.html)
- [SQL Server transaction locking and row versioning](https://learn.microsoft.com/en-us/sql/relational-databases/sql-server-transaction-locking-and-row-versioning-guide)
- [Oracle Database data concurrency and consistency](https://docs.oracle.com/en/database/oracle/oracle-database/19/cncpt/data-concurrency-and-consistency.html)

## In-place database migration

An in-place migration changes the database that is currently serving traffic. The old and new
ThunderID versions can therefore use the same physical database during the transition.

```text
ThunderID old version \
                       > shared database being migrated
ThunderID new version /
```

The main advantage is that no second database must be kept synchronized. Every login-time update
is already committed to the database being migrated. The main risks are lock contention, resource
contention and maintaining a schema that is compatible with every concurrently running version.

### Schema changes and backfills

Schema changes add, alter or remove database objects. Depending on the provider and operation, they
can acquire table or metadata locks that block request-serving reads or writes. Destructive changes,
such as dropping or renaming a column used by the old version, are unsafe while that version remains
active.

A backfill populates new storage from existing data. For example, a newly introduced lifecycle
table may need an initial record for every existing entity. Large backfills should not run as one
transaction because they can retain locks, create substantial transaction logs and compete with
login traffic for CPU and I/O.

Online backfills should:

- process bounded batches and commit each batch separately;
- be idempotent and restartable;
- record progress independently of a single database transaction;
- avoid overwriting rows already created or updated by live traffic; and
- validate migrated data before it becomes authoritative.

Database serialization alone does not prevent stale business state from winning. For example:

```text
1. A backfill reads an entity as ACTIVE.
2. A login-time operation suspends the entity.
3. The backfill writes the previously calculated ACTIVE state.
```

The backfill must use conditional inserts or updates, source versions or another conflict rule so a
stale migration result cannot overwrite a newer security decision. When initializing a new table,
inserting only missing rows is safer than unconditionally replacing existing rows.

### Expand, migrate and contract

A safe in-place migration normally follows an expand-and-contract sequence:

1. Add new tables, columns or indexes without removing objects required by the old version.
2. Deploy code that can operate with the expanded schema.
3. Backfill existing records in small batches while request traffic continues.
4. Validate the result and switch reads to the new representation.
5. Remove obsolete schema objects in a later release after no running version depends on them.

The intermediate schema must support both application versions if they run concurrently. Database
features advertised as online or concurrent still need provider-specific verification because some
operations acquire stronger locks during particular phases.

### Operational requirements

An in-place migration should define:

- the expected lock mode and maximum lock duration for every statement;
- lock and statement timeouts so the migration does not wait indefinitely;
- batching, retry and idempotency behavior;
- monitoring for lock waits, deadlocks, database load and login latency;
- compatibility rules for old and new application versions; and
- the rollback boundary after which a destructive change cannot safely be reversed.

In-place migration is best suited to additive, backward-compatible and bounded changes. Large or
incompatible transformations are harder because the migration competes directly with production
traffic and both application versions may need different representations of the same data.

## Database-copy migration

A database-copy migration creates a separate destination database and migrates the new ThunderID
version against that copy. The existing version continues serving traffic from the source database
until cutover.

```text
ThunderID old version > source database
                              |
                     snapshot plus CDC
                              |
                              v
ThunderID new version > destination database
```

The main advantage is isolation. Schema changes, data transformations and validation run against
the destination without locking the source tables used by login traffic. The main challenge is
transferring every change committed to the source after the snapshot without data loss or incorrect
ordering.

### Snapshot and change capture

A snapshot provides the initial source state at a defined point in its transaction history. Change
capture must start before, or be coordinated with, the snapshot so no commit can fall into a gap
between snapshot creation and change-data capture startup.

A safe sequence is:

1. Configure change-data capture and establish a source-log position.
2. Take a transactionally consistent snapshot.
3. Load and transform the snapshot into the destination schema.
4. Replay captured changes starting from the snapshot position.
5. Continue streaming changes while the old version serves traffic.
6. Wait for the destination to catch up and validate its state.
7. Transfer write ownership to the new version.
8. Apply changes through the final source position and route traffic to the new version.

### Generic change pipeline

The synchronization mechanism can be separated into provider-specific capture and a common
migration pipeline:

```text
Source transaction log
        |
        v
Database-specific CDC connector
        |
        v
Durable ordered stream
        |
        v
Migration transformer
        |
        v
Destination applier
```

PostgreSQL uses its write-ahead log, MySQL uses its binary log, SQL Server uses its transaction log,
and Oracle Database uses its redo log. After capture, a common change representation should expose
the source table, operation, primary key, before and after values, transaction identifier, source-log
position, order within the transaction and source schema metadata. `DEPLOYMENT_ID` remains part of
the captured ThunderID row and must be preserved during transformation and application.

### Transformation and destination application

When source and destination schemas are identical, an applier can translate captured inserts,
updates and deletes directly into destination operations. A schema-changing migration requires
version-specific transformation rules for table or column renames, split or combined tables,
changed data formats, derived values, deletes and foreign-key relationships.

Destination application must be idempotent because CDC delivery can repeat an event. Reapplying an
event must not corrupt data or create duplicates. The applier should persist its last successfully
processed source position so it can resume after a restart.

The pipeline must preserve the order of changes affecting the same record and any transaction-level
relationships required for consistency. Inserts and deletes must also be applied in an order that
respects foreign keys. A global order across unrelated entities is not necessarily required.

### Cutover and write ownership

The source remains the only application write authority during migration. The destination is
written by the migration pipeline until cutover. The handoff should:

1. Stop routing new writes to the old version.
2. Allow in-flight source transactions to finish.
3. Record the final source-log position.
4. Apply changes through that position.
5. Confirm that the destination has caught up.
6. Give the new version write ownership and route traffic to it.

This does not require a database outage, but it requires coordinated routing so old and new versions
do not independently accept writes. A gateway, deployment coordinator or ownership lease can
enforce which version currently owns writes.

### Validation and recovery

CDC lag reaching zero does not prove that a transformation is correct. Validation should include
deployment-scoped row counts, primary-key coverage, checksums for unchanged data, foreign-key and
uniqueness checks, sample comparisons and domain invariants. Deletes must be included in validation.

The migration should also define:

- source-log retention requirements;
- retry and idempotency behavior;
- handling for events that cannot be transformed;
- maximum acceptable CDC lag;
- monitoring and alerting; and
- the conditions that abort or postpone cutover.

The migration remains restartable without another snapshot only while the required source-log
history is retained. If that history is discarded, a new snapshot may be required.

### Rollback after cutover

Before cutover, the source remains authoritative and rollback is relatively simple. After the new
version writes to the destination, the source becomes stale. Returning traffic to it would lose
post-cutover changes unless those changes are synchronized back.

Possible approaches are reverse CDC, retaining a schema compatible with the old version, providing
a reverse transformation, or treating the migration as roll-forward-only after the first
destination write. Bidirectional synchronization requires conflict resolution and loop prevention,
so roll-forward recovery is normally simpler for transformations that are not reversible.

### Security-critical data

The generic pipeline should migrate lifecycle data through the same capture and application
mechanism as other mutable tables. Security-critical tables require stronger cutover validation: a
newer suspension or lock-until value must not be replaced by an older snapshot or event, and all
lifecycle changes through the final source position must be applied before traffic moves. A failure
to migrate such a record must prevent cutover.

Additional lifecycle-specific versions are required only if source-log ordering and destination
idempotency do not provide sufficient conflict protection.

Database-copy migration is best suited to incompatible schema changes, long-running transformations
or migrations that would place unacceptable locks or load on the source. It provides stronger
workload isolation than in-place migration, but requires CDC, transformation, validation and a
carefully coordinated single-writer cutover.

## Login data access

### Configuration database

The following tables may be read depending on the client, configured flow and enabled login
features:

| Table | Login purpose |
|---|---|
| `INBOUND_CLIENT` | Resolves the application-facing client and its authentication flow. |
| `OAUTH_INBOUND_PROFILE` | Resolves OAuth and OpenID Connect client settings, redirect URIs, grants and token configuration. |
| `FLOW` | Resolves the selected authentication flow and active version. |
| `FLOW_VERSION` | Loads the executable flow definition. |
| `CERTIFICATE` | Loads keys and certificates used to sign or verify protocol artifacts. |
| `SERVER_CONFIG` | Loads server-wide authentication and protocol settings. |
| `ENTITY_TYPES` | Resolves identity schemas and credential definitions when required by authentication. |
| `IDP` | Resolves external identity provider configuration for federated login. |
| `RESOURCE_SERVER`, `RESOURCE`, `ACTION` | Resolves audiences, scopes and permissions for authorization requests. |
| `ROLE`, `ROLE_PERMISSION`, `ROLE_ASSIGNMENT` | Resolves role-based authorization data when claims or policy evaluation require it. |
| `THEME`, `LAYOUT`, `TRANSLATION` | Renders the configured login experience. |

No request-serving write to these tables is inherently required for a login-only mode. Management
APIs and background or startup reconciliation that mutate configuration must be disabled or kept
outside the login-only process.

### Entity database

| Table | Access | Login purpose |
|---|---|---|
| `ENTITY_IDENTIFIER` | Read | Locates an identity from a username or another identifier. |
| `ENTITY` | Read | Loads attributes, credentials and system credentials. |
| `ENTITY` | Write, conditional | Updates passkey system credentials after successful authentication. It can also be written by registration or just-in-time provisioning flows. |
| `GROUP`, `GROUP_MEMBER_REFERENCE` | Read, conditional | Resolves group membership when authorization or claims require it. |
| `ORGANIZATION_UNIT` | Read, conditional | Resolves organizational context when a flow or claim requires it. |

Password verification itself is read-only. OAuth client-secret authentication also reads the
application entity and its system credentials without changing them.

Passkey authentication is different. After validating the assertion, the service persists the
updated WebAuthn credential, including its sign counter, through `UpdateSystemCredentials`. The
current implementation returns an internal server error if this update fails. Consequently,
read-only `entitydb` is incompatible with passkey login.

### Runtime-transient database

PostgreSQL stores these as partitions of `RUNTIME_STORE`. SQLite and other runtime-store providers
represent the same logical namespaces without necessarily using separate physical tables.

| Namespace | Access | Login purpose |
|---|---|---|
| `authz:req` | Read-write-delete | Holds the authorization request while the authentication flow executes. |
| `flow:state` | Read-write-delete | Holds short-lived flow execution state. |
| `attribute:cache` | Read-write | Carries identity attributes between authorization and token issuance. |
| `authz:code` | Read-write-delete | Creates and consumes single-use authorization codes. |
| `jti:token` | Read-write | Records JWT identifiers for replay prevention, including single-use authentication assertions. |
| `par:req` | Read-write-delete, conditional | Holds pushed authorization requests. |
| `webauthn:session` | Read-write-delete, conditional | Holds WebAuthn ceremony state for passkey operations. |
| `vp:state` | Read-write-delete, conditional | Holds OpenID for Verifiable Presentations state. |
| `ciba:req` | Read-write-delete, conditional | Holds Client-Initiated Backchannel Authentication requests. |

The `jti:token` write is required for single-use authentication assertions. An authentication
assertion is recorded when redeemed so it can be used only once. Store failure is handled by
rejecting the assertion, so successful authorization cannot depend on a read-only or unavailable
transient store.

### Runtime-persistent database

| Table | Access | Login purpose |
|---|---|---|
| `SSO_SESSION` | Read-write | Resolves, creates, refreshes and terminates SSO sessions. Fresh authentication in an existing session updates `AUTHENTICATED_AT`. |
| `SSO_SESSION_CONTEXT` | Read-write | Stores an authenticated checkpoint snapshot that lets a later SSO request resume after the authentication branch. |
| `SSO_SESSION_PARTICIPANT` | Read-write | Associates an application with the SSO session for logout and lifecycle handling. |
| `CONSENT` | Read-write, conditional | Stores the subject's consent record. |
| `CONSENT_AUTHORIZATION` | Read-write, conditional | Stores the permissions authorized by that consent. |
| `REVOKED_TOKEN` | Read-write, conditional | Stores explicit token revocations. |
| `REVOCATION_CRITERIA` | Read-write, conditional | Stores broader revocation conditions. |

`SSO_SESSION_CONTEXT` is not a duplicate of the transient flow context. It is a durable snapshot per
session checkpoint. The snapshot contains the runtime data, authenticated user, completed steps and
context version needed to rehydrate a later flow execution when SSO allows the authentication steps
to be skipped.

## Token issuance data access

Every token endpoint request first resolves and authenticates the OAuth client. This reads
`ENTITY_IDENTIFIER` and `ENTITY` from `entitydb`, and `OAUTH_INBOUND_PROFILE` and, when certificate
authentication is used, `CERTIFICATE` from `configdb`.

Additional access depends on the grant and enabled token features:

| Token issuance path | Additional database usage |
|---|---|
| Authorization code | Reads and consumes `authz:code`, reads `attribute:cache`, and may read `RESOURCE_SERVER`, `RESOURCE` and `ACTION`. Code replay handling may write `REVOCATION_CRITERIA`. |
| Client credentials | May read `RESOURCE_SERVER`, `RESOURCE`, `ACTION`, `ROLE`, `ROLE_PERMISSION` and `ROLE_ASSIGNMENT`. Group or organization claims may also read `GROUP`, `GROUP_MEMBER_REFERENCE`, `ORGANIZATION_UNIT` and `ENTITY`. |
| Refresh token | Reads token revocation state from `REVOKED_TOKEN` and `REVOCATION_CRITERIA`, may write revocation criteria for rotation or replay handling, and reads or refreshes `attribute:cache`. It may also read entity, client, resource, role and group data when claims or authorization are recalculated. |
| CIBA | Reads, updates and consumes `ciba:req`, reads `attribute:cache`, and may read resource authorization tables. The preceding CIBA authentication flow also uses transient flow state. |
| Token exchange | Reads revocation state for ThunderID-issued subject or actor tokens. External tokens may require `IDP`; authorization assertions write `jti:token`; resource authorization and subject classification may read configuration and entity data. |
| JWT bearer | Reads `IDP`, writes `jti:token` to enforce single use, and may read resource authorization and entity data. |
| DPoP or private-key JWT client authentication | Writes `jti:token` for replay prevention. Private-key JWT authentication also reads `CERTIFICATE`. |

Access tokens, ID tokens and refresh tokens are generated as JWTs and are not normally stored as
token records. The stored runtime data supports authorization-code and CIBA consumption, attribute
transfer, replay prevention and revocation checks.

The token endpoint does not normally access `SSO_SESSION`, `SSO_SESSION_CONTEXT`,
`SSO_SESSION_PARTICIPANT`, `CONSENT` or `CONSENT_AUTHORIZATION`. Those tables belong to the login,
session and consent work that precedes token issuance.

For broad token issuance support, `configdb` and `entitydb` can be read-only, while
`runtime_transient` must remain read-write. `runtime_persistent` must remain read-write when refresh
token rotation, replay response or other revocation behavior can create revocation records. These
requirements cover token issuance itself; the preceding login flow can still require entity and
persistent-runtime writes.

## Authentication methods with read-only entitydb

Read-only in this section refers specifically to `entitydb`. A complete OAuth or OpenID Connect
login still requires read-write `runtime_transient`, and enabled SSO or consent behavior still
requires read-write `runtime_persistent`.

| Authentication method | Works with read-only `entitydb` | Conditions |
|---|---:|---|
| Password | Yes | The identity already exists and the flow does not update, recover or reset credentials. |
| Email or SMS OTP | Yes | The identity already exists and notification delivery remains available. OTP generation and verification do not update entity credentials. |
| Magic link | Yes | The identity already exists and the flow does not provision one. Token generation and verification do not update entity credentials. |
| Generic OAuth federation | Yes | The subject resolves to an existing identity, or authentication without a local identity is explicitly allowed, and just-in-time provisioning is disabled. |
| Generic OIDC federation | Yes | The subject resolves to an existing identity, or authentication without a local identity is explicitly allowed, and just-in-time provisioning is disabled. |
| Google authentication | Yes | The same restrictions as other federated OIDC authentication apply. |
| GitHub authentication | Yes | The same restrictions as other federated OAuth authentication apply. |
| OpenID4VP | Yes | The verified holder resolves to an existing identity, or authentication without a local identity is explicitly allowed, and provisioning is disabled. Its `vp:state` transient namespace remains read-write. |
| SSO session reuse | Yes | It does not require an entity update, but SSO session and participant state in `runtime_persistent` remains read-write. |
| Passkey | No | Successful authentication updates the WebAuthn credential, including its authenticator sign counter, in `ENTITY.SYSTEM_CREDENTIALS`. |

OAuth client authentication can also use read-only `entitydb`. Client-secret authentication reads
the application entity's system credentials. Private-key JWT authentication does not update the
application entity, but replay prevention writes its JTI to `runtime_transient`.

Federated and OpenID4VP authentication remain entity-read-only by themselves. They become
entity-writing flows only when a `ProvisioningExecutor` creates a local identity. Registration,
credential-setting and custom entity-mutating executors are also incompatible with read-only
`entitydb`.

## Reduced-capability alternatives

A narrower mode could use fewer write permissions, but it must explicitly reject unsupported login
paths rather than allowing them to fail after a request begins.

### Read-only entity database

This is possible only if the mode excludes all entity mutations, including:

- passkey authentication;
- passkey enrollment;
- registration;
- just-in-time user or agent provisioning;
- credential reset, update or recovery; and
- flows containing custom executors that mutate entities.

Password authentication for an existing identity can then use `entitydb` read-only. The transient
runtime store still requires read-write access.

### No persistent runtime writes

This is possible only for a deliberately stateless login profile that disables SSO session creation
and reuse, consent persistence, logout participation and database-backed revocation behavior. The
current general-purpose login experience should not be assumed to satisfy those restrictions.

## Implications for the login-only design

The mode needs to be defined as an allowed-capability boundary, not only as a database permission
switch. At minimum, implementation must address:

1. Disable management endpoints and non-login background work.
2. Prevent declarative configuration reconciliation from writing during startup.
3. Validate database compatibility before enabling request handling.
4. Give `configdb` a read-only connection or credentials during login-only serving.
5. Retain read-write connections for transient state and for every enabled persistent login feature.
6. Reject configured flows whose executors require writes that the selected mode does not allow.
7. Keep health and readiness checks sensitive to every store required by the enabled login profile.

## Decisions still required

Before implementation, agree on whether "login-only" includes:

- passkeys;
- federated login with just-in-time provisioning;
- user and agent registration;
- SSO reuse, `prompt=none`, `prompt=login` and `max_age`;
- consent collection;
- authorization-code redemption and refresh tokens;
- token exchange, CIBA, PAR, OpenID4VP and OpenID4VCI;
- logout and token revocation; and
- custom flow executors.

The broad interpretation supports existing authentication behavior and requires all stores except
`configdb` to remain read-write. Any smaller permission set requires a named, validated subset of
features.

## Implementation references

The main implementation points supporting this assessment are:

- [`backend/internal/oauth/oauth2/authz/service.go`](backend/internal/oauth/oauth2/authz/service.go),
  authorization request persistence, SSO-aware `prompt=none` handling and single-use assertion
  consumption;
- [`backend/internal/oauth/oauth2/jti/store.go`](backend/internal/oauth/oauth2/jti/store.go), replay
  records written to the `jti:token` namespace;
- [`backend/internal/flow/session/service.go`](backend/internal/flow/session/service.go), SSO session,
  checkpoint and participant persistence;
- [`backend/internal/authn/passkey/service.go`](backend/internal/authn/passkey/service.go), passkey
  session handling and credential sign-counter persistence;
- [`backend/internal/oauth/oauth2/clientauth/clientauth.go`](backend/internal/oauth/oauth2/clientauth/clientauth.go),
  OAuth client lookup and client-secret authentication; and
- [`backend/dbscripts`](backend/dbscripts), the current PostgreSQL and SQLite schemas for all four
  logical stores.
