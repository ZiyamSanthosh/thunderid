# Conventions

## Files and ids

| Item | Rule | Example |
|---|---|---|
| Operation file | `NN_snake_name.go` | `02_add_entity_type_handle.go` |
| Test file | same name plus `_test.go` | `02_add_entity_type_handle_test.go` |
| Rollback file | `NN_snake_name_rollback.go` | `04_backfill_entity_type_handle_rollback.go` |
| Operation id | `<release>/<store>/<NN>_<snake_name>` | `2.0.0/configdb/02_add_entity_type_handle` |
| Rollback id | forward id plus `_rollback` | |
| Catch-up id | forward id plus `_catch_up` | |
| Constructor | `newXxxOperation()` returning `(engine.Operation, error)` when it can fail | |
| Query id | `MIG-<CFG\|ENT\|RTP>_MGT-<n>` | `MIG-CFG_MGT-05` |

Query ids are unique across the release package. Pick the next unused number by listing the existing
ids. Operation ids carry the store segment that matches `StoreID`.

Every `.go` file starts with the licence header used in the package:

```go
// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0
```

## SQL

- PostgreSQL text goes in `PostgresQuery`. A query with no text for the active database type is
  refused at run time. There is no SQLite migration path.
- Define queries as package level `dbmodel.DBQuery` values with ids. Do not build SQL strings from
  input. Bind values with `$1`, `$2`.
- Quote table names in DDL (`"ENTITY_TYPES"`). Catalogue queries compare names through `UPPER` or
  `LOWER`.
- Scope catalogue queries to `CURRENT_SCHEMA()` so shared database layouts and test schemas behave.
- Data queries filter `($1 = '' OR DEPLOYMENT_ID = $1)`.
- Table locking DDL starts with `SET LOCAL lock_timeout = '2s'` inside a transaction.
- Build indexes with `CONCURRENTLY` in online operations. Drop with `CONCURRENTLY` too.
- Add constraints with `NOT VALID` and validate in a separate step when the table is large.
- Use `IF NOT EXISTS` / inspection to make repeats safe.
- Keep generated names stable and lower case with underscores (`idx_<table>_<purpose>`).

## Code style

- Wrap errors with context: `fmt.Errorf("inspect widget columns: %w", err)`.
- Findings use stable snake_case codes (`unexpected_widget_schema`) and human readable messages.
  Remediation text says what the operator does next.
- Keep logic in small pure functions: classify, convert, next value, batch result. Unit tests cover them
  without a database.
- Do not add logging inside operations. The engine and the CLI report results.
- Do not add comments that restate the code. Comment why a state is treated as source, target or
  invalid, and any irreversible effect.
- No em dashes or double hyphens in prose, comments or messages.
- Always write the full product name ThunderID in comments, messages and descriptions.

## Descriptions

`Description` is required, short, written as an action: "Add a nullable entity-type handle column".
It appears in operator output, so avoid internal jargon.

## What not to do

- Do not edit a shipped release.
- Do not catch and ignore errors to keep a run going.
- Do not read time, randomness or environment inside verifiers. They must be deterministic.
- Do not share mutable package state between operations.
- Do not write to `MIGRATION_*` tables. The engine owns them.
