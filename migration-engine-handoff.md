# Migration Engine Handoff

## Repository state

- Repository: ThunderID
- Branch: `migration-plan`
- Latest migration commit: `c5e0a474b Add explicit migration rollback lifecycle`
- The rollback implementation is committed locally. Nothing has been pushed.
- This handoff file was intentionally created after that commit.

The following pre-existing files are unrelated to the rollback commit and must be preserved unless the user explicitly asks to change them:

- `backend/cmd/server/deployment.yaml`
- `migration-engine-implementation-backlog.md`
- `zero-downtime-migration-final-decision.md`
- `claude-migration-diagrams.excalidraw`
- `claude-migration-system-review-findings.md`
- `migration-engine-review-findings.md`

## Current migration model

ThunderID ships the migration engine inside the target binary. Migration commands currently run as standalone administrative commands rather than as a background task in a serving target process.

The lifecycle is divided into:

1. `expand`: add backward-compatible schema and structures.
2. `migrate`: backfill or transform data, including resumable batched operations.
3. `contract`: remove obsolete representations only after source instances are drained.

Migration can target the complete store or selected deployment IDs. Schema operations execute once for the physical store. Deployment-aware data operations resolve selected deployments or the complete store.

Operation execution and implementation style are independent dimensions:

- Execution can be atomic or resumable.
- Implementation can use SQL or programmatic Go code.
- Programmatic operations may receive narrowly wired ThunderID business-service dependencies through the release construction seam.

## Major capabilities already implemented

- Release manifests and ordered expand, migrate, and contract plans
- PostgreSQL migration metadata and initialization
- Compatibility gate before serving
- Human-readable status and check output
- Store assessment and optional operation readiness checks
- Atomic and resumable operation execution
- Durable checkpoints and resume-forward recovery
- PostgreSQL execution ownership lock
- Deployment-selective migrate execution
- Operation-specific target resolution
- Restricted operating mode and generic feature gates
- Management API restriction and flow write fencing
- Manual Docker-based migration POC setup and teardown
- Realistic v2.0.0 schema and data migration operations
- Explicit administrator-controlled rollback lifecycle

## Rollback design

Rollback is compensation, not database transaction reversal. It is never triggered automatically.

Rollback operations are optional and declared separately in `MigrationPlan.Rollback`. Each rollback operation identifies its forward operation through `ForwardOperationID()`. The plan registry validates that the mapping is unique and that the forward and rollback operations use the same store and operation kind.

Supported commands:

```text
thunderid migration rollback
thunderid migration rollback --phase migrate
thunderid migration rollback --revert-all
```

Behavior:

- Plain rollback compensates only the failed forward operation and exact execution target. Earlier successful operations remain unchanged.
- After plain rollback, the migration state remains failed. The administrator repairs the failure cause and runs `migration resume` to continue forward.
- Atomic operations whose failed transaction committed no progress require no compensation.
- Phase rollback compensates mapped operations from the failed phase in reverse order and restores the preceding phase boundary.
- Complete rollback compensates all mapped executed operations across phases in reverse order and restores the accepted source boundary.
- A completed contract operation without compensation blocks complete rollback.
- Deployment-specific execution targets are preserved during compensation.
- Resumable rollback operations persist and resume their own checkpoints.

Durable failure identity is stored in `MIGRATION_FAILURE`, including:

- run ID
- migration state ID
- failed forward operation ID
- currently executing operation ID
- execution target ID
- forward or rollback direction
- compensation status
- failure summary

The realistic rollback example is:

- Forward: `05_backfill_entity_type_handle`
- Compensation: `08_rollback_entity_type_handle_backfill`

The compensation clears handles populated by the backfill in bounded batches. It supports both complete-store and deployment-specific targets.

## Important rollback files

- `backend/internal/system/migration/engine/rollback.go`
- `backend/internal/system/migration/engine/rollback_test.go`
- `backend/internal/system/migration/engine/state_controller.go`
- `backend/internal/system/migration/engine/metadata.go`
- `backend/internal/system/migration/engine/metadata_store.go`
- `backend/internal/system/migration/engine/postgres_metadata.sql`
- `backend/cmd/server/migration_cmd.go`
- `backend/internal/system/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill.go`
- `backend/tests/migration/releases/v2_0_0/08_rollback_entity_type_handle_backfill_test.go`

## Validation completed

The following checks passed before the final small rollback cleanup:

```bash
GOCACHE=/tmp/thunderid-go-cache go test ./internal/system/migration/... ./cmd/server
GOCACHE=/tmp/thunderid-go-cache go test -tags migration_integration -run '^$' ./tests/migration/releases/v2_0_0
```

After the final cleanup, the focused engine suite passed:

```bash
GOCACHE=/tmp/thunderid-go-cache go test -vet=off ./internal/system/migration/engine
```

`git diff --check` also passed.

## Environment blocker

The real PostgreSQL rollback integration test could not complete because the host disk became full. Rancher Desktop BuildKit changed to a read-only filesystem while building the migration runner image. The failure occurred before test assertions ran.

Observed conditions:

- Host filesystem was at 100 percent capacity.
- Rancher Desktop reported writes under `/var/lib/docker/buildkit` as read-only.
- Go also reported no space left on device.

The disposable `/tmp/thunderid-go-cache` created during testing was removed. The user must free additional host and Rancher Desktop disk space, then restart Rancher Desktop before retrying Docker integration tests.

## Required next work

1. Free disk space and restart Rancher Desktop.
2. Recreate the migration POC databases because metadata now includes the `MIGRATION_FAILURE` table.
3. Run the existing real rollback integration test:

   ```bash
   cd backend
   GOCACHE=/tmp/thunderid-go-cache go test -tags migration_integration \
     -run '^TestPostgresMigrationRollback$' -v ./tests/migration/releases/v2_0_0
   ```

4. Add real PostgreSQL end-to-end cases for:
   - `migration rollback --phase migrate`
   - `migration rollback --revert-all`
   - reverse operation order
   - deployment-specific phase rollback
   - complete rollback blocked by an uncompensated completed contract operation
   - interrupted resumable compensation followed by another rollback invocation
5. Run the targeted migration suites again.
6. Fix only defects revealed by those tests. The rollback architecture has already been decided.

## Starting prompt for the next conversation

Use this prompt:

```text
Read migration-engine-handoff.md and continue the remaining rollback PostgreSQL integration tests. Preserve the unrelated dirty files listed in the handoff. Do not redesign the rollback lifecycle unless a test exposes a concrete defect.
```
