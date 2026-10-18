# Multi-value entity identifier migration follow-up

## Current implementation boundary

The current migration increment implements only the PostgreSQL expand operation:

`2.0.0/entitydb/07_expand_entity_identifier_primary_key`

It changes the `ENTITY_IDENTIFIER` primary key from
`(ENTITY_ID, DEPLOYMENT_ID, NAME)` to
`(ENTITY_ID, DEPLOYMENT_ID, NAME, VALUE)` without changing existing rows.

The operation is part of the expand phase even though its append-only identifier uses the next
available operation number. Migration phase ordering is defined by the release plan, not by the
numeric portion of an operation identifier.

## Pending query behavior

The entity lookup fallback compares a requested scalar value with the complete JSON attribute value.
It can match an existing scalar attribute but cannot match one item inside an array. For example, a
lookup for `personal@example.com` does not match an `email` attribute containing
`["personal@example.com", "work@example.com"]` when the corresponding `ENTITY_IDENTIFIER` rows are
absent.

This limitation existed before multi-value identifier support. It is nevertheless incomplete fallback
behavior once multi-valued indexed attributes are supported and must be addressed separately.

The follow-up must define and test consistent PostgreSQL and SQLite behavior for:

- scalar attribute lookup;
- membership lookup within arrays of scalar values;
- schema and system attribute precedence;
- mixed indexed and non-indexed filters;
- multiple matching entities and ambiguity reporting; and
- malformed or unsupported complex attribute values.

The fallback is a correctness path, not a replacement for the identifier projection. Normal indexed
lookups should continue using `ENTITY_IDENTIFIER`.

## Identifier resynchronization decision

A bulk identifier resynchronization operation is not required for the current migration. Existing
entities without projected rows continue through the entity JSON lookup fallback, and later entity
mutations can populate the projection through the normal product path. The array-membership defect
in that fallback remains a product query issue and must be fixed separately.

Do not treat identifier resynchronization as pending implementation work. Reconsider it only if a
future requirement demands a complete eagerly populated projection, or if production evidence shows
that relying on the corrected fallback is not operationally acceptable.

### Superseded design retained for reference

Existing entities may contain multi-valued indexed attributes that have no corresponding identifier
rows because the previous synchronization logic skipped arrays. If the decision is revisited, an
operation could rebuild the identifier projection for those entities.

The following design was considered before the decision above. It is retained to preserve the
analysis, but it is not part of the current migration plan.

The planned operation is a deployment-scoped, resumable data operation that runs after source
instances are drained. It should be the first contract operation for this change.

For each entity it must:

1. Read the authoritative `ATTRIBUTES` and `SYSTEM_ATTRIBUTES` values.
2. Resolve the indexed attributes configured for the deployment.
3. Produce one identifier row for a scalar value and one row per supported scalar array item.
4. Apply system-over-schema precedence for an attribute name.
5. Deduplicate exact `(NAME, VALUE)` pairs.
6. Enforce the indexed-value limit.
7. Replace the entity's identifier projection atomically.
8. Persist a checkpoint after each bounded batch so an interrupted run can resume safely.

The implementation should use the existing resumable-operation contract and a narrow release-local
service interface injected through the release dependency seam. The generic migration engine must not
depend on entity business packages.

### Superseded mixed-version constraints

Before the source is drained:

- the expanded primary key may be shared by source and target versions;
- historical identifier rows remain unchanged;
- authentication and token issuance remain available; and
- capabilities that create, update, or delete the affected identifier projection must be fenced when
  required by this migration.

The exact writer inventory must include entity creation, entity deletion, attribute and system
attribute updates, complete entity updates, progressive attribute collection, and credential flows
that update system attributes. Normal authentication lookup does not rewrite `ENTITY_IDENTIFIER`.

After source instances are drained, contract authorization ends source rollback eligibility. The
resynchronization operation is resumed after interruption rather than reversed to the old
array-skipping projection.

### Superseded validation plan

- Source binaries continue serving supported authentication against the expanded primary key.
- Target binaries preserve existing scalar identifier behavior before resynchronization.
- The JSON fallback handles scalar and array membership consistently after its correction.
- Relevant mutation capabilities are fenced during the mixed-version window.
- Authentication remains available while identifier batches are rebuilt.
- Each entity projection replacement is atomic and idempotent.
- Unique multi-valued identifiers resolve after resynchronization.
- Shared values produce consistent ambiguity results.
- Interrupted resynchronization resumes without missing or duplicating rows.
