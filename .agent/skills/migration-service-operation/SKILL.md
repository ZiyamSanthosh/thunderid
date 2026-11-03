---
name: migration-service-operation
description: Implement or review a ThunderID migration operation that requires business-service dependency injection. Use when a release migration needs application, entity, organization, or another ThunderID service rather than SQL access alone.
---

# Migration Service Operation

Read `migration-service-injection-guide.md` completely before changing code.

## Required design

- Keep the migration engine independent of ThunderID business packages.
- Implement the operation through the existing atomic or resumable contract. Do not introduce a separate programmatic-operation executor.
- Define the smallest service interface needed by the operation in the release package, preferably next to the operation.
- Inject that interface through the operation constructor.
- Add narrowly typed fields to the existing release dependency seam only when a concrete operation consumes them. Do not add universal registries or speculative service providers.
- Let the release construct operations, `current` select and forward to the release, and `cmd/server` initialize concrete services.
- Prefer lazy service providers so migrations that do not need a service avoid initializing it.

## Implementation checks

1. Identify whether the operation is atomic or resumable from its transaction and checkpoint needs, not from whether it uses SQL or a service.
2. Verify the injected service uses the migration context and resolved deployment identity where applicable.
3. Verify database writes and migration progress have the required transaction boundary.
4. Review idempotency, retries, caches, external side effects, and compatibility with concurrently serving binaries.
5. Keep phase assignment in the release plan.
6. Add focused operation tests and release-construction tests.
7. Add PostgreSQL integration coverage when real service initialization, transaction participation, or persistence behavior must be proven.

Do not copy the EntityService proof mechanically. Inspect the real service's initialization graph and transaction behavior, then wire only what the concrete operation requires.
