// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var queryOnlineIndex = dbmodel.DBQuery{
	ID: "MIG-OIX-01",
	PostgresQuery: `SELECT i.indisvalid AS is_valid, i.indisready AS is_ready, i.indisunique AS is_unique,
			(i.indpred IS NOT NULL) AS has_predicate, t.relname AS table_name,
			pg_get_indexdef(i.indexrelid) AS definition
		FROM pg_class idx
		INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
		INNER JOIN pg_index i ON i.indexrelid = idx.oid
		INNER JOIN pg_class t ON t.oid = i.indrelid
		WHERE n.nspname = CURRENT_SCHEMA() AND idx.relname = $1`,
}

// onlineIndexSpec describes a plain btree index that an operation builds without blocking writes.
type onlineIndexSpec struct {
	id          engine.OperationID
	description string
	storeID     engine.StoreID
	table       string
	name        string
	// definition is the part of the index definition after USING btree, such as "(deployment_id, project_id)".
	// A partial index adds its WHERE clause as the catalog prints it.
	definition string
	createSQL  string
	// unique and partial say the index is a unique index, or has a predicate, and so must be found as one.
	unique  bool
	partial bool
	// precondition, when set, is checked while the index is still to be built. It returns a blocked
	// readiness, or nil when the index can be built.
	precondition func(ctx context.Context, access engine.ReadAccess) (*engine.ReadinessResult, error)
}

type onlineIndexOperation struct {
	spec       onlineIndexSpec
	definition engine.OperationDefinition
	create     dbmodel.DBQuery
	drop       dbmodel.DBQuery
}

// newOnlineIndexOperation returns an online schema change that builds the index concurrently. A leftover invalid
// index from an interrupted build is dropped and built again, and a valid index with the expected definition
// makes the operation a no-op.
func newOnlineIndexOperation(spec onlineIndexSpec, queryID string) engine.Operation {
	return &onlineIndexOperation{
		spec: spec,
		definition: engine.OperationDefinition{
			ID: spec.id, Description: spec.description, StoreID: spec.storeID,
			Kind: engine.OperationKindSchemaChange, InterruptionMode: engine.InterruptionModeOnline,
		},
		create: dbmodel.DBQuery{ID: queryID + "-02", PostgresQuery: spec.createSQL},
		drop: dbmodel.DBQuery{
			ID: queryID + "-03", PostgresQuery: fmt.Sprintf(`DROP INDEX CONCURRENTLY %q`, spec.name),
		},
	}
}

func (o *onlineIndexOperation) Definition() engine.OperationDefinition { return o.definition }

func (o *onlineIndexOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, _, err := o.inspect(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationSource && o.spec.precondition != nil {
		blocked, err := o.spec.precondition(ctx, access)
		if err != nil {
			return engine.ReadinessResult{}, err
		}
		if blocked != nil {
			return *blocked, nil
		}
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary,
			Findings: []engine.Finding{{
				Code: "unexpected_index_definition", Message: verification.Summary,
				Remediation: "Remove or reconcile the conflicting index before retrying",
			}},
		}, nil
	}
	return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
}

func (*onlineIndexOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *onlineIndexOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	verification, _, err := o.inspect(ctx, access)
	return verification, err
}

func (o *onlineIndexOperation) Execute(ctx context.Context, execution engine.OperationExecution) error {
	return o.build(ctx, execution.Access())
}

// build creates the index unless it is already valid, and first drops an interrupted build.
func (o *onlineIndexOperation) build(ctx context.Context, access engine.ExecutionAccess) error {
	verification, leftover, err := o.inspect(ctx, access)
	if err != nil {
		return err
	}
	if verification.State == engine.VerificationTarget {
		return nil
	}
	if verification.State != engine.VerificationSource {
		return fmt.Errorf("prepare index %s: %s", o.spec.name, verification.Summary)
	}
	if leftover {
		if _, err := access.Execute(ctx, o.spec.storeID, o.drop); err != nil {
			return fmt.Errorf("drop interrupted index %s: %w", o.spec.name, err)
		}
	}
	if _, err := access.Execute(ctx, o.spec.storeID, o.create); err != nil {
		return fmt.Errorf("create index %s concurrently: %w", o.spec.name, err)
	}
	return nil
}

func (*onlineIndexOperation) Finalize(context.Context, engine.OperationExecution) error { return nil }

func (o *onlineIndexOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	verification, _, err := o.inspect(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if verification.State != engine.VerificationTarget {
		return invalidVerification("index_not_built", fmt.Sprintf("Index %s was not built: %s",
			o.spec.name, verification.Summary)), nil
	}
	return verification, nil
}

// inspect classifies the index. The second result reports a leftover invalid index that has to be dropped first.
func (o *onlineIndexOperation) inspect(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, bool, error) {
	relations, err := access.Query(ctx, o.spec.storeID, queryTableRelation, o.spec.table)
	if err != nil {
		return engine.VerificationResult{}, false, fmt.Errorf("inspect %s relation: %w", o.spec.table, err)
	}
	if len(relations) != 1 {
		return invalidVerification("unexpected_table_schema", fmt.Sprintf(
			"Expected one %s table, found %d", o.spec.table, len(relations))), false, nil
	}
	rows, err := access.Query(ctx, o.spec.storeID, queryOnlineIndex, o.spec.name)
	if err != nil {
		return engine.VerificationResult{}, false, fmt.Errorf("inspect index %s: %w", o.spec.name, err)
	}
	if len(rows) == 0 {
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("Index %s is ready to be created", o.spec.name)}, false, nil
	}
	valid, validErr := rows[0].Bool("is_valid")
	ready, readyErr := rows[0].Bool("is_ready")
	unique, uniqueErr := rows[0].Bool("is_unique")
	predicate, predicateErr := rows[0].Bool("has_predicate")
	table, tableErr := rows[0].String("table_name")
	definition, definitionErr := rows[0].String("definition")
	if err := errorsJoin(validErr, readyErr, uniqueErr, predicateErr, tableErr, definitionErr); err != nil {
		return engine.VerificationResult{}, false, fmt.Errorf("read index %s: %w", o.spec.name, err)
	}
	if table != o.spec.table || unique != o.spec.unique || predicate != o.spec.partial ||
		!strings.HasSuffix(normalizeIndexDefinition(definition), normalizeIndexDefinition(
			"USING btree "+o.spec.definition)) {
		return invalidVerification("unexpected_index_definition", fmt.Sprintf(
			"Index %s exists with an unexpected definition: %s", o.spec.name, definition)), false, nil
	}
	if !valid || !ready {
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("Interrupted index %s is ready to be rebuilt", o.spec.name)}, true, nil
	}
	return engine.VerificationResult{State: engine.VerificationTarget,
		Summary: fmt.Sprintf("Index %s is present and valid", o.spec.name)}, false, nil
}

func normalizeIndexDefinition(definition string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "\n", "", "\t", "", `"`, "").Replace(definition))
}
