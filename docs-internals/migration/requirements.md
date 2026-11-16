# Requirements: upgrading between ThunderID versions

> Status: these requirements were written before implementation. The implemented system covers PostgreSQL only; SQLite and Redis are not supported by the migration engine. The design and its rationale are in `zero-downtime-migration-final-decision.md`, and behaviour is defined by the code.


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
