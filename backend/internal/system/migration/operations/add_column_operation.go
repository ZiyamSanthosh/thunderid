// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// addColumnSpec describes the columns, and the indexes on them, that an operation adds to an existing table.
type addColumnSpec struct {
	id          engine.OperationID
	description string
	storeID     engine.StoreID
	table       string
	// columns are the added columns as INFORMATION_SCHEMA reports them. Their position in the table is not
	// compared, because ALTER TABLE appends a column.
	columns []columnSpec
	// indexes are the explicit indexes the operation creates. They may name columns the table already has.
	indexes []indexSpec
	// statements are the ordered statements that add them.
	statements []engine.SQLStatement
}

// newAddColumnOperation returns an atomic operation that adds columns, and optionally indexes, to a table. The
// change is at source while every added column and index is absent and at target when each has exactly the
// expected definition. A definition that differs, a partly applied change, a name taken by another relation or a
// table that is not an ordinary table is invalid.
func newAddColumnOperation(spec addColumnSpec) (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		return verifyAddedColumns(ctx, access, spec)
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID: spec.id, Description: spec.description, StoreID: spec.storeID,
			Kind: engine.OperationKindSchemaChange, InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			return readinessFromVerification(ctx, access,
				func(ctx context.Context, access engine.ReadAccess, _ string) (engine.VerificationResult, error) {
					return verifyAddedColumns(ctx, access, spec)
				})
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: spec.statements},
		},
		PostVerifier: verify,
	})
}

func verifyAddedColumns(ctx context.Context, access engine.ReadAccess, spec addColumnSpec) (
	engine.VerificationResult, error) {
	relations, err := access.Query(ctx, spec.storeID, queryTableRelation, spec.table)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s relation: %w", spec.table, err)
	}
	if len(relations) != 1 {
		return invalidVerification("unexpected_table_schema", fmt.Sprintf(
			"Expected one %s table, found %d", spec.table, len(relations))), nil
	}
	kind, err := relations[0].String("relation_kind")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if kind != "r" {
		return invalidVerification("unexpected_table_schema", fmt.Sprintf(
			"Relation name %s is not an ordinary table", spec.table)), nil
	}

	present := 0
	columnRows, err := access.Query(ctx, spec.storeID, queryTableColumns, spec.table)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s columns: %w", spec.table, err)
	}
	actualColumns := make(map[string]columnSpec, len(columnRows))
	for _, row := range columnRows {
		actual, err := readColumn(row)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		actualColumns[actual.name] = actual
	}
	for _, expected := range spec.columns {
		actual, exists := actualColumns[expected.name]
		if !exists {
			continue
		}
		if actual != expected {
			return invalidVerification("unexpected_column_definition", fmt.Sprintf(
				"Unexpected %s.%s definition: nullable=%q, type=%q, length=%q, default=%q", spec.table,
				expected.name, actual.nullable, actual.dataType, actual.maximumLength, actual.defaultValue)), nil
		}
		present++
	}

	if len(spec.indexes) > 0 {
		indexPresent, invalid, err := countAddedIndexes(ctx, access, spec)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if invalid != "" {
			return invalidVerification("unexpected_index_definition", invalid), nil
		}
		present += indexPresent
	}

	changes, verb := describeAddedColumns(spec)
	switch present {
	case 0:
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: changes + " " + verb + " absent"}, nil
	case len(spec.columns) + len(spec.indexes):
		return engine.VerificationResult{State: engine.VerificationTarget,
			Summary: changes + " " + verb + " present"}, nil
	default:
		return invalidVerification("partially_applied_schema_change", fmt.Sprintf(
			"%s are partly present: %d of %d", changes, present, len(spec.columns)+len(spec.indexes))), nil
	}
}

// countAddedIndexes counts the expected indexes already on the table. It returns a description when one has a
// different definition, or when its name belongs to another relation.
func countAddedIndexes(ctx context.Context, access engine.ReadAccess, spec addColumnSpec) (int, string, error) {
	rows, err := access.Query(ctx, spec.storeID, queryTableIndexes, spec.table)
	if err != nil {
		return 0, "", fmt.Errorf("inspect %s indexes: %w", spec.table, err)
	}
	actual := make(map[string]indexSpec, len(rows))
	for _, row := range rows {
		name, nameErr := row.String("index_name")
		unique, uniqueErr := row.String("is_unique")
		columns, columnsErr := row.String("column_names")
		if err := errors.Join(nameErr, uniqueErr, columnsErr); err != nil {
			return 0, "", fmt.Errorf("read %s index: %w", spec.table, err)
		}
		actual[name] = indexSpec{name: name, unique: unique, columns: columns}
	}
	count := 0
	for _, expected := range spec.indexes {
		found, exists := actual[expected.name]
		if exists {
			if found != expected {
				return 0, fmt.Sprintf("Index %s on %s has unexpected uniqueness or columns", expected.name,
					spec.table), nil
			}
			count++
			continue
		}
		taken, err := access.Query(ctx, spec.storeID, queryTableRelation, expected.name)
		if err != nil {
			return 0, "", fmt.Errorf("inspect relation name %s: %w", expected.name, err)
		}
		if len(taken) != 0 {
			return 0, fmt.Sprintf("Relation name %s is already in use and is not an index of %s", expected.name,
				spec.table), nil
		}
	}
	return count, "", nil
}

// describeAddedColumns names what the operation adds, with the form of "to be" that agrees with it.
func describeAddedColumns(spec addColumnSpec) (string, string) {
	names := make([]string, 0, len(spec.columns)+len(spec.indexes))
	for _, column := range spec.columns {
		names = append(names, spec.table+"."+column.name)
	}
	for _, index := range spec.indexes {
		names = append(names, "index "+index.name)
	}
	if len(names) == 1 {
		return names[0], "is"
	}
	return strings.Join(names, ", "), "are"
}
