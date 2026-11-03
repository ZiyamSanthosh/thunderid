// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"sort"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var (
	queryConfigTableRelation = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-19",
		PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
			FROM pg_class c
			INNER JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = $1`,
	}
	queryConfigTableColumns = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-20",
		PostgresQuery: `SELECT COLUMN_NAME, IS_NULLABLE, DATA_TYPE,
			COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS maximum_length,
			COALESCE(COLUMN_DEFAULT, 'none') AS column_default
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = $1
			ORDER BY ORDINAL_POSITION`,
	}
	queryConfigTableKeys = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-21",
		PostgresQuery: `SELECT c.contype::TEXT AS constraint_type,
			string_agg(LOWER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names,
			COALESCE(LOWER(referenced_table.relname), '') AS referenced_table,
			COALESCE(string_agg(LOWER(referenced_column.attname), ',' ORDER BY key_position.ordinality)
				FILTER (WHERE referenced_column.attname IS NOT NULL), '') AS referenced_columns,
			CASE WHEN c.contype = 'f' THEN c.confdeltype::TEXT ELSE '' END AS delete_action
			FROM pg_constraint c
			INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
			LEFT JOIN pg_class referenced_table ON referenced_table.oid = c.confrelid
			LEFT JOIN pg_attribute referenced_column ON referenced_column.attrelid = c.confrelid
				AND referenced_column.attnum = c.confkey[key_position.ordinality]
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1
				AND c.contype IN ('p', 'u', 'f')
			GROUP BY c.oid, c.contype, referenced_table.relname, c.confdeltype
			ORDER BY c.contype, column_names, referenced_table`,
	}
	queryConfigTableChecks = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-22",
		PostgresQuery: `SELECT pg_get_constraintdef(c.oid, TRUE) AS definition
			FROM pg_constraint c
			INNER JOIN pg_class table_class ON table_class.oid = c.conrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1 AND c.contype = 'c'
			ORDER BY definition`,
	}
	queryConfigTableIndexes = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-23",
		PostgresQuery: `SELECT index_class.relname AS index_name, i.indisunique::TEXT AS is_unique,
			string_agg(LOWER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names,
			COALESCE(pg_get_expr(i.indpred, i.indrelid), '') AS predicate
			FROM pg_index i
			INNER JOIN pg_class table_class ON table_class.oid = i.indrelid
			INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
			INNER JOIN pg_class index_class ON index_class.oid = i.indexrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
			INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
			WHERE n.nspname = CURRENT_SCHEMA() AND table_class.relname = $1
				AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)
			GROUP BY index_class.relname, i.indisunique, i.indpred, i.indrelid
			ORDER BY index_class.relname`,
	}
)

type configColumnSpec struct {
	name          string
	nullable      string
	dataType      string
	maximumLength string
	defaultValue  string
}

type configKeySpec struct {
	typeCode          string
	columns           string
	referencedTable   string
	referencedColumns string
	deleteAction      string
}

type configCheckSpec struct {
	requiredFragments []string
}

type configIndexSpec struct {
	name               string
	unique             string
	columns            string
	predicateFragments []string
}

type configTableSpec struct {
	name                  string
	columns               []configColumnSpec
	keys                  []configKeySpec
	checks                []configCheckSpec
	indexes               []configIndexSpec
	reservedRelationNames []string
	dependencies          []configTableDependency
}

type configTableDependency struct {
	table       string
	operationID engine.OperationID
}

type configTableInspection struct {
	state   engine.VerificationState
	summary string
}

func newConfigTableOperation(
	id engine.OperationID,
	description string,
	table configTableSpec,
	statements []engine.SQLStatement,
) (engine.Operation, error) {
	inspect := func(ctx context.Context, access engine.ReadAccess) (configTableInspection, error) {
		return inspectConfigTable(ctx, access, table)
	}
	verify := func(
		ctx context.Context,
		access engine.ReadAccess,
		_ engine.ExecutionTarget,
	) (engine.VerificationResult, error) {
		inspection, err := inspect(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
		if inspection.state == engine.VerificationInvalid {
			result.Findings = []engine.Finding{{Code: "unexpected_config_schema", Message: inspection.summary}}
		}
		return result, nil
	}
	readiness := func(
		ctx context.Context,
		access engine.ReadAccess,
		_ engine.PlanContext,
	) (engine.ReadinessResult, error) {
		inspection, err := inspect(ctx, access)
		if err != nil {
			return engine.ReadinessResult{}, err
		}
		if inspection.state != engine.VerificationInvalid {
			if inspection.state == engine.VerificationSource {
				for _, dependency := range table.dependencies {
					rows, queryErr := access.Query(
						ctx, engine.StoreIDConfigDB, queryConfigTableRelation, dependency.table,
					)
					if queryErr != nil {
						return engine.ReadinessResult{}, fmt.Errorf(
							"inspect dependency %s: %w", dependency.table, queryErr,
						)
					}
					if len(rows) == 0 {
						return engine.ReadinessResult{
							State:         engine.ReadinessDeferred,
							Summary:       fmt.Sprintf("%s depends on table %s", table.name, dependency.table),
							DeferredUntil: []engine.OperationID{dependency.operationID},
						}, nil
					}
				}
			}
			return engine.ReadinessResult{State: engine.ReadinessReady, Summary: inspection.summary}, nil
		}
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: "unexpected_config_schema", Message: inspection.summary,
				Remediation: "Remove or reconcile the conflicting or partial configuration schema before retrying",
			}},
		}, nil
	}

	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID: id, Description: description, StoreID: engine.StoreIDConfigDB,
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

func inspectConfigTable(
	ctx context.Context,
	access engine.ReadAccess,
	table configTableSpec,
) (configTableInspection, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableRelation, table.name)
	if err != nil {
		return configTableInspection{}, fmt.Errorf("inspect %s relation: %w", table.name, err)
	}
	if len(rows) == 0 {
		reservedNames := append([]string(nil), table.reservedRelationNames...)
		for _, index := range table.indexes {
			reservedNames = append(reservedNames, index.name)
		}
		for _, name := range reservedNames {
			nameRows, queryErr := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableRelation, name)
			if queryErr != nil {
				return configTableInspection{}, fmt.Errorf("inspect relation name %s: %w", name, queryErr)
			}
			if len(nameRows) != 0 {
				return invalidConfigTable(fmt.Sprintf("Relation name %s is already in use", name)), nil
			}
		}
		return configTableInspection{
			state: engine.VerificationSource, summary: table.name + " is ready to be created",
		}, nil
	}
	if len(rows) != 1 {
		return invalidConfigTable(fmt.Sprintf("Expected one %s relation, found %d", table.name, len(rows))), nil
	}
	kind, err := rows[0].String("relation_kind")
	if err != nil {
		return configTableInspection{}, err
	}
	if kind != "r" {
		return invalidConfigTable(fmt.Sprintf("Relation name %s is not an ordinary table", table.name)), nil
	}
	if err := validateConfigTable(ctx, access, table); err != nil {
		return invalidConfigTable(err.Error()), nil
	}
	return configTableInspection{
		state: engine.VerificationTarget, summary: table.name + " already matches the target schema",
	}, nil
}

func invalidConfigTable(summary string) configTableInspection {
	return configTableInspection{state: engine.VerificationInvalid, summary: summary}
}

func validateConfigTable(ctx context.Context, access engine.ReadAccess, table configTableSpec) error {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableColumns, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table.name, err)
	}
	if len(rows) != len(table.columns) {
		return fmt.Errorf("expected %d %s columns, found %d", len(table.columns), table.name, len(rows))
	}
	for index, expected := range table.columns {
		actual, err := readConfigColumn(rows[index])
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("%s column %d does not match the target schema", table.name, index+1)
		}
	}

	keyRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableKeys, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s keys: %w", table.name, err)
	}
	actualKeys := make([]configKeySpec, 0, len(keyRows))
	for _, row := range keyRows {
		key, readErr := readConfigKey(row)
		if readErr != nil {
			return readErr
		}
		actualKeys = append(actualKeys, key)
	}
	if !equalConfigKeys(actualKeys, table.keys) {
		return fmt.Errorf("%s keys do not match the target schema", table.name)
	}

	checkRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableChecks, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s checks: %w", table.name, err)
	}
	if err := validateConfigChecks(checkRows, table.checks); err != nil {
		return fmt.Errorf("%s checks do not match the target schema: %w", table.name, err)
	}

	indexRows, err := access.Query(ctx, engine.StoreIDConfigDB, queryConfigTableIndexes, table.name)
	if err != nil {
		return fmt.Errorf("inspect %s indexes: %w", table.name, err)
	}
	if err := validateConfigIndexes(indexRows, table.indexes); err != nil {
		return fmt.Errorf("%s indexes do not match the target schema: %w", table.name, err)
	}
	return nil
}

func readConfigColumn(row engine.QueryRow) (configColumnSpec, error) {
	name, err := row.String("column_name")
	if err != nil {
		return configColumnSpec{}, err
	}
	nullable, err := row.String("is_nullable")
	if err != nil {
		return configColumnSpec{}, err
	}
	dataType, err := row.String("data_type")
	if err != nil {
		return configColumnSpec{}, err
	}
	length, err := row.String("maximum_length")
	if err != nil {
		return configColumnSpec{}, err
	}
	defaultValue, err := row.String("column_default")
	if err != nil {
		return configColumnSpec{}, err
	}
	return configColumnSpec{
		name: name, nullable: nullable, dataType: dataType,
		maximumLength: length, defaultValue: normalizeConfigExpression(defaultValue),
	}, nil
}

func readConfigKey(row engine.QueryRow) (configKeySpec, error) {
	typeCode, err := row.String("constraint_type")
	if err != nil {
		return configKeySpec{}, err
	}
	columns, err := row.String("column_names")
	if err != nil {
		return configKeySpec{}, err
	}
	referencedTable, err := configOptionalString(row, "referenced_table")
	if err != nil {
		return configKeySpec{}, err
	}
	referencedColumns, err := configOptionalString(row, "referenced_columns")
	if err != nil {
		return configKeySpec{}, err
	}
	deleteAction, err := configOptionalString(row, "delete_action")
	if err != nil {
		return configKeySpec{}, err
	}
	return configKeySpec{
		typeCode: typeCode, columns: columns, referencedTable: referencedTable,
		referencedColumns: referencedColumns, deleteAction: deleteAction,
	}, nil
}

func equalConfigKeys(actual, expected []configKeySpec) bool {
	key := func(value configKeySpec) string {
		return strings.Join([]string{value.typeCode, value.columns, value.referencedTable,
			value.referencedColumns, value.deleteAction}, "|")
	}
	actualValues := make([]string, 0, len(actual))
	for _, value := range actual {
		actualValues = append(actualValues, key(value))
	}
	expectedValues := make([]string, 0, len(expected))
	for _, value := range expected {
		expectedValues = append(expectedValues, key(value))
	}
	sort.Strings(actualValues)
	sort.Strings(expectedValues)
	return strings.Join(actualValues, "\n") == strings.Join(expectedValues, "\n")
}

func validateConfigChecks(rows []engine.QueryRow, expected []configCheckSpec) error {
	if len(rows) != len(expected) {
		return fmt.Errorf("expected %d checks, found %d", len(expected), len(rows))
	}
	unused := append([]configCheckSpec(nil), expected...)
	for _, row := range rows {
		definition, err := row.String("definition")
		if err != nil {
			return err
		}
		normalized := normalizeConfigExpression(definition)
		matched := -1
		for index, check := range unused {
			matches := true
			for _, fragment := range check.requiredFragments {
				if !strings.Contains(normalized, normalizeConfigExpression(fragment)) {
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

func validateConfigIndexes(rows []engine.QueryRow, expected []configIndexSpec) error {
	if len(rows) != len(expected) {
		return fmt.Errorf("expected %d explicit indexes, found %d", len(expected), len(rows))
	}
	byName := make(map[string]configIndexSpec, len(expected))
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
		unique, err := row.String("is_unique")
		if err != nil {
			return err
		}
		columns, err := row.String("column_names")
		if err != nil {
			return err
		}
		predicate, err := configOptionalString(row, "predicate")
		if err != nil {
			return err
		}
		if unique != expectedIndex.unique || columns != expectedIndex.columns {
			return fmt.Errorf("index %s has unexpected uniqueness or columns", name)
		}
		normalizedPredicate := normalizeConfigExpression(predicate)
		for _, fragment := range expectedIndex.predicateFragments {
			if !strings.Contains(normalizedPredicate, normalizeConfigExpression(fragment)) {
				return fmt.Errorf("index %s has an unexpected predicate", name)
			}
		}
		delete(byName, name)
	}
	return nil
}

func normalizeConfigExpression(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

func configOptionalString(row engine.QueryRow, column string) (string, error) {
	value, exists := row[column]
	if !exists || value == nil {
		return "", nil
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("column %q has unsupported type %T", column, value)
	}
}
