// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var (
	queryTableRelation = dbmodel.DBQuery{
		ID: "MIG-USM-TBL-01",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = $1`,
	}
	queryTableColumns = dbmodel.DBQuery{
		ID: "MIG-USM-TBL-02",
		PostgresQuery: `SELECT COLUMN_NAME, IS_NULLABLE, DATA_TYPE,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length,
			COALESCE(COLUMN_DEFAULT, 'none') AS column_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = $1
			ORDER BY ORDINAL_POSITION`,
	}
	queryTableKeys = dbmodel.DBQuery{
		ID: "MIG-USM-TBL-03",
		PostgresQuery: `SELECT c.contype::TEXT AS constraint_type,
			string_agg(LOWER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_constraint c
			INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1
				AND c.contype IN ('p', 'u', 'f')
			GROUP BY c.oid, c.contype
			ORDER BY c.contype, column_names`,
	}
	queryTableChecks = dbmodel.DBQuery{
		ID: "MIG-USM-TBL-04",
		PostgresQuery: `SELECT pg_get_constraintdef(c.oid, TRUE) AS definition
			FROM pg_constraint c
			INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1 AND c.contype = 'c'
			ORDER BY definition`,
	}
	queryTableIndexes = dbmodel.DBQuery{
		ID: "MIG-USM-TBL-05",
		PostgresQuery: `SELECT index_class.relname AS index_name, i.indisunique::TEXT AS is_unique,
			string_agg(LOWER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
			FROM pg_index i
			INNER JOIN pg_class table_class ON table_class.oid = i.indrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			INNER JOIN pg_class index_class ON index_class.oid = i.indexrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1
				AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)
			GROUP BY index_class.relname, i.indisunique
			ORDER BY index_class.relname`,
	}
)

// columnSpec describes one column as INFORMATION_SCHEMA reports it, in the order name, nullable, data type,
// maximum length and normalized default. Column names are lowercase because the DDL declares them unquoted.
type columnSpec struct {
	name          string
	nullable      string
	dataType      string
	maximumLength string
	defaultValue  string
}

// keySpec describes a primary key ("p") or unique constraint ("u") by its ordered columns.
type keySpec struct {
	typeCode string
	columns  string
}

// checkSpec matches one check constraint whose definition contains every fragment.
type checkSpec struct {
	requiredFragments []string
}

// indexSpec describes an explicit index, one not backing a constraint.
type indexSpec struct {
	name    string
	unique  string
	columns string
}

// tableSpec is the exact target shape of a table that an operation creates.
type tableSpec struct {
	storeID engine.StoreID
	name    string
	columns []columnSpec
	keys    []keySpec
	checks  []checkSpec
	indexes []indexSpec
}

type tableInspection struct {
	state   engine.VerificationState
	summary string
}

// newCreateTableOperation returns an atomic operation that creates one table and its indexes. The table is
// at source while absent, and at target only when every column, key, check and index matches the spec.
func newCreateTableOperation(
	id engine.OperationID,
	description string,
	table tableSpec,
	statements []engine.SQLStatement,
) (engine.Operation, error) {
	verify := func(
		ctx context.Context,
		access engine.ReadAccess,
		_ engine.ExecutionTarget,
	) (engine.VerificationResult, error) {
		inspection, err := inspectTable(ctx, access, table)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
		if inspection.state == engine.VerificationInvalid {
			result.Findings = []engine.Finding{{Code: "unexpected_table_schema", Message: inspection.summary}}
		}
		return result, nil
	}
	readiness := func(
		ctx context.Context,
		access engine.ReadAccess,
		_ engine.PlanContext,
	) (engine.ReadinessResult, error) {
		inspection, err := inspectTable(ctx, access, table)
		if err != nil {
			return engine.ReadinessResult{}, err
		}
		if inspection.state != engine.VerificationInvalid {
			return engine.ReadinessResult{State: engine.ReadinessReady, Summary: inspection.summary}, nil
		}
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: "unexpected_table_schema", Message: inspection.summary,
				Remediation: "Remove or reconcile the conflicting or partial table before retrying",
			}},
		}, nil
	}

	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID: id, Description: description, StoreID: table.storeID,
			Kind: engine.OperationKindSchemaChange, InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: readiness,
		PreVerifier:      verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: statements},
		},
		PostVerifier: verify,
	})
}

func inspectTable(ctx context.Context, access engine.ReadAccess, table tableSpec) (tableInspection, error) {
	rows, err := access.Query(ctx, table.storeID, queryTableRelation, table.name)
	if err != nil {
		return tableInspection{}, fmt.Errorf("inspect %s relation: %w", table.name, err)
	}
	if len(rows) == 0 {
		for _, index := range table.indexes {
			nameRows, queryErr := access.Query(ctx, table.storeID, queryTableRelation, index.name)
			if queryErr != nil {
				return tableInspection{}, fmt.Errorf("inspect relation name %s: %w", index.name, queryErr)
			}
			if len(nameRows) != 0 {
				return invalidTable(fmt.Sprintf("Relation name %s is already in use", index.name)), nil
			}
		}
		return tableInspection{state: engine.VerificationSource, summary: table.name + " is ready to be created"}, nil
	}
	if len(rows) != 1 {
		return invalidTable(fmt.Sprintf("Expected one %s relation, found %d", table.name, len(rows))), nil
	}
	kind, err := rows[0].String("relation_kind")
	if err != nil {
		return tableInspection{}, err
	}
	if kind != "r" {
		return invalidTable(fmt.Sprintf("Relation name %s is not an ordinary table", table.name)), nil
	}
	if err := validateTable(ctx, access, table); err != nil {
		return invalidTable(err.Error()), nil
	}
	return tableInspection{
		state: engine.VerificationTarget, summary: table.name + " already matches the target schema",
	}, nil
}

func invalidTable(summary string) tableInspection {
	return tableInspection{state: engine.VerificationInvalid, summary: summary}
}

func validateTable(ctx context.Context, access engine.ReadAccess, table tableSpec) error {
	rows, err := access.Query(ctx, table.storeID, queryTableColumns, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table.name, err)
	}
	if len(rows) != len(table.columns) {
		return fmt.Errorf("expected %d %s columns, found %d", len(table.columns), table.name, len(rows))
	}
	for index, expected := range table.columns {
		actual, err := readColumn(rows[index])
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("%s column %d does not match the target schema", table.name, index+1)
		}
	}

	keyRows, err := access.Query(ctx, table.storeID, queryTableKeys, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s keys: %w", table.name, err)
	}
	actualKeys := make([]keySpec, 0, len(keyRows))
	for _, row := range keyRows {
		typeCode, typeErr := row.String("constraint_type")
		columns, columnsErr := row.String("column_names")
		if err := errors.Join(typeErr, columnsErr); err != nil {
			return fmt.Errorf("read %s key: %w", table.name, err)
		}
		actualKeys = append(actualKeys, keySpec{typeCode: typeCode, columns: columns})
	}
	if !equalKeys(actualKeys, table.keys) {
		return fmt.Errorf("%s keys do not match the target schema", table.name)
	}

	checkRows, err := access.Query(ctx, table.storeID, queryTableChecks, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s checks: %w", table.name, err)
	}
	if err := validateChecks(checkRows, table.checks); err != nil {
		return fmt.Errorf("%s checks do not match the target schema: %w", table.name, err)
	}

	indexRows, err := access.Query(ctx, table.storeID, queryTableIndexes, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s indexes: %w", table.name, err)
	}
	if err := validateIndexes(indexRows, table.indexes); err != nil {
		return fmt.Errorf("%s indexes do not match the target schema: %w", table.name, err)
	}
	return nil
}

func readColumn(row engine.QueryRow) (columnSpec, error) {
	name, nameErr := row.String("column_name")
	nullable, nullableErr := row.String("is_nullable")
	dataType, dataTypeErr := row.String("data_type")
	length, lengthErr := row.String("maximum_length")
	defaultValue, defaultErr := row.String("column_default")
	if err := errors.Join(nameErr, nullableErr, dataTypeErr, lengthErr, defaultErr); err != nil {
		return columnSpec{}, err
	}
	return columnSpec{
		name: name, nullable: nullable, dataType: dataType,
		maximumLength: length, defaultValue: normalizeExpression(defaultValue),
	}, nil
}

func equalKeys(actual, expected []keySpec) bool {
	render := func(keys []keySpec) string {
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			values = append(values, key.typeCode+"|"+key.columns)
		}
		sort.Strings(values)
		return strings.Join(values, "\n")
	}
	return render(actual) == render(expected)
}

func validateChecks(rows []engine.QueryRow, expected []checkSpec) error {
	if len(rows) != len(expected) {
		return fmt.Errorf("expected %d checks, found %d", len(expected), len(rows))
	}
	unused := append([]checkSpec(nil), expected...)
	for _, row := range rows {
		definition, err := row.String("definition")
		if err != nil {
			return err
		}
		normalized := normalizeExpression(definition)
		matched := -1
		for index, check := range unused {
			matches := true
			for _, fragment := range check.requiredFragments {
				if !strings.Contains(normalized, normalizeExpression(fragment)) {
					matches = false
					break
				}
			}
			if matches {
				matched = index
				break
			}
		}
		if matched < 0 {
			return fmt.Errorf("unexpected check %q", definition)
		}
		unused = append(unused[:matched], unused[matched+1:]...)
	}
	return nil
}

func validateIndexes(rows []engine.QueryRow, expected []indexSpec) error {
	if len(rows) != len(expected) {
		return fmt.Errorf("expected %d explicit indexes, found %d", len(expected), len(rows))
	}
	byName := make(map[string]indexSpec, len(expected))
	for _, index := range expected {
		byName[index.name] = index
	}
	for _, row := range rows {
		name, err := row.String("index_name")
		if err != nil {
			return err
		}
		expectedIndex, ok := byName[name]
		if !ok {
			return fmt.Errorf("unexpected index %s", name)
		}
		unique, uniqueErr := row.String("is_unique")
		columns, columnsErr := row.String("column_names")
		if err := errors.Join(uniqueErr, columnsErr); err != nil {
			return fmt.Errorf("read index %s: %w", name, err)
		}
		if unique != expectedIndex.unique || columns != expectedIndex.columns {
			return fmt.Errorf("index %s has unexpected uniqueness or columns", name)
		}
		delete(byName, name)
	}
	return nil
}

func normalizeExpression(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

// emptyStringDefault is how an empty string default of a character varying column reads back from the catalog.
var emptyStringDefault = normalizeExpression("''::character varying")
