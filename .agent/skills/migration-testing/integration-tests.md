# Integration tests (Docker, real PostgreSQL)

The suite lives in `backend/tests/migration/` and is guarded by the build tag `migration_integration`,
so normal `go test` skips it.

## How it works

1. The harness (`harness/harness.go`) starts PostgreSQL in Docker Compose. It creates the four
   databases from the **source release** schema, using the published source image
   (`sourceProductVersion` in `releases/<version>/config.yaml`).
2. It builds the **target** binary from this repository (`target.Dockerfile`) as the
   `migration-runner` service and runs `migration initialize`.
3. Test code seeds data in the source schema, then runs real CLI commands inside the runner
   (`environment.RunMigrationCommand(t, "apply expand")`) and asserts through direct SQL connections.
4. The suite runs twice: separate databases (`config.yaml`) and one shared database
   (`config-shared.yaml`, `deployment-shared.yaml`). Both must pass.

```text
backend/tests/migration/
  harness/harness.go                  Start, RunMigrationCommand, OpenDatabase, AssertQueryValue, AssertOperationCompleted
  docker-compose.yml, ...shared.yml   Environment definition
  target.Dockerfile                   Builds the binary under test
  resources/                          Declarative resource files used by the runner
  releases/<version>/
    config.yaml, config-shared.yaml   Source version and layout
    migration_test.go                 The ordered suite: one verifyX call per operation or group
    source_fixture_test.go            Seeds source schema data
    NN_name_test.go                   verifyX for one operation or a group
    resume_test.go, deployment_selective_test.go, execution_lock_test.go, compatibility_gate_test.go
```

Every Go file carries `//go:build migration_integration` and the licence header.

## Harness helpers

| Helper | Use |
|---|---|
| `environment.RunMigrationCommand(t, "apply expand")` | Run a CLI action, fail the test on a non zero exit. The argument is the text after `thunderid migration` |
| `environment.RunMigrationCommandExpectFailure(t, "status")` | Run and require failure, returning the output |
| `environment.OpenDatabase(t, "configdb")` | A `*sql.DB` to a store. In the shared layout it opens the one shared database |
| `migrationharness.AssertQueryValue(t, db, query, expected, args...)` | Run a scalar query and compare to text |
| `migrationharness.AssertOperationCompleted(t, db, operationID)` | The operation's progress row is completed |

## Adding checks for a new operation

1. **Fixture.** In `source_fixture_test.go` insert rows in the source schema that exercise the
   operation: normal rows, edge rows, several deployments, enough rows to span more than one batch for a
   resumable operation (a batch is 100, so seed 101 or more), and a conflicting row when the operation
   has a defined behaviour for conflicts.
2. **Verification function.** Create `NN_name_test.go` with the build tag and a function
   `verifyXxx(t, environment)`:

```go
func verifyAddWidgetColor(t *testing.T, environment migrationharness.Environment) {
	t.Helper()
	t.Run("60_add_widget_color", func(t *testing.T) {
		db := environment.OpenDatabase(t, "configdb")
		migrationharness.AssertQueryValue(t, db,
			`SELECT IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'WIDGET' AND COLUMN_NAME = 'color'`,
			"YES")
		migrationharness.AssertOperationCompleted(t, db, "2.1.0/configdb/60_add_widget_color")
	})
}
```

3. **Register** the call in `migration_test.go` at the point in the sequence after the phase that
   runs it. The suite applies phases at specific points (expand in the compatibility gate check,
   later checks apply migrate and contract) and checks run against the accumulated state. Place
   yours where its phase has already been applied and its data still exists.
4. **Assert the data**, not just completion: converted values, counts, constraint and index presence,
   progress checkpoints for resumable operations, and that other deployments were not touched during a
   selective run.
5. **Old binary compatibility.** For Expand steps add a check that the **old binary's SQL** still
   works after the step, for example by inserting a row the way the old release does (without the new
   column, with `ON CONFLICT` on the old key). This is the real proof an Expand step is safe.
6. **Set the source version.** For a new release, `sourceProductVersion` in the two `config*.yaml` files
   names the previous release image.

## Interruption and recovery tests

Dedicated tests exist for resume, rollback, revert-all, selective deployments and concurrent runners.
Follow their style for a new operation, see [failure-scenarios.md](failure-scenarios.md). Typical
ingredients:

- force a failure by tampering with the database (for example delete or alter progress, insert a
  conflicting row, rename an object) and assert the command fails and the state is `failed`;
- fix the cause and run `resume`, then assert the same final state as an uninterrupted run;
- run `rollback`, `rollback --phase <p>` or `rollback --revert-all`, assert the original data and
  schema, then apply again and assert the result is identical.

## Running

```bash
cd backend
go test -tags migration_integration -run 'TestPostgresMigration$' -count=1 -timeout 30m ./tests/migration/releases/<version>
go test -tags migration_integration -run 'TestPostgresSharedDatabaseMigration$' -count=1 -timeout 30m ./tests/migration/releases/<version>
```

The first run builds images and can take several minutes. A compile only check
(`-run '^$'`) needs no Docker and catches broken test code.

If Docker is unavailable, say so explicitly in the pull request. Do not claim integration coverage
that did not run.
