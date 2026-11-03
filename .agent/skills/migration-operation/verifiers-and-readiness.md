# Verifiers and readiness

Verifiers and readiness are how an operation is made safe, resumable and re-runnable. Write them
before the execution code. Reviewers read them first.

## Three states, one classifier

Write one function that inspects the store and returns a classification. Use it for `PreVerify`,
`PostVerify` and `CheckReadiness`, so the three can never disagree.

```go
func classifyWidget(ctx context.Context, access engine.ReadAccess) (engine.VerificationState, string, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryWidgetColumns)
	if err != nil {
		return "", "", fmt.Errorf("inspect widget columns: %w", err)
	}
	// build a map from the rows ...
	switch {
	case /* source shape */:
		return engine.VerificationSource, "WIDGET.COLOR is absent", nil
	case /* target shape */:
		return engine.VerificationTarget, "WIDGET.COLOR is present and nullable", nil
	default:
		return engine.VerificationInvalid, "WIDGET.COLOR has an unexpected definition", nil
	}
}
```

| Return | When |
|---|---|
| `source` | Exactly the accepted starting shape |
| `target` | Exactly the result shape. Also the correct answer after a restart that finished the work |
| `invalid` | Anything else: a third shape, a half done unrecognised state, data that conflicts |

An error return is for failures to **inspect** (query failed). It is not for "wrong shape". Wrong shape
is `invalid` with findings.

## Verification results

```go
engine.VerificationResult{State: state, Summary: summary}
```

- `Summary` is required.
- `invalid` needs at least one `engine.Finding{Code: "...", Message: "..."}`. The code is a stable
  snake_case identifier. The message names the table, column or row.

```go
if state == engine.VerificationInvalid {
	result.Findings = []engine.Finding{{Code: "unexpected_widget_schema", Message: summary}}
}
```

## Readiness

```go
func checkWidgetReadiness(ctx context.Context, access engine.ReadAccess, plan engine.PlanContext) (engine.ReadinessResult, error)
```

| State | Use | Required fields |
|---|---|---|
| `ready` | Source or target shape, nothing blocks | `Summary` |
| `deferred` | Depends on an earlier operation that has not run yet | `Summary`, `DeferredUntil` listing operation ids that **precede** this one in the plan |
| `blocked` | A known condition prevents a safe run | `Summary`, at least one `Finding` with `Code`, `Message`, and `Remediation` |

Examples:

```go
// the column this operation changes does not exist yet because operation 02 adds it
return engine.ReadinessResult{
	State:         engine.ReadinessDeferred,
	Summary:       "Handle uniqueness depends on the preceding handle backfill",
	DeferredUntil: []engine.OperationID{backfillOperationID},
}, nil

// data that cannot be migrated safely
return engine.ReadinessResult{
	State: engine.ReadinessBlocked, Summary: summary,
	Findings: []engine.Finding{{
		Code: "unexpected_widget_schema", Message: summary,
		Remediation: "Restore the expected WIDGET schema before retrying",
	}},
}, nil
```

Rules:

- Readiness is **read only** and must not depend on earlier operations having run. If it does, return
  `deferred`.
- A `deferred` result naming an operation that does not precede the current one is rejected.
- Do not repeat the same expensive scan in readiness and in verification if you can share the result.
- Readiness may be wider than verification: it can check preconditions of the whole plan, such as "no
  entity type name collides with a declarative one".
- A missing `CheckReadiness` means always ready. Provide one for every operation where the data can be
  wrong.

## Reading rows

`access.Query` returns `[]engine.QueryRow`. Column names are the lower case aliases in your query.

| Accessor | Behaviour |
|---|---|
| `row.String(col)` | Required non empty string. **Errors on NULL and on empty string** |
| `row.Int64(col)` | Required integer |
| `row.Bool(col)` | Required boolean |
| raw `row[col]` | For nullable columns, JSON columns (`[]byte` or `string`), and anything needing a nil check |

Use the typed accessors for columns the query guarantees. For nullable columns read `row[col]` and
handle `nil`. Alias catalogue columns with `UPPER(...)` or `LOWER(...)` so comparisons do not depend on
how the database stored the name.

## Classifying data, not just schema

A data change classifies by sampling the data in scope for the target, using the same deployment
filter as the execution queries:

- every row already converted: `target`;
- no row converted: `source`;
- some converted: `source` (the operation converts the rest, and its pre-verification must still run);
- a row that is converted wrongly, ambiguous, or colliding: `invalid` with a finding naming it.

An empty scope is `target`. Nothing to convert is success.

## Partial states of online and resumable operations

| Operation | Possible leftovers | Classifier must |
|---|---|---|
| Concurrent index build | Valid index, **invalid** index from an interrupted build, no index | Report `source` for invalid or missing (Execute drops the invalid one), `target` for valid and attached |
| Constraint swap | Index exists, constraint not attached | `source`. Finalize attaches |
| Batched backfill | Some rows done | `source` while any remain, `target` when none |
| Key swap with legacy index | New key present, legacy index missing | Recognise each combination or return `invalid` |

## Idempotency checklist

- [ ] `PreVerify` returns `target` when everything is already done.
- [ ] `Execute` is safe if run twice (guard `IF NOT EXISTS` or re-classify first).
- [ ] A crash between any two steps leaves a state the classifier recognises.
- [ ] `PostVerify` uses the same classifier and must answer `target`.
- [ ] After a rollback and re-apply, the verifiers still classify correctly.
