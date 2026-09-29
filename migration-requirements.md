# Requirements: Upgrading between ThunderID versions

Working requirements, parked for further fundamental research. The requirements body includes the
latest trust-based initialization and independently finalized-store decisions. Earlier working notes
and change-log entries are historical context where later revisions explicitly supersede them.

Published draft. Context, R1 to R25 and change log 0.7 are posted for the Specification pass-one read. The refinements in change log 0.8 to 0.10 are currently local and pending publication.

- Stage: Specification, pass one.
- Feature issue: [#5208](https://github.com/thunder-id/thunderid/issues/5208)
- Discussion: [#5271](https://github.com/thunder-id/thunderid/discussions/5271), id `D_kwDOOi7AYc4Ao-Zv`
- Posted as a comment: [discussioncomment-18281435](https://github.com/thunder-id/thunderid/discussions/5271#discussioncomment-18281435)
- Comment id for updates: `DC_kwDOOi7AYc4BFvPb`
- Published comment holds Context plus R1 to R25, change log at 0.7.
- Local postable content includes the pending 0.8 refinement and therefore does not yet match the published draft.
- Pass one ends in a read by the Area Owner and the PM, not an approval.

## Requirement index

R1 to R25 remain draft until the Specification pass-one read is complete. Their numbers are
permanent because the design's traceability table, delivery tasks and tests will reference them.
Never renumber; append, and mark a withdrawal as withdrawn.

| Candidate | Status |
|---|---|
| Continuity across an upgrade | **R1, draft** |
| Upgrading with no instance serving | **R2, draft** |
| Unattended operation | **R3, draft** |
| Migration separated from serving | **R4, draft** |
| Recorded migration level of each database | **R5, draft** |
| Compatibility check before serving | **R6, draft** |
| Fresh creation and upgrade produce the same structure | **R7, draft** |
| Built-in resources reaching existing installations | **R9, draft** |
| Declared upgrade paths | **R8, draft** |
| Refusal when migration cannot be performed safely | **R15, draft** |
| Published migration notes per release | **R11, draft** |
| Checking whether an upgrade can proceed | **R12, draft** |
| Report of a migration run | **R13, draft** |
| Recovery after a failed upgrade | **R14, draft** |
| Migration operation integrity | **R10, draft** |
| Configuration changes across a release | **R16, draft** |
| Declarative resources across a release | **R17, draft** |
| Upgradeability verified before release | **R18, draft** |
| Migration impact declared by every release | **R19, draft** |
| Declaring what an existing installation needs | **R20, draft** |
| Documented upgrade procedure | **R21, draft** |
| Reaching the current release from an older one | **R22, draft** |
| Verified migration operations | **R23, draft** |
| Deployment-scoped data in shared stores | **R24, draft** |
| Establishing the initial migration level | **R25, draft** |

The postable Context and all requirements await the pass-one read.

## Requirement provenance

- **Seeded directly by the Feature Brief:** R1, R3, R11 to R14, R16 to R21.
- **Settled through the author's answers during Specification:** R2, R4 to R10, R15, R22 and R23.
- **Drafted from the author's latest concerns and repository investigation, and awaiting confirmation:** R24 and R25, together with the 0.7 refinements to R2, R5, R6, R10, R14, R15, R20, R22 and R23.

## Working notes for Context

**Vocabulary.** Terms used precisely:

- **Instance** - a running ThunderID process, serving requests. Has a product version.
- **Data** - the databases an instance runs against. Has its own recorded state, and this is what migration changes.
- **Installation** - the two together, as the administrator owns them. Used only when speaking about the whole.
- **Deployment** - one logical ThunderID environment identified by the stable `server.identifier` value. All replicas of that deployment use the same identifier.
- **Replica** - one running instance of a deployment. Replicas share the deployment identifier and its data.

An upgrade is two things: replacing the software, which the administrator does, and bringing the data forward, which is the only part migration performs.

**Accepted risk.** The product does not detect that an instance is serving the data being migrated. Detection from the database side cannot dependably tell a ThunderID instance from any other client, so a check would be heuristic and would give false confidence. Stopping affected instances and maintaining a recoverable backup are documented administrator responsibilities, reinforced in command output, not mandatory acknowledgements. Consequence: a careless administrator can migrate beneath a live instance or without a suitable backup and the product will not stop them.

## Working design record

Agreed design decisions are maintained separately in
[`migration-design-decisions.md`](migration-design-decisions.md). They are not part of the postable
requirements body.

## Candidate design considerations, not agreed

These notes preserve repository findings and implementation possibilities for later discussion. They are not part of the postable requirements and do not represent an agreed design. The postable body below is authoritative for Specification pass one.

- The record held with a logical database is independent evidence of the level actually completed there. Startup compares it with the required level in the embedded release manifest; reading the manifest alone says nothing about the connected data.
- The serving process performs this comparison read-only and never creates, stamps, migrates or repairs migration state. A separate initialization or migration path performs authorised writes.
- The release build executes the SQLite creation scripts and packages the resulting database files, while PostgreSQL schemas are administrator-created from the shipped scripts. Both providers use the same verified fresh-initialization action during setup before bootstrap or serving. A pre-created schema is not evidence of an established migration level, and SQLite has no separate stamping mechanism. If release assembly also initializes a packaged database, it must use that same action; setup validates any existing compatible state without rewriting it.
- Initialize uses the agreed trust-based policy: absent metadata tables register fixed clean baseline 1.0.0; missing rows in existing supported tables register manifest target levels; existing records are preserved. No separate adopt-baseline command or baseline validators are required.
- The initialization action must run before the existing one-shot bootstrap creates default resources. Migration compatibility and completion of deployment-scoped bootstrap are distinct states and must not be inferred from each other.
- Stamping must insert only, never update, so an older distribution cannot rewrite a newer level downward.
- Deploy versus start: migration belongs to deploying a new version, which happens once. Only the read-only check belongs to starting a process, which happens on every restart. Invoking the client on every start would require structure-altering credentials to be present permanently in the serving environment, would let replicas race, and would turn a 3am eviction into a migration.
- Trigger surfaces: a separate command in the distribution, alongside `start.sh`, with a Windows counterpart since the distribution ships `start.ps1` and `setup.ps1`. On Kubernetes a job that runs once per rollout rather than an init container, which would re-run on every pod restart. The R6 refusal message must name the command to run, or a developer hits a wall with no next step.
- Persistence of the level: each logical database owns its state, but several logical databases may share one physical database. The same status table can therefore contain independently keyed rows for each logical database. A separate migration database is not required. The shared metadata installer establishes equivalent metadata structures for every supported relational provider.
- Reports versus the record: the record in each database is authoritative and is what tooling reads. A report to terminal or file is for the human and is a convenience, because it can be lost when the client is killed or the pod evicted, or never seen when someone else runs the client next.
- Forward-only was chosen over rollback on failure. Reverse steps are the same machinery the brief already excluded, cannot cover destructive steps, and there is no atomic commit across four databases so the rollback sequence can itself fail part way. Recovery is fix-and-resume or restore from backup.
- Consequence of strict level equality in R6: once data has been migrated, the binary cannot be rolled back on its own. Returning to the previous version means restoring the data too. Belongs in documentation and in the recovery requirement.
- R7 covers structure only. Content the product ships, such as built-in default flows and roles, is created by setup rather than by the create scripts, which hold no INSERT statements today. A release adding a new built-in default would therefore reach a fresh installation but not an upgraded one, and R7 would not catch it. Handled as its own requirement rather than by widening R7, because structure comparison is exact and mechanical while content comparison is not.
- Level semantics: a logical database's level is the last release that changed it, not the version of the product. A fully upgraded 1.3.2 installation can legitimately read configdb at 1.1.4 if later releases changed nothing there. Levels have full semantic-version granularity because patch, minor and major releases may carry migrations.
- Indexing chosen: the release version that last changed the database, rather than a per-database counter or a global step sequence. A counter needs a shipped mapping table to be interpretable, which is one more artifact to drift; a global sequence has gaps per database and reads poorly. Version-shaped levels are legible without any lookup.
- Steps are applied in strict release order across all affected logical databases, because cross-database dependencies are possible. Consequence: a partial failure is inconsistent rather than merely uneven, and resume must continue at exactly that point in the release order rather than restarting the sequence. Per-database levels plus the incomplete marker encode enough to determine that point.
- Atomicity is an operation capability, not a universal property of a level. An atomic operation rolls back with its transaction. A batched, service-layer, cross-deployment or otherwise non-atomic operation persists scoped progress and resumes from its last verified checkpoint. The logical database level advances only after every operation has completed and passed post-verification.
- No release may require an intermediate version to have been run. Not stated as its own requirement: it follows from R4, which bars the serving process from applying any step, and from R8's third criterion. Between them, a change relying on the application converting data in the background is closed off.
- Three categories of migration, each landing on a different requirement: config migration is manual, with new keys shipped with defaults for the administrator to review and set in their own deployment.yaml; schema migration goes to the database through the client; data migration may need the product's own services and therefore the target version's code.
- Every database-related change goes through the client, so one approach is maintained rather than several.
- Because data steps may need ThunderID's services, the client and the release it migrates to must be the same vintage. An argument for shipping the client inside the distribution rather than versioning it separately.
- Two kinds of step: structure and content steps applied straight to the database by the client, and resource steps applied through the service layer by running the target version one-shot and non-serving, which is what `start.sh --bootstrap` already does.
- Built-in resources are created today by a templated YAML bundle applied through the import service, which upserts. Re-running the bundle therefore resets any customised default back to shipped state, which is why R9 prohibits wholesale re-application.
- Universal step-level idempotence is not required. Atomic operations rely on rollback. Resumable operations must classify each scope as source, target or invalid, record committed progress with the affected data where possible, and safely skip a scope already verified at the target state.
- In-progress detection must be recorded in the data rather than on the machine that started the run, or two machines will not see each other. Follows from R10's refusal of a concurrent attempt.
- Configuration strictness, verified in the repository: the loader uses `decoder.KnownFields(true)` in `backend/internal/system/config/config.go`, so an unknown key is a hard failure today. There is no silent-drift problem; the problem is that a leftover key from an older release stops the new version starting.
- Decision on strictness: known-removed and renamed keys are accepted, ignored and warned about; genuinely unknown keys still fail. Blanket leniency was considered and rejected because it turns a typo into silent misconfiguration, which on an identity product means a mistyped secret, TLS setting, token lifetime or allowed origin silently reverting to a default with nothing to indicate the intended value is not in effect. Warn logs do not cover that, since they are read after the fact. Open item flagged by the author: if blanket leniency is still wanted, raise it separately as its own discussion, since it changes existing security-relevant behaviour.
- Consequence: developers must record a key when they remove or rename it, so it can be recognised as deprecated rather than unknown.
- Patch migration policy resolved: any patch, minor or major release may carry a migration. The release manifest, not the semantic-version category, tells tooling and administrators whether each logical database changes. This keeps patch delivery flexible while preserving an explicit upgrade contract.
- Reversal, recorded so the earlier position does not look forgotten. An earlier draft of R8 allowed a single run to span several releases, on the strength of how other products behave. That was withdrawn. Two problems were under-weighted. First, a task written to tolerate any prior state does remove version branches from the code but moves the combinations into the test matrix, which is what gets trimmed when a pipeline is slow. Second, a "frozen" Go step compiled into a newer client still calls shared helpers from that newer codebase, so freezing is discipline rather than a guarantee. The deciding argument was not technical: loosening this rule later is easy, withdrawing it later is not, and ThunderID has one release so being strict now costs nothing. Spans can be revisited once there are real releases and evidence about which migrations turn out to be convergent in practice.
- Consequence: the convergent-versus-sequential distinction stays as guidance for writing clean migrations, but stops being load-bearing for correctness across spans.
- The operator cost of stepping is paid by R22 rather than by relaxing R8. The CLI already fetches releases, so the chain can be driven for the administrator: fetch, migrate, fetch, migrate, each hop using its own client and binary. Correctness is identical to stepping by hand because it is stepping by hand.
- A single hop is the product; the walk is convenience. Every requirement except R22 concerns one hop. R22 phases later and is genuinely optional, because one hop done well is a complete answer for every deployment path.
- Stepping suits automated deployment better than manual. On Kubernetes or OpenChoreo, walking four releases is a loop in the pipeline: set the image tag, run the migration job, repeat. The burden falls on manual upgrades, which is the case the CLI covers.
- The CLI reaches people only through npm today. It is built separately into `tools/cli/dist`, copied into `tools/npx/dist`, and is absent from the product distribution, which carries the server binary, deployment.yaml, config, dbscripts, scripts, bootstrap and the frontend apps. Until that packaging changes, the walk is unavailable to anyone who does not want Node.js involved. The CLI also only knows about installations it manages, tracked in `~/.thunderid/state.json`, which is why R22's walking criterion is scoped to those.
- `releases.json` at `https://thunderid.dev/data/releases.json` already carries a `releases` array. Nothing consumes it as an ordered sequence yet, and that is what R22's first criterion needs.
- Restart loops do not re-enter migration because serving startup is read-only. On Kubernetes, migration runs once as an explicitly authorised rollout job; restarted serving pods only repeat the compatibility check.
- Fresh creation today, verified in the repository: SQLite databases are built at build time from `backend/dbscripts/<db>/sqlite.sql` and ship as ready-made files; PostgreSQL schema is applied by the administrator from `backend/dbscripts/<db>/postgres.sql`. Nothing creates schema at first startup. The create scripts remain the fresh-install path.
- Multi-deployment completion: when a logical database contains deployment-scoped data, its release level advances only after the required work and verification have completed for every deployment represented in that database.
- The first migration delivery is scoped to relational stores backed by PostgreSQL and SQLite. Redis migration is a separate follow-up phase and must not be represented as covered by the initial delivery.
- Each migration operation has one developer-authored definition containing plan-wide readiness, read-only pre-verification, execution and exact post-verification. The whole preflight check runs every pending operation's readiness check and discloses exact checks deferred until preceding operations complete. Apply performs exact pre-verification immediately before execution after establishing the store reservations.
- Framework ownership: migration orchestration is implemented and shipped within ThunderID, using the existing database providers and standard Go facilities. Goose and golang-migrate are not required dependencies. This avoids outsourcing ThunderID's cross-store, deployment-scoped verification contract to a SQL-file runner.
- `server.identifier` is a process-wide data namespace. It identifies a logical deployment, is shared by all replicas of that deployment, and remains stable across restarts and upgrades. Changing it exposes a different namespace and is not a version migration.
- `DEPLOYMENT_ID` isolates rows and Redis or cache keys, but not relational schema. All deployments sharing an affected physical store form one migration coordination group and must be out of service while that store changes.
- Migration state is store-scoped system metadata and is not keyed by `DEPLOYMENT_ID`. Detailed progress may be deployment-scoped when an operation must process deployments separately.
- ThunderID has no authoritative registry of deployments sharing a store today. A deployment-scoped operation therefore discovers affected identifiers from the data it changes and proves completion with a store-wide post-verification. It must not assume the migration client's configured identifier is the only one present.
- Current service and store instances bind to the configured deployment identifier at initialization. A migration that requires service-layer behavior for several deployments needs migration-only adapters that accept an explicit deployment scope; it must not mutate the process-wide runtime configuration to impersonate another deployment.

<!-- POSTABLE BODY BELOW -->

# Requirements - Upgrading between ThunderID versions

## Context

ThunderID has no upgrade process. An administrator running an instance against their own database can point a newer version at it, and that is the whole of the procedure. Nothing detects that the newer version expects the database organised differently, or expects what is already stored in a different form, nothing corrects it, and there is no documented path to follow. Whether an upgrade works depends on whether that release happened to change anything, and a clean failure is not guaranteed either.

The release of v1.0.0 changed what that costs. Administrators are being asked to put the identity data their organisation depends on into ThunderID, which is a stability promise the product has no means of keeping. The other half of the problem is already present: changes to what ThunderID stores are made during development with no obligation to bring existing installations along, because no process asks for one. Every such change is a break waiting for the next release to deliver it.

The timing follows from those two facts together. Development does not pause, so the first release that needs an existing installation brought forward is coming. A process introduced later cannot cover the releases that shipped before it, because there is no record of what those releases did to anyone's data and no path back through them. Delay does not postpone this work, it removes the option of doing it properly. In these requirements, a logical database is one ThunderID-owned relational store; a physical database is the PostgreSQL database and schema or SQLite file that may hold one or more of those stores. The initial delivery covers PostgreSQL and SQLite. Redis migration is deferred to a separate phase.

## Requirements

### Requirement 1 - Continuity across an upgrade

> As an administrator, I want everything ThunderID holds for me to still be there after an upgrade, so that upgrading costs me the maintenance window and nothing else.

- Anything that survives a normal restart of ThunderID must survive an upgrade.
- After the upgrade, existing configuration data and identity data must be intact and usable, with no manual data handling required of the administrator.
- Work in progress at the moment the installation is taken out of service need not survive the upgrade.
- Where a release cannot carry something forward that would otherwise survive a restart, that must be declared for the release and disclosed to the administrator before the upgrade begins.
- Nothing that would otherwise survive a restart may be discarded without having been declared and disclosed.
- The structure of every in-scope relational database ThunderID uses must be brought forward, including relational databases holding only short-lived data. No in-scope database is exempt on the grounds that its content is short-lived.
- A release may migrate one logical database, several logical databases, or all of them. A logical database not changed by that release retains its existing level.
- A release may bring a database forward by recreating its structure rather than altering it. The resulting content loss falls under the declaration rule above.
- Recreating must not be used for a database that holds anything other than short-lived data.

### Requirement 2 - Upgrading with no instance serving

> As an administrator, I want to bring my data forward while nothing is serving it, so that the upgrade is not competing with live traffic and I can plan a maintenance window.

- Bringing the data forward must not require a ThunderID instance to be running or reachable.
- Migration documentation and preflight and execution output must remind the administrator to stop all affected instances before migration. Ordinary apply and resume must not require an offline acknowledgement flag or prompt.
- The documentation must state that ensuring no instance is serving is the administrator's responsibility, and that this covers every instance in every deployment sharing any affected physical database.
- The product must not represent an upgrade as performable without interruption unless that has been stated for that release.

### Requirement 3 - Unattended operation

> As an administrator, I want to run migration from my own deployment automation, so that upgrading fits the way I already deploy rather than requiring me to be present.

- Migration must be able to run with no person present and no interactive input.
- Explicit authorization for exceptional recovery actions, such as taking over a stranded run or abandoning progress, must be expressible in advance. Ordinary apply and resume must not require operational acknowledgement flags or interactive prompts.
- Where a required acknowledgement has not been given, migration must refuse rather than wait for input.
- Whether migration succeeded or failed must be determinable by whatever invoked it, without anything having to read or parse a report.
- What migration did must be available both in a form a program can consume and a form a person can read.

### Requirement 4 - Migration separated from serving

> As an administrator, I want the serving process never to alter my data's structure, so that it can run without holding rights to change my database.

- The serving process must not create or alter the structure of the data.
- The serving process must not apply any migration step, including one that changes only content.
- Migration must be performed through a dedicated non-serving operation.
- That operation may be invoked as part of deploying a new version, and must migrate only where the administrator has authorised migration in advance. Absent that authorisation, it must report what is needed and stop.
- Migration must not run as part of starting a serving process, including on restart.
- The serving process must be able to operate with database credentials that do not permit altering the structure of the data.

### Requirement 5 - Recorded migration level of each logical database

> As an administrator, I want each logical database to state how far it has been migrated, so that the level travels with the data even when stores are backed up or restored separately.

- Each logical database must have an authoritative record stating the migration level it has been brought to.
- Backing up, copying or restoring a logical database must carry its recorded level with it.
- Where several logical databases share one physical database, their records must remain independently identifiable and independently updatable.
- One authoritative record must apply to the logical database as a whole. Deployments sharing that logical database must not have separate completed levels.
- Each record must be readable without a ThunderID instance running.
- A newly created database must have its record set to the level of the release that created it only after the database has been verified as a fresh database with the structure that release requires.
- Absence of the record must invoke the initial-level procedure in R25. It must not by itself be treated as proof that the database is fresh or at the v1.0.0 baseline.
- A logical database's record must be advanced only after every step for that level has completed successfully in that logical database.
- Where a migration changes deployment-scoped data, the logical database's record must be advanced only after the required work and verification have completed for every deployment represented in it.
- Where an interrupted operation rolls back completely, the record must remain cleanly at its previous level.
- Where committed work may remain after an interruption, the database must retain enough persistent progress to resume safely and must not read as complete at the target level.
- Migration and startup reports must show every logical database's level independently. They must not derive compatibility from a lowest aggregate level; each recorded level must be compared with the level that the release requires for that logical database.

### Requirement 6 - Compatibility check before serving

> As an administrator, I want an instance to check every database before completing startup, so that a missed, incomplete or mismatched migration is caught by the product rather than by my users.

- On startup, an instance must compare the level recorded in each database against the level that release requires of that database.
- The compatibility check must be a mandatory gate in normal serving startup. It must complete before the process activates request listeners, background workers or any other application behavior that depends on compatible persistent data.
- The level required of each database must come from the release's own declaration. The level recorded with the database is independent evidence of what has completed there. Neither value may be supplied through the administrator's configuration surface.
- Where any database's recorded level does not match the level required of it, the process must terminate startup and must state which databases mismatched, the recorded and required levels, and in which direction.
- Where any database is marked as left incomplete, the process must terminate startup and must name it, whatever level is recorded against it.
- Where compatibility cannot be established for every required database, including because a migration record is missing or invalid, the process must terminate startup. It must not remain running in a partial-serving or degraded mode.
- A refusal must state, per mismatched database, the recorded level and the required level, and must point to documentation covering the change between those versions.
- The check must be read-only: the instance must not create, infer, stamp, repair or otherwise modify any record or data as a result of performing it.

### Requirement 7 - Fresh creation and upgrade produce the same structure

> As a developer, I want it verified that newly created data and upgraded data end up structurally identical, so that a defect cannot depend on how an installation came to exist.

- For every release, it must be verified that creating data at that release produces the same structure as creating data at the baseline and then applying every migration up to that release.
- Same structure means tables, columns, types, nullability, defaults, keys, indexes and constraints, including their names.
- The verification must cover every in-scope logical database in both PostgreSQL and SQLite.
- The verification must run against the shipped distribution rather than source alone.
- The verification must confirm that both paths end with the same recorded migration level, and that it is the level the release declares as required.
- A release must not be published where this verification does not pass.

### Requirement 8 - Declared adjacent-release upgrade paths

> As an administrator, I want to know which version I can upgrade from, so that I can plan how to reach the release I want.

- A release must declare the source level it accepts for each database.
- A release must accept as its source the levels left by the immediately preceding published release, whether the target is a patch, minor or major release.
- Migration must refuse any source older than the accepted one, before making any change, and must name the version the administrator has to install next.
- Reaching a release from its accepted source must require only the migration capability supplied with that target release.
- The documentation must state the ordered chain of versions to pass through when upgrading from an older release.

### Requirement 9 - Built-in resources reaching existing installations

> As an administrator, I want changes to ThunderID's built-in resources to reach my existing installation, so that upgrading leaves me with the same built-in behaviour as a fresh installation of the same release.

- Where a release changes a built-in resource, and an existing installation requires that change in order to run the release correctly, migration must apply it.
- Such a change must identify the specific resource and the specific change required, and must preserve administrator changes outside that declared change.
- Wholesale re-application of the shipped resource set must not be used to deliver such changes to an existing installation.
- Where the resource is absent from an existing installation, it must be created only where the release declares that resource mandatory. An absent resource the release does not declare mandatory must be left absent.
- The developer must declare, per change, whether an existing installation requires it, and whether the resource is mandatory. Changes that are only conveniences need not be applied.
- A change to a built-in resource must also be reflected in what a fresh installation creates, so that a fresh installation and an upgraded installation of the same release hold the same built-in resources.

### Requirement 10 - Migration operation integrity

> As an administrator, I want each level applied once, in order, and recoverably, so that an interruption does not make the outcome depend on how many times migration was attempted.

- Levels must be applied in strict release order across all databases.
- A level recorded as completed for a database must never be applied to it again.
- Every operation must declare whether an interruption leaves no partial change or may leave committed progress that requires resumption.
- An operation declaring no partial change must leave its data and completion state either fully at the source state or fully at the target state after an interruption.
- An operation that may leave committed progress must retain enough durable state to identify what completed at each required scope.
- Re-running such an interrupted operation must verify and continue from its last completed unit rather than repeat completed work.
- An operation that may encounter partially completed work must be able to classify each required scope as still at its source state, already at its target state, or invalid. It must safely skip only a scope verified at its target state.
- A logical database's level must advance only after every operation for that level has completed and passed post-verification for every required scope.
- Where a level can be established as neither completed nor cleanly absent for a database, that database must be marked incomplete, and migration must not proceed past it until the administrator has resolved it.
- Where migration is already running against a database, a second attempt must be refused and must state that one is in progress.

### Requirement 11 - Published migration notes per release

> As an administrator, I want to read what an upgrade will change before I plan it, so that I can decide when to schedule it and what to prepare.

- For every release that carries migration, the documentation must state what it changes, per database.
- The notes must be addressable by source version and target version, so that an administrator upgrading across several versions can see the changes between them.
- Where a release cannot carry something forward, or discards content, that must appear in these notes.
- The notes must be published no later than the release itself.

### Requirement 12 - Checking whether an upgrade can proceed

> As an administrator, I want to find out whether an upgrade can proceed before I open a maintenance window, so that I do not discover a blocker inside it.

- The migration facility must be able to run the checks that migration performs and report the result, without applying anything.
- The check must invoke the plan-wide readiness check defined by every pending migration operation and disclose exact verification deferred until a preceding operation completes.
- The check must report every condition that would prevent migration from proceeding, not only the first one encountered.
- The check must report, per database, the recorded level and the level required.
- The check must not alter any data or any record.
- The check must not be represented as a guarantee that migration will succeed.
- Running the check must be optional. Migration must not require that one has been run.

### Requirement 13 - Report of a migration run

> As an administrator, I want a report of what a migration run did, so that I can confirm the outcome or investigate a failure.

- Every migration run must produce a report, whether it succeeded or failed.
- The report must state, per database, the level it started at, the level it reached, and the outcome.
- Where a run failed, the report must name the database and the level it failed on, and state what the failure was.
- The report must cover the run just performed. Retaining a history of earlier runs is not required.

### Requirement 14 - Recovery after a failed upgrade

> As an administrator, I want a defined way back to a working installation when an upgrade fails, so that a failure is recoverable rather than terminal.

- Documentation and preflight and execution output must state that maintaining a recoverable backup is the administrator's responsibility. Ordinary apply and resume must not require a backup acknowledgement flag or prompt. Before resuming, the administrator must ensure that the intended recovery backup remains suitable; a backup of partially migrated data is not a substitute for the original recovery point.
- Where migration fails, the report must state what the administrator can do next, not only what went wrong.
- The documentation must publish the recovery procedure for a failed migration, covering both resuming once the cause is fixed and restoring from backup.
- The documentation for each migration-bearing release must state whether its operations recover by leaving no partial change, resuming retained progress, or restoring from backup. ThunderID must not promise universal automatic rollback.
- The documentation must state that once data has been migrated the previous version can no longer run against it, so returning to that version requires restoring the data as well.
- The documentation must cover reconciling an installation whose databases have been restored to differing levels.

### Requirement 15 - Refusal when migration cannot be performed safely

> As an administrator, I want migration to refuse to begin when it is unsafe and stop safely if a later failure prevents progress, so that it does not report an incompatible database as migrated.

- Migration must refuse, before making any change, where it cannot read the recorded level of a database that holds content.
- Migration must refuse where a database's recorded level is not one the release recognises.
- Migration must refuse where a database's recorded level is beyond the target level.
- Where a database is already at the target level, migration must make no change to it and must report that, rather than treating it as an error.
- Migration must refuse where it cannot reach a database it would have to change.
- Preflight must verify every required permission the target store can expose without making a change, and must report any permission that cannot be established read-only.
- Migration must stop on a permission failure and must not advance the affected logical database's level.
- Migration must refuse where a pending operation's pre-verification does not observe the expected source state.
- Every refusal must state which condition was met and what the administrator can do about it.

### Requirement 16 - Configuration changes across a release

> As an administrator, I want to know what a new version expects of my configuration, so that my own configuration is not what stops the upgrade.

- For every release, the documentation must state which configuration keys are added, renamed, removed, or have had their default changed.
- A newly added key must ship with a default, so that an administrator content with the default need do nothing.
- A key that a release has removed or renamed must be accepted and ignored, and must produce a warning naming the key and its replacement where there is one.
- A key the release does not recognise at all, whether current or previously removed, must be refused rather than ignored.
- Where the product refuses to start because of the administrator's configuration, the message must name the keys at fault and state what is required.
- The product must not modify the administrator's configuration.
- Status and the pre-upgrade check must display configuration release-change summaries and links to the release's migration documentation without modifying files or claiming deployment-specific applicability.
- The pre-upgrade check should reuse existing target-release validators for supplied configuration files where available, without starting the product. Changes not covered by existing validation require explicit manual-review notices, not a custom detector for each release change.

### Requirement 17 - Declarative resources across a release

> As an administrator, I want my declarative resource files checked against a new version before I upgrade, so that I find out from the product rather than from a failed start.

- Status and the pre-upgrade check must display declarative-resource release-change summaries and links to the release's migration documentation. Applicability is not inferred from those notices.
- The pre-upgrade check should reuse existing target-release resource validators for supplied files where available. Detected validation failures must identify each affected file and the reason, without stopping at the first file failure.
- Missing files or changes not covered by existing validation must be reported as requiring manual review. The output must identify the files inspected and must not claim complete deployment compatibility. A custom detector for each release change is not required.
- For every release, the documentation must state what has changed in the shape of declarative resources.
- A field that a release has removed or renamed must be accepted and ignored, and must produce a warning naming the field and its replacement where there is one.
- A field the release does not recognise at all must be refused rather than ignored.
- The product must not modify the administrator's declarative resource files.
- The documentation must state that keeping declarative resource files up to date with the release is the administrator's responsibility.

### Requirement 18 - Upgradeability verified before release

> As an administrator, I want every release to have been proven upgradeable from the version it accepts, so that my upgrade is not the first time anyone has tried it.

- For every release, it must be verified that an installation at its accepted source level can be brought forward to that release and then serve.
- The verification must exercise the migration operation supplied with the release, not only the resulting structure.
- The verification must cover PostgreSQL and SQLite.
- A release must not be published where this verification does not pass.

### Requirement 19 - Migration impact declared by every release

> As an administrator, I want every release to declare whether it requires migration, so that patch, minor and major upgrades all follow an explicit and predictable process.

- A patch, minor or major release may change one or more logical databases and may therefore require migration.
- Every release must declare the level it requires for each logical database and whether that release changes it.
- A release that changes no logical database must preserve their required levels and explicitly declare that no migration is carried.
- A release that carries migration must use the same check, administrator backup responsibilities, offline operation, reporting, recovery and verification requirements regardless of its semantic-version category.
- Documentation and automation must determine migration need from the release's declaration, not infer it from whether a release is a patch, minor or major.

### Requirement 20 - Declaring what an existing installation needs

> As a developer, I want a defined place to declare what my change requires of an existing installation, so that I can make the change the feature needs without breaking anyone.

- A developer must have a defined way, within the development process, to declare and author what an existing installation requires in order to run their change.
- The migration capability must be supplied as part of ThunderID. Applying a migration must not require the administrator to install or operate a separate migration tool or service.
- A change that alters the structure of what is stored, or the content already stored, must not reach a release without that declaration.
- The declaration must cover PostgreSQL and SQLite.
- A database or store type must not be described as supported by the migration facility until that facility can check, apply and verify changes for it.
- The declaration must state how state already held by an existing installation is affected, including anything that will no longer be carried forward.
- For each migration operation, the declaration must include its pre-verification, execution and post-verification behaviour.
- The declaration must state what an interruption can leave behind, the scope at which progress is retained, and how the operation recovers.
- Where an operation changes deployment-scoped data, the declaration must state how all affected deployments are identified, processed and verified without relying only on the deployment that initiated migration.
- Review of the change must cover the declaration.

### Requirement 21 - Documented upgrade procedure

> As an administrator, I want the upgrade procedure documented for the way I run ThunderID, so that I am not assembling it from scattered notes.

- The upgrade procedure must be documented for each supported way of running ThunderID.
- Each procedure must cover the steps in order: taking instances out of service, checking whether the upgrade can proceed, applying migration, starting the new version, and verifying the result.
- Each procedure must state how migration is invoked unattended in that deployment path.
- Each procedure must state where the run report can be found in that deployment path.
- Each procedure must point to what the release changes and to the recovery procedure, rather than restating them.
- A procedure must be updated no later than the release that changes it.

### Requirement 22 - Reaching the current release from an older one

> As an administrator several versions behind, I want a defined way to reach the current release, so that having fallen behind is recoverable.

- The ordered sequence of patch, minor and major releases between any two versions must be published in a form a program can consume, so that an administrator's own automation can walk it.
- The documentation must state that upgrading proceeds one release at a time, and must show the sequence for a given starting version.
- Every release hop must be evaluated from its migration declaration. A release that declares no migration requires no database transition, but remains part of the ordered release path.
- Where the product manages the installation itself, it must be able to walk an explicitly requested upgrade sequence unattended, without offline or backup acknowledgement prompts at each hop.
- Where a hop fails, the walk must stop there and report which hops completed and which remain.
- After a stopped walk, every store must retain its last independently verified level and durable
  progress. The installation may contain stores finalized by the failed hop and stores still at its
  source; it must not serve until one artifact's requirements are satisfied across all stores.

### Requirement 23 - Verified migration operations

> As a developer, I want every migration operation to define how its starting state and completed state are verified, so that checks, execution and recovery use the same reviewed contract.

- Every migration operation must define plan-wide readiness, pre-verification, execution and post-verification as one reviewed unit.
- Pre-verification must be read-only and must establish whether each required scope is at the source state the operation expects, already at the target state, or invalid. For example, an operation adding a column verifies its complete definition, not only whether its name is absent or present.
- The whole-upgrade check must run every pending operation's plan-wide readiness check, aggregate the blockers it finds and disclose exact checks deferred until valid preceding operations complete.
- Migration must run an operation's exact pre-verification immediately before execution and after establishing the store reservations, so that an earlier readiness check is not treated as proof that the state is unchanged.
- Post-verification must establish the exact invariant promised by the operation. Existence alone is insufficient where type, nullability, default, indexes, constraints, content or another property is part of the promised result.
- A logical database must not be marked complete for a level until every operation's post-verification has passed across the whole affected store, including every affected deployment where content is deployment-scoped.
- A failed pre-verification, execution or post-verification must stop the level, prevent its record from advancing, and identify the exact operation and failed condition in the report.
- The developer responsible for a migration must author all four parts for PostgreSQL and SQLite, and review must cover them together.

### Requirement 24 - Deployment-scoped data in shared stores

> As an administrator running several ThunderID deployments against a shared store, I want migration to cover the whole store without confusing deployment identity with schema compatibility, so that no deployment is silently left with incompatible data.

- Each deployment must have a stable identity shared by all of its replicas and preserved across restarts and version upgrades.
- Changing a deployment's configured identity must be documented as selecting a different data namespace, not as renaming or migrating the existing deployment.
- A deployment identifier may scope rows, keys or other content, but it must not create a separate schema level for each deployment within one logical database.
- Schema migration and the authoritative completed level of a logical database must be applied once for the affected physical store, not once per deployment.
- Migration must not assume that the deployment which initiated it is the only deployment represented in an affected store.
- An operation that changes deployment-scoped content must include every deployment whose affected data is present, process each required scope, and prove through store-wide post-verification that no incompatible content remains.
- Progress and reports for such an operation must identify which deployment scopes completed and which failed, while the logical database's completed level remains a single store-scoped value.
- Migration must process deployment-scoped content without changing any deployment's configured identity.

### Requirement 25 - Establishing the initial migration level

> As an administrator, I want one initialization procedure to register fresh target levels or the fixed legacy baseline, preserve existing records and explain any required upgrade path.

- Each release must declare the level it requires for every logical database. Database records hold initialized trusted levels or subsequently verified migration levels.
- Serving startup must be read-only. Missing records terminate startup and direct the administrator to migration initialize.
- Fresh creation scripts must include empty migration metadata tables. Initialize registers the manifest target level for missing store rows in those tables, relying on the administrator's correct use of those scripts.
- Every supported fresh-install path must register target levels before bootstrap or serving, using the same initialize command for SQLite and PostgreSQL.
- When both metadata tables are absent, initialize must atomically create them and register fixed clean baseline 1.0.0 per physical namespace. This assumes untracked pre-framework databases are valid baseline inputs; no schema/data baseline validator is required. Partial tables or invalid records must not authorize stamping.
- Initialization relies on correct administrator inputs rather than baseline schema/data validation. Administrators must not manually create or empty metadata to bypass migration; untracked databases are assumed to be valid 1.0.0 inputs.
- Missing records block serving until explicit initialization. Partial metadata tables, unsupported metadata structure or inconsistent existing records require diagnosis rather than blind stamping.
- Establishing database compatibility and creating built-in resources are separate actions and must not be treated as evidence of each other. A fresh-install procedure must complete both, in that order, before serving begins.

## Change log

| Version | Change |
|---|---|
| 0.1 | Initial: R1 to R7 |
| 0.2 | R8 to R12 added. R4 permits invoking the product in a non-serving mode. R6 gains the refusal-detail criterion. |
| 0.3 | R13 to R21 added. R1 widened to cover which databases participate. R10 restated in terms of levels rather than steps. Context written. |
| 0.4 | R8 tightened: a release accepts only the immediately preceding minor as its source, so upgrades step one release at a time. R18 narrowed to that single path. R22 added, so the chain from an older release is walkable. |
| 0.5 | R22 scoped: publishing the ordered sequence is the universal obligation; walking it applies only where the product manages the installation. |
| 0.6 | Allow migrations in patch, minor and major releases; require adjacent-release hops; support independent logical stores sharing one physical database; require completion across every affected deployment; and require reviewed pre-verification, execution and post-verification for every supported store. |
| 0.7 | Define deployment identity and store-wide migration semantics for shared stores; define recoverable interruption outcomes; separate required release levels from achieved database records; and add verified fresh initialization and v1.0.0 baseline adoption for databases without migration metadata. |
| 0.8 | Replace verified baseline adoption with one trust-based initialize policy and clarify independently finalized store levels after a stopped multi-hop walk. |
| 0.8 | Make database compatibility a mandatory serving-startup gate and require the process to terminate instead of remaining available in a partial-serving or degraded mode. |
| 0.9 | Treat stopping affected instances and maintaining a recoverable backup as documented administrator responsibilities with command-output reminders, not mandatory apply or resume acknowledgements. Preserve explicit authorization for exceptional recovery actions. |
| 0.10 | Show configuration and declarative-resource release notices with documentation links in status and check. Reuse existing validators where available and identify manual review and coverage limits instead of requiring custom per-change detectors. |

### Working revision: initialization policy

The trust-based initialization decision supersedes previous verified-baseline adoption requirements.
Existing valid records are never overwritten. Installation route is selected before table creation;
per-namespace table creation and baseline records commit together. Failed namespaces can be retried
without rewriting successful namespaces. Required migrations still use exact operation verification
and adjacent-release artifacts. These local revisions have not been published to GitHub.
