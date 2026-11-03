---
name: migration-testing
description: Test ThunderID migration operations and plans. Use when writing or reviewing tests for a migration operation, a release plan, a rollback operation, a catch-up, or the migration engine. Covers unit tests with fake read access, release plan tests, the Docker based PostgreSQL integration suite, failure injection (crash mid batch, resume, rollback, revert-all, concurrent runner, deployment selection) and the commands to run them.
---

# Testing Migrations

A migration that is only unit tested has not been tested. The SQL, the locks, the batching and the
recovery behaviour only exist against a real PostgreSQL. Plan for both layers.

| Layer | Proves | Where |
|---|---|---|
| Unit | Definition, verifiers, readiness, SQL text, pure transforms, plan shape | `backend/internal/system/migration/**/_test.go` |
| Integration | The operation really changes a 1.0.x style database, survives interruption, rolls back, and works for separate and shared database layouts | `backend/tests/migration/releases/<version>` |

Guides:

- [unit-tests.md](unit-tests.md): fakes, table tests, what to assert, what cannot be unit tested.
- [integration-tests.md](integration-tests.md): the Docker suite, fixtures, registering a check.
- [failure-scenarios.md](failure-scenarios.md): the interruption matrix to cover per operation type.

For engine changes (coordinator, state controller and so on) also read
[migration-engine](../migration-engine/SKILL.md).

## Minimum bar per operation

Every new operation needs all of these. Reviewers will look for them.

1. **Definition test.** Id, store, read stores, kind, mode, and that the interface it claims to
   implement is implemented.
2. **Verifier tests.** `source`, `target` and at least one `invalid` case, for both `PreVerify` and
   `PostVerify`. For data changes include the empty scope and deployment filtered cases.
3. **Readiness tests.** Ready, deferred (with the ids it waits for) and blocked (with a finding and
   remediation).
4. **Statement or transform tests.** Atomic: statement count, order and key fragments (for example the
   lock timeout comes first). Resumable: pure helpers and the batch result (`Completed`, checkpoint).
5. **Plan test.** The release test pins plan sizes per phase and the position of the new ids.
6. **Integration check.** Fixture in the source schema, the apply that runs it, assertions on the
   resulting data or schema, and the operation recorded as completed.
7. **Interruption test** for online and resumable operations: crash or fail partway, run again, same
   final state.
8. **Rollback test** when a rollback operation exists: forward, rollback, forward again.

## Commands

```bash
cd backend
go test ./internal/system/migration/... ./cmd/server/                   # unit tests
go vet ./internal/system/migration/... ./cmd/server/
go test -tags migration_integration -run '^$' ./tests/migration/releases/<version>   # compile only, no Docker
go test -tags migration_integration -run 'TestPostgresMigration$' ./tests/migration/releases/<version>              # separate databases
go test -tags migration_integration -run 'TestPostgresSharedDatabaseMigration$' ./tests/migration/releases/<version> # shared database
```

The last two need Docker (or a compatible runtime) and network access to pull the source image. Run
both layouts before opening a pull request when an operation touches data or keys.

Before merging also run the repository gate: `make pr_checks`.

## Rules

- Do not weaken or delete a failing test. Fix the cause.
- Do not replace a failing real test with mocked or hard coded data that only passes.
- Tests assert behaviour the operator would notice, not implementation details of private helpers,
  except for the pure helpers that carry the logic.
- Seed integration data in the **source schema**. A fixture that already looks migrated proves nothing.
- Keep fixtures deterministic: fixed ids, fixed deployment ids, no clock dependence.
