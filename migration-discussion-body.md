# Design Discussion: Upgrading between ThunderID versions

Created. This file holds the body as posted.

- Stage: Specification. Discovery approved.
- Feature issue: [#5208](https://github.com/thunder-id/thunderid/issues/5208)
- Discussion: [#5271](https://github.com/thunder-id/thunderid/discussions/5271), id `D_kwDOOi7AYc4Ao-Zv` (needed for posting comments)
- Title: `[Design Discussion] Upgrading between ThunderID versions`
- Category: Design
- Linked back from #5208 by comment. Chain complete.

The body is a landing page, not a segment. It says which issue this belongs to and what the
feature is, in enough detail that a reader knows whether to keep reading. Requirements and
design arrive as separate comments.

Relocated rather than dropped:

- The six decisions taken during Discovery go into the requirements comment's Context, and
  later into the design's decisions section.
- The items carried into Specification are answered by the requirements themselves rather
  than listed as pending.

<!-- POSTABLE BODY BELOW -->

### Related Feature Issue

#5208

### Problem Summary

ThunderID has no upgrade process. An administrator can point a newer version at their existing database, and that is the whole of the procedure: nothing detects that the new version expects the database organised differently or expects what is already stored in a different form, nothing corrects it, and there is no documented path to follow.

With v1.0.0 released, administrators are being asked to rely on the product with real identity data in it, while changes to what ThunderID stores continue to be made with no obligation to bring existing installations along. This feature introduces the process and the means to move an installation from one version to the next.

### Scope

Bringing an existing installation from one published version to the next, including patch, minor and major releases; covering relational stores backed by PostgreSQL and SQLite, including installations where several deployments share stored data; providing the means for an administrator to perform the upgrade; and defining how a developer declares what an existing installation needs in order to run a release. Redis migration is deferred to a separate delivery phase.

The boundary that shapes the rest: ThunderID migrates what it owns, its databases, and reports on what the administrator owns, their configuration and declarative resource files. Full in and out of scope lists, with the reasoning for each boundary, are in #5208.

---

Requirements and design follow as comments on this discussion.
