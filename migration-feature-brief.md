## What problem are we solving?

ThunderID has no upgrade process. An administrator running an instance against their own database can point a newer version at it, and that is the whole of the procedure. If the newer version expects the database organised differently, or expects what is already stored in a different form, nothing detects the mismatch, nothing corrects it, and there is no documented path to follow. Whether the upgrade succeeds depends on whether that particular release happened to change anything, and there is no way to know beforehand. Nor is a clean failure guaranteed: with nothing checking compatibility, an upgrade can equally fail somewhere further in, after it appears to have worked.

The release of v1.0.0 changed what that costs. Before it, reshaping stored data freely was legitimate, because users were expected to start over between versions. Having released v1.0.0, ThunderID is asking people to put the identity data their organisation depends on into it. That is a stability promise, and ThunderID currently has no means of keeping it. The gap is not that upgrades fail today, it is that nothing makes them succeed and nobody can say in advance which way a given upgrade will go.

The other half of the problem is already happening. Changes to the structure of what ThunderID stores, or to the content already stored in it, are made during development with no obligation to bring existing installations along, because no process asks for one. Every such change is a break waiting for the next release to deliver it. The product is accumulating an upgrade debt it has no way to pay, and the window closes at the first release after v1.0.0: either that release breaks the people who adopted v1.0.0, or the changes get dropped and the product carries compromises it has outgrown.

## Who are we solving this for?

**Primary: the ThunderID administrator.**

Owns both the identity configuration in ThunderID (users, groups, applications, flows, roles, connections) and the deployment it runs on, including database access.

- **Goal.** Stay on a current, supported ThunderID version so they get fixes and new capability, without putting the identity data their organisation depends on at risk.
- **Pain.** Moving to a newer version is an act of faith. They cannot tell in advance whether their existing configuration and identity data will survive it, they have no defined procedure to follow, and no way back if it goes wrong. The safe choice becomes not upgrading, which leaves them on an ageing version and eventually strands them.

**Secondary: the ThunderID developer.**

A contributor introducing or improving a feature, or fixing a defect, whose change alters the structure of what ThunderID stores, or the content already stored in it. Not necessarily an employee, since ThunderID is open source.

- **Goal.** Make the change the feature actually needs, including changes to the structure of what is stored and to content already stored, and have it reach users safely.
- **Pain.** There is no process to follow. The choice is to break every existing installation, or to avoid the change and carry the compromise into the product permanently. Neither is acceptable, so the decision gets deferred and the product accumulates shapes it has outgrown.

## Why should we solve this now?

ThunderID v1.0.0 is released, and a GA release is an invitation to rely on the product. Administrators are being asked to put real identity data into ThunderID on the understanding that it will keep working. At the same time development does not pause: the product will keep changing the structure of what it stores, and sometimes the content already stored, as it grows. Those two facts together mean the first release that needs an existing installation brought forward is coming, and today it would arrive with no process to follow and no way to bring anyone across.

The cost of waiting is asymmetric. A migration process introduced later cannot cover the releases that shipped before it, because there is no record of what those releases did to anyone's data and no path back through them. Every release shipped without a process permanently widens a gap that nothing can cross afterwards. Delay does not postpone this work, it removes the option of doing it properly. The cheapest moment to introduce it is before the first release that needs it, which is now.

**Value to the administrator.** They can adopt v1.0.0 and keep moving with the product. Upgrading becomes a routine operation they can plan, verify and recover from, rather than a project with an uncertain outcome that is easier to avoid.

**Value to ThunderID.** The product regains the freedom to evolve what it stores and how it is organised. Without a migration process the only choices are to break existing installations or to freeze the design, and both get paid for permanently. It also gives the product a credible answer to how upgrades work, which is something any serious adopter asks before committing.

## Usecase

**An administrator upgrading to the next release.**

An administrator runs ThunderID in production against their own database. Their users, applications, flows and roles are configured in it and their organisation logs in through it. A new release comes out with a capability or fix they want.

They point the new version at their existing database, because that is the only thing available to do. Nothing tells them whether this release expects the database organised differently than v1.0.0 left it, and if it does, nothing brings it forward. They cannot check in advance, so they cannot decide whether to schedule a maintenance window or whether a backup is needed. If it fails they have no defined way back. If it appears to succeed they have no way to confirm it actually did. The rational response is to not upgrade, and stay on v1.0.0 indefinitely.

**A developer taking a feature through the development process.**

A developer is building a capability that needs the database altered: somewhere new to keep what it records, a new attribute on something that already exists, a faster way to look it up.

They take the feature through the process the product defines, from the brief through design, review, implementation and verification. At no point does that process ask what an existing installation needs in order to run the release. No artifact records it, no review step covers it, and verification runs against databases created from scratch, so nothing fails. The feature ships correct on a fresh installation and incomplete on every installation that already exists.

A developer who does notice has nothing to do about it. There is no step in which to declare what is needed and no mechanism to carry it, so the choice is to ship the break or to not build it that way. The gap stays invisible until it reaches an administrator, and by then the release is out.

## Scope

**Principle.** ThunderID migrates what it owns and reports on what the administrator owns. The databases belong to the product, so the product transforms them. Configuration and declarative resource files belong to the administrator, are held in their repository and may be generated or templated, so the product inspects them and reports rather than rewriting them.

**In scope**

- Bringing an existing installation forward so that it can run a newer version. The initial delivery covers relational stores backed by PostgreSQL and SQLite, including both structure and existing content.
- Structure is always brought forward in every in-scope relational database, including a relational database holding short-lived data. How short-lived content is treated may differ by case.
- Patch, minor and major releases. Any release may require existing stored data to be brought forward, and every release states whether it does.
- PostgreSQL and SQLite deployments, including installations where several deployments share the same stored data.
- The means to perform a migration, not only its definition. An administrator can upgrade their own installation using what the product gives them, without writing their own scripts against the database.
- A defined path for a developer to declare and author what an existing installation needs in order to run a release, as part of the development process.
- A report before the upgrade, as a decision aid: whether it can proceed, what it will change, what it found that blocks it, and whether a maintenance window or a backup is needed. Produced without changing anything.
- A report after the upgrade, as a record: what changed, whether it completed, and where the installation now stands.
- Both reports cover what the administrator owns as well as what the product owns: release notices explain configuration and declarative-resource changes and link to required manual actions. The pre-upgrade check reuses existing validators for supplied files where available and identifies what still requires manual review, without requiring custom per-change detectors.
- Telling an administrator what configuration a new version requires them to change, so a stale configuration is reported rather than silently accepted.
- Recovering an installation after an upgrade fails.
- Documentation of the upgrade procedure for the ways ThunderID is run.

**Out of scope**

- **Redis migration.** Migration compatibility for Redis-backed `runtime_transient` is deferred to a separate delivery phase. Existing Redis runtime and cache support is unchanged, but it is not covered by the initial relational migration capability.
- **API and SDK contract changes.** Handled by interface versioning and deprecation. Nothing an administrator holds is transformed, so it is a different mechanism under the same word.
- **Applying configuration changes on the administrator's behalf.** Their configuration is theirs, and may be templated, generated, held in version control or managed by another system, so rewriting it can conflict with how they run it. This feature reports what must change; applying it stays with the administrator.
- **Versioning declarative resources so that outdated ones keep working.** That commits the product to carrying compatibility for every past resource shape, which is a deprecation-policy decision rather than a migration one, and it deserves its own brief. Validating them and reporting what is wrong is in scope and is needed either way.
- **Secrets and key material.** Adjacent and real, but a separate concern with its own risks.
- **Reverting a successful upgrade.** Transforming everything backwards roughly doubles the authoring cost of every change, for a case that restoring a backup serves better.
- **Upgrading from any version before v1.0.0.** Pre-GA installations were never promised a path.

## Acceptance criteria

- An administrator can upgrade a running installation to a newer version, and afterwards their existing configuration and identity data is intact and working.
- Before committing to an upgrade, an administrator can check what it would change and what currently blocks it, without altering their installation or being given a false guarantee that the later upgrade will succeed.
- An administrator can move through published releases one at a time, including patch, minor and major releases, and the installation works after each completed upgrade.
- When an upgrade fails, the administrator is told what happened, and has a documented way back to a working installation.
- An administrator is told what in their own configuration and declarative resource files the new version will not accept, before the upgrade rather than after it.
