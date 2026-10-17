// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

var (
	primaryKeyTablePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	primaryKeyIndexPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

type onlinePrimaryKeyConfig struct {
	definition    engine.OperationDefinition
	table         string
	legacyColumns []string
	targetColumns []string
	indexName     string
	queryPrefix   string
	references    []primaryKeyReference
	sourceSummary string
	targetSummary string
	findingCode   string
	remediation   string
}

type primaryKeyReference struct {
	table          string
	legacyColumns  []string
	targetColumns  []string
	constraintName string
	onDelete       string
	onUpdate       string
}

type primaryKeyReferenceQueries struct {
	inspect  dbmodel.DBQuery
	add      dbmodel.DBQuery
	validate dbmodel.DBQuery
}

type onlinePrimaryKeyOperation struct {
	config                   onlinePrimaryKeyConfig
	preserveLegacyUniqueness bool
	tableQuery               dbmodel.DBQuery
	columnsQuery             dbmodel.DBQuery
	primaryKeyQuery          dbmodel.DBQuery
	indexQuery               dbmodel.DBQuery
	legacyIndexQuery         dbmodel.DBQuery
	createIndexQuery         dbmodel.DBQuery
	dropIndexQuery           dbmodel.DBQuery
	createLegacyIndexQuery   dbmodel.DBQuery
	dropLegacyIndexQuery     dbmodel.DBQuery
	lockTimeoutQuery         dbmodel.DBQuery
	finalizeQuery            dbmodel.DBQuery
	referenceQueries         []primaryKeyReferenceQueries
}

type onlinePrimaryKeyInspection struct {
	state              engine.VerificationState
	summary            string
	indexPresent       bool
	indexValid         bool
	legacyIndexPresent bool
	legacyIndexValid   bool
	references         []primaryKeyReferenceInspection
}

type primaryKeyReferenceInspection struct {
	legacyConstraintName string
	targetConstraintName string
	targetPresent        bool
	targetValid          bool
}

func newOnlinePrimaryKeyOperation(config onlinePrimaryKeyConfig) (*onlinePrimaryKeyOperation, error) {
	if err := validateOnlinePrimaryKeyConfig(config); err != nil {
		return nil, err
	}
	legacyColumns := strings.Join(config.legacyColumns, ",")
	quotedLegacyColumns := quotePrimaryKeyIdentifiers(config.legacyColumns)
	quotedTargetColumns := quotePrimaryKeyIdentifiers(config.targetColumns)
	legacyIndexName := legacyPrimaryKeyIndexName(config.table)
	preserveLegacyUniqueness := config.targetColumns[0] == "DEPLOYMENT_ID"
	operation := &onlinePrimaryKeyOperation{
		config: config, preserveLegacyUniqueness: preserveLegacyUniqueness,
		tableQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-01",
			PostgresQuery: `SELECT c.relkind::TEXT AS relation_kind
				FROM pg_class c
				INNER JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = $1`,
		},
		columnsQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-02",
			PostgresQuery: `SELECT UPPER(COLUMN_NAME) AS column_name, IS_NULLABLE AS is_nullable
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = $1
				AND UPPER(COLUMN_NAME) = ANY($2::TEXT[])
				ORDER BY ORDINAL_POSITION`,
		},
		primaryKeyQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-03",
			PostgresQuery: `SELECT c.conname AS constraint_name,
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
				FROM pg_constraint c
				INNER JOIN pg_class t ON t.oid = c.conrelid
				INNER JOIN pg_namespace n ON n.oid = t.relnamespace
				CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = $1 AND c.contype = 'p'
				GROUP BY c.oid, c.conname`,
		},
		indexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-04",
			PostgresQuery: `SELECT i.indisunique AS is_unique, i.indisvalid AS is_valid,
				i.indisready AS is_ready,
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
				FROM pg_class idx
				INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
				INNER JOIN pg_index i ON i.indexrelid = idx.oid
				INNER JOIN pg_class t ON t.oid = i.indrelid
				CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = $1 AND idx.relname = $2
				GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready`,
		},
		legacyIndexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-09",
			PostgresQuery: `SELECT i.indisunique AS is_unique, i.indisvalid AS is_valid,
				i.indisready AS is_ready,
				string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) AS column_names
				FROM pg_class idx
				INNER JOIN pg_namespace n ON n.oid = idx.relnamespace
				INNER JOIN pg_index i ON i.indexrelid = idx.oid
				INNER JOIN pg_class t ON t.oid = i.indrelid
				CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
				INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
				WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = $1 AND idx.relname = $2
				GROUP BY i.indexrelid, i.indisunique, i.indisvalid, i.indisready`,
		},
		createIndexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-05",
			PostgresQuery: fmt.Sprintf(`CREATE UNIQUE INDEX CONCURRENTLY %s ON %s (%s)`,
				quotePrimaryKeyIdentifier(config.indexName), quotePrimaryKeyIdentifier(config.table),
				quotedTargetColumns),
		},
		dropIndexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-06",
			PostgresQuery: fmt.Sprintf(`DROP INDEX CONCURRENTLY %s`,
				quotePrimaryKeyIdentifier(config.indexName)),
		},
		createLegacyIndexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-10",
			PostgresQuery: fmt.Sprintf(`CREATE UNIQUE INDEX CONCURRENTLY %s ON %s (%s)`,
				quotePrimaryKeyIdentifier(legacyIndexName), quotePrimaryKeyIdentifier(config.table),
				quotedLegacyColumns),
		},
		dropLegacyIndexQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-11",
			PostgresQuery: fmt.Sprintf(`DROP INDEX CONCURRENTLY %s`,
				quotePrimaryKeyIdentifier(legacyIndexName)),
		},
		lockTimeoutQuery: dbmodel.DBQuery{
			ID:            config.queryPrefix + "-07",
			PostgresQuery: "SET LOCAL lock_timeout = '2s'",
		},
		finalizeQuery: dbmodel.DBQuery{
			ID: config.queryPrefix + "-08",
			PostgresQuery: fmt.Sprintf(`DO $migration$
DECLARE
	primary_key_name TEXT;
BEGIN
	SELECT c.conname INTO primary_key_name
	FROM pg_constraint c
	INNER JOIN pg_class t ON t.oid = c.conrelid
	INNER JOIN pg_namespace n ON n.oid = t.relnamespace
	CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
	INNER JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_position.attnum
	WHERE n.nspname = CURRENT_SCHEMA() AND t.relname = %s AND c.contype = 'p'
	GROUP BY c.oid, c.conname
	HAVING string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality) = %s;

	IF primary_key_name IS NULL THEN
		RAISE EXCEPTION 'expected legacy primary key was not found for %s';
	END IF;

	EXECUTE format('ALTER TABLE %%I.%%I DROP CONSTRAINT %%I',
		CURRENT_SCHEMA(), %s, primary_key_name);
	EXECUTE format('ALTER TABLE %%I.%%I ADD CONSTRAINT %%I PRIMARY KEY USING INDEX %%I',
		CURRENT_SCHEMA(), %s, primary_key_name, %s);
END;
$migration$;`, quotePrimaryKeyLiteral(config.table), quotePrimaryKeyLiteral(legacyColumns), config.table,
				quotePrimaryKeyLiteral(config.table), quotePrimaryKeyLiteral(config.table),
				quotePrimaryKeyLiteral(config.indexName)),
		},
	}
	for index, reference := range config.references {
		queryID := fmt.Sprintf("%s-R%02d", config.queryPrefix, index+1)
		addReference := fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s)
			REFERENCES %s (%s) ON DELETE %s ON UPDATE %s NOT VALID`,
			quotePrimaryKeyIdentifier(reference.table),
			quotePrimaryKeyIdentifier(reference.constraintName),
			quotePrimaryKeyIdentifiers(reference.targetColumns),
			quotePrimaryKeyIdentifier(config.table), quotedTargetColumns,
			reference.onDelete, reference.onUpdate)
		validateReference := fmt.Sprintf(`ALTER TABLE %s VALIDATE CONSTRAINT %s`,
			quotePrimaryKeyIdentifier(reference.table),
			quotePrimaryKeyIdentifier(reference.constraintName))
		operation.referenceQueries = append(operation.referenceQueries, primaryKeyReferenceQueries{
			inspect: dbmodel.DBQuery{
				ID: queryID + "-01",
				PostgresQuery: `SELECT c.conname AS constraint_name, c.convalidated AS is_valid,
					(SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
					 FROM unnest(c.conkey) WITH ORDINALITY AS key_position(attnum, ordinality)
					 INNER JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = key_position.attnum)
						AS child_columns,
					(SELECT string_agg(UPPER(a.attname), ',' ORDER BY key_position.ordinality)
					 FROM unnest(c.confkey) WITH ORDINALITY AS key_position(attnum, ordinality)
					 INNER JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = key_position.attnum)
						AS parent_columns,
					c.confdeltype::TEXT AS delete_action, c.confupdtype::TEXT AS update_action
				FROM pg_constraint c
				INNER JOIN pg_class child ON child.oid = c.conrelid
				INNER JOIN pg_namespace child_namespace ON child_namespace.oid = child.relnamespace
				INNER JOIN pg_class parent ON parent.oid = c.confrelid
				INNER JOIN pg_namespace parent_namespace ON parent_namespace.oid = parent.relnamespace
				WHERE c.contype = 'f' AND child_namespace.nspname = CURRENT_SCHEMA()
					AND parent_namespace.nspname = CURRENT_SCHEMA()
					AND child.relname = $1 AND parent.relname = $2`,
			},
			add: dbmodel.DBQuery{
				ID: queryID + "-02", PostgresQuery: boundedPrimaryKeyDDL(addReference),
			},
			validate: dbmodel.DBQuery{
				ID: queryID + "-03", PostgresQuery: boundedPrimaryKeyDDL(validateReference),
			},
		})
	}
	return operation, nil
}

func validateOnlinePrimaryKeyConfig(config onlinePrimaryKeyConfig) error {
	if config.definition.InterruptionMode != engine.InterruptionModeOnline {
		return fmt.Errorf("create online primary-key operation %q: interruption mode must be online",
			config.definition.ID)
	}
	if config.definition.Kind != engine.OperationKindSchemaChange {
		return fmt.Errorf("create online primary-key operation %q: kind must be schema change",
			config.definition.ID)
	}
	if !primaryKeyTablePattern.MatchString(config.table) {
		return fmt.Errorf("create online primary-key operation %q: invalid table %q",
			config.definition.ID, config.table)
	}
	if !primaryKeyIndexPattern.MatchString(config.indexName) {
		return fmt.Errorf("create online primary-key operation %q: invalid index %q",
			config.definition.ID, config.indexName)
	}
	if len(config.legacyColumns) == 0 || len(config.targetColumns) == 0 {
		return fmt.Errorf("create online primary-key operation %q: key columns are required",
			config.definition.ID)
	}
	for _, column := range append(append([]string(nil), config.legacyColumns...), config.targetColumns...) {
		if !primaryKeyTablePattern.MatchString(column) {
			return fmt.Errorf("create online primary-key operation %q: invalid column %q",
				config.definition.ID, column)
		}
	}
	if strings.TrimSpace(config.queryPrefix) == "" {
		return fmt.Errorf("create online primary-key operation %q: query prefix is required",
			config.definition.ID)
	}
	for _, reference := range config.references {
		if !primaryKeyTablePattern.MatchString(reference.table) {
			return fmt.Errorf("create online primary-key operation %q: invalid reference table %q",
				config.definition.ID, reference.table)
		}
		if !primaryKeyIndexPattern.MatchString(reference.constraintName) {
			return fmt.Errorf("create online primary-key operation %q: invalid reference constraint %q",
				config.definition.ID, reference.constraintName)
		}
		if len(reference.legacyColumns) == 0 || len(reference.legacyColumns) != len(config.legacyColumns) ||
			len(reference.targetColumns) != len(config.targetColumns) {
			return fmt.Errorf("create online primary-key operation %q: invalid %s reference columns",
				config.definition.ID, reference.table)
		}
		for _, column := range append(append([]string(nil), reference.legacyColumns...),
			reference.targetColumns...) {
			if !primaryKeyTablePattern.MatchString(column) {
				return fmt.Errorf("create online primary-key operation %q: invalid reference column %q",
					config.definition.ID, column)
			}
		}
		if _, ok := primaryKeyReferenceActionCode(reference.onDelete); !ok {
			return fmt.Errorf("create online primary-key operation %q: invalid ON DELETE action %q",
				config.definition.ID, reference.onDelete)
		}
		if _, ok := primaryKeyReferenceActionCode(reference.onUpdate); !ok {
			return fmt.Errorf("create online primary-key operation %q: invalid ON UPDATE action %q",
				config.definition.ID, reference.onUpdate)
		}
	}
	return nil
}

func (o *onlinePrimaryKeyOperation) Definition() engine.OperationDefinition {
	return o.config.definition
}

func (o *onlinePrimaryKeyOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	inspection, err := o.inspect(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if inspection.state == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: inspection.summary,
			Findings: []engine.Finding{{
				Code: o.findingCode(), Message: inspection.summary,
				Remediation: o.remediation(),
			}},
		}, nil
	}
	return engine.ReadinessResult{State: engine.ReadinessReady, Summary: inspection.summary}, nil
}

func (o *onlinePrimaryKeyOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (o *onlinePrimaryKeyOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *onlinePrimaryKeyOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	access := execution.Access()
	inspection, err := o.inspect(ctx, access)
	if err != nil {
		return err
	}
	if inspection.state == engine.VerificationTarget {
		return nil
	}
	if inspection.state != engine.VerificationSource {
		return fmt.Errorf("prepare %s primary key: %s", o.config.table, inspection.summary)
	}
	if inspection.indexPresent {
		if !inspection.indexValid {
			if _, err := access.Execute(ctx, o.config.definition.StoreID, o.dropIndexQuery); err != nil {
				return fmt.Errorf("drop invalid %s primary-key index: %w", o.config.table, err)
			}
			inspection.indexPresent = false
		}
	}
	if !inspection.indexPresent {
		if _, err := access.Execute(ctx, o.config.definition.StoreID, o.createIndexQuery); err != nil {
			return fmt.Errorf("create %s primary-key index concurrently: %w", o.config.table, err)
		}
	}
	if o.preserveLegacyUniqueness && inspection.legacyIndexPresent && !inspection.legacyIndexValid {
		if _, err := access.Execute(ctx, o.config.definition.StoreID, o.dropLegacyIndexQuery); err != nil {
			return fmt.Errorf("drop invalid %s legacy compatibility index: %w", o.config.table, err)
		}
		inspection.legacyIndexPresent = false
	}
	if o.preserveLegacyUniqueness && !inspection.legacyIndexPresent {
		if _, err := access.Execute(ctx, o.config.definition.StoreID, o.createLegacyIndexQuery); err != nil {
			return fmt.Errorf("create %s legacy compatibility index concurrently: %w", o.config.table, err)
		}
	}
	for index, reference := range o.config.references {
		state := inspection.references[index]
		queries := o.referenceQueries[index]
		if !state.targetPresent {
			if _, err := access.Execute(ctx, o.config.definition.StoreID, queries.add); err != nil {
				return fmt.Errorf("add %s deployment-scoped foreign key: %w", reference.table, err)
			}
		}
		if !state.targetValid {
			validateQuery := queries.validate
			if state.targetPresent {
				validateQuery.PostgresQuery = boundedPrimaryKeyDDL(fmt.Sprintf(
					`ALTER TABLE %s VALIDATE CONSTRAINT %s`,
					quotePrimaryKeyIdentifier(reference.table),
					quoteDiscoveredPrimaryKeyIdentifier(state.targetConstraintName)))
			}
			if _, err := access.Execute(ctx, o.config.definition.StoreID, validateQuery); err != nil {
				return fmt.Errorf("validate %s deployment-scoped foreign key: %w", reference.table, err)
			}
		}
	}
	return nil
}

func (o *onlinePrimaryKeyOperation) Finalize(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	access := execution.Access()
	inspection, err := o.inspect(ctx, access)
	if err != nil {
		return err
	}
	if inspection.state == engine.VerificationTarget {
		return nil
	}
	if inspection.state != engine.VerificationSource || !inspection.indexValid ||
		(o.preserveLegacyUniqueness && !inspection.legacyIndexValid) {
		return fmt.Errorf("finalize %s primary key: online preparation is incomplete", o.config.table)
	}
	for index, reference := range inspection.references {
		if reference.legacyConstraintName == "" || !reference.targetValid {
			return fmt.Errorf("finalize %s primary key: %s foreign-key preparation is incomplete",
				o.config.table, o.config.references[index].table)
		}
	}
	if _, err := access.Execute(ctx, o.config.definition.StoreID, o.lockTimeoutQuery); err != nil {
		return fmt.Errorf("set %s primary-key lock timeout: %w", o.config.table, err)
	}
	for index, reference := range o.config.references {
		dropQuery := dbmodel.DBQuery{
			ID: fmt.Sprintf("%s-R%02d-04", o.config.queryPrefix, index+1),
			PostgresQuery: fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT %s`,
				quotePrimaryKeyIdentifier(reference.table),
				quoteDiscoveredPrimaryKeyIdentifier(inspection.references[index].legacyConstraintName)),
		}
		if _, err := access.Execute(ctx, o.config.definition.StoreID, dropQuery); err != nil {
			return fmt.Errorf("drop %s legacy foreign key: %w", reference.table, err)
		}
	}
	if _, err := access.Execute(ctx, o.config.definition.StoreID, o.finalizeQuery); err != nil {
		return fmt.Errorf("replace %s primary key: %w", o.config.table, err)
	}
	return nil
}

func (o *onlinePrimaryKeyOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access)
}

func (o *onlinePrimaryKeyOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, error) {
	inspection, err := o.inspect(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{Code: o.findingCode(), Message: inspection.summary}}
	}
	return result, nil
}

func (o *onlinePrimaryKeyOperation) inspect(
	ctx context.Context,
	access engine.ReadAccess,
) (onlinePrimaryKeyInspection, error) {
	tableRows, err := access.Query(ctx, o.config.definition.StoreID, o.tableQuery, o.config.table)
	if err != nil {
		return onlinePrimaryKeyInspection{}, fmt.Errorf("inspect %s table: %w", o.config.table, err)
	}
	if len(tableRows) != 1 {
		return o.invalid(fmt.Sprintf("Expected one %s table, found %d", o.config.table, len(tableRows))), nil
	}
	relationKind, err := tableRows[0].String("relation_kind")
	if err != nil {
		return onlinePrimaryKeyInspection{}, err
	}
	if relationKind != "r" {
		return o.invalid(fmt.Sprintf("Relation %s is not an ordinary table", o.config.table)), nil
	}

	requiredColumns := uniquePrimaryKeyColumns(o.config.legacyColumns, o.config.targetColumns)
	columnRows, err := access.Query(ctx, o.config.definition.StoreID, o.columnsQuery,
		o.config.table, primaryKeyTextArray(requiredColumns))
	if err != nil {
		return onlinePrimaryKeyInspection{}, fmt.Errorf("inspect %s key columns: %w", o.config.table, err)
	}
	if len(columnRows) != len(requiredColumns) {
		return o.invalid(fmt.Sprintf("Expected %d %s key columns, found %d",
			len(requiredColumns), o.config.table, len(columnRows))), nil
	}
	for _, row := range columnRows {
		name, nameErr := row.String("column_name")
		nullable, nullableErr := row.String("is_nullable")
		if nameErr != nil || nullableErr != nil {
			return onlinePrimaryKeyInspection{}, errorsJoin(nameErr, nullableErr)
		}
		if !containsPrimaryKeyColumn(requiredColumns, name) || nullable != "NO" {
			return o.invalid(fmt.Sprintf("Unexpected %s key column %s nullable=%s",
				o.config.table, name, nullable)), nil
		}
	}

	primaryKeyRows, err := access.Query(ctx, o.config.definition.StoreID, o.primaryKeyQuery, o.config.table)
	if err != nil {
		return onlinePrimaryKeyInspection{}, fmt.Errorf("inspect %s primary key: %w", o.config.table, err)
	}
	if len(primaryKeyRows) != 1 {
		return o.invalid(fmt.Sprintf("Expected one %s primary key, found %d",
			o.config.table, len(primaryKeyRows))), nil
	}
	primaryKeyColumns, err := primaryKeyRows[0].String("column_names")
	if err != nil {
		return onlinePrimaryKeyInspection{}, err
	}

	indexRows, err := access.Query(ctx, o.config.definition.StoreID, o.indexQuery,
		o.config.table, o.config.indexName)
	if err != nil {
		return onlinePrimaryKeyInspection{}, fmt.Errorf("inspect %s migration index: %w", o.config.table, err)
	}
	if len(indexRows) > 1 {
		return o.invalid(fmt.Sprintf("Expected at most one %s migration index, found %d",
			o.config.table, len(indexRows))), nil
	}
	indexPresent := len(indexRows) == 1
	indexValid := false
	if indexPresent {
		columns, columnsErr := indexRows[0].String("column_names")
		unique, uniqueErr := indexRows[0].Bool("is_unique")
		valid, validErr := indexRows[0].Bool("is_valid")
		ready, readyErr := indexRows[0].Bool("is_ready")
		if err := errorsJoin(columnsErr, uniqueErr, validErr, readyErr); err != nil {
			return onlinePrimaryKeyInspection{}, err
		}
		if columns != strings.Join(o.config.targetColumns, ",") || !unique {
			return o.invalid(fmt.Sprintf("Relation name %s is used by an unexpected index",
				o.config.indexName)), nil
		}
		indexValid = valid && ready
	}

	legacyIndexPresent := false
	legacyIndexValid := false
	if o.preserveLegacyUniqueness {
		legacyIndexRows, err := access.Query(ctx, o.config.definition.StoreID, o.legacyIndexQuery,
			o.config.table, legacyPrimaryKeyIndexName(o.config.table))
		if err != nil {
			return onlinePrimaryKeyInspection{}, fmt.Errorf("inspect %s legacy compatibility index: %w",
				o.config.table, err)
		}
		if len(legacyIndexRows) > 1 {
			return o.invalid(fmt.Sprintf("Expected at most one %s legacy compatibility index, found %d",
				o.config.table, len(legacyIndexRows))), nil
		}
		legacyIndexPresent = len(legacyIndexRows) == 1
		if legacyIndexPresent {
			columns, columnsErr := legacyIndexRows[0].String("column_names")
			unique, uniqueErr := legacyIndexRows[0].Bool("is_unique")
			valid, validErr := legacyIndexRows[0].Bool("is_valid")
			ready, readyErr := legacyIndexRows[0].Bool("is_ready")
			if err := errorsJoin(columnsErr, uniqueErr, validErr, readyErr); err != nil {
				return onlinePrimaryKeyInspection{}, err
			}
			if columns != strings.Join(o.config.legacyColumns, ",") || !unique {
				return o.invalid(fmt.Sprintf("Relation name %s is used by an unexpected index",
					legacyPrimaryKeyIndexName(o.config.table))), nil
			}
			legacyIndexValid = valid && ready
		}
	}

	references, err := o.inspectReferences(ctx, access)
	if err != nil {
		return onlinePrimaryKeyInspection{}, err
	}

	switch primaryKeyColumns {
	case strings.Join(o.config.targetColumns, ","):
		if indexPresent {
			return o.invalid(fmt.Sprintf("Target primary key has leftover migration index %s",
				o.config.indexName)), nil
		}
		if legacyIndexPresent && !legacyIndexValid {
			return o.invalid(fmt.Sprintf("Target primary key has invalid legacy compatibility index %s",
				legacyPrimaryKeyIndexName(o.config.table))), nil
		}
		for index, reference := range references {
			if reference.legacyConstraintName != "" || !reference.targetValid {
				return o.invalid(fmt.Sprintf("Unexpected %s foreign-key state after primary-key cutover",
					o.config.references[index].table)), nil
			}
		}
		return onlinePrimaryKeyInspection{
			state: engine.VerificationTarget, summary: o.targetSummary(),
			legacyIndexPresent: legacyIndexPresent, legacyIndexValid: legacyIndexValid,
		}, nil
	case strings.Join(o.config.legacyColumns, ","):
		for index, reference := range references {
			if reference.legacyConstraintName == "" {
				return o.invalid(fmt.Sprintf("Expected %s legacy foreign key was not found",
					o.config.references[index].table)), nil
			}
		}
		return onlinePrimaryKeyInspection{
			state: engine.VerificationSource, summary: o.sourceSummary(),
			indexPresent: indexPresent, indexValid: indexValid,
			legacyIndexPresent: legacyIndexPresent, legacyIndexValid: legacyIndexValid,
			references: references,
		}, nil
	default:
		return o.invalid(fmt.Sprintf("Unexpected %s primary key columns %q",
			o.config.table, primaryKeyColumns)), nil
	}
}

func (o *onlinePrimaryKeyOperation) sourceSummary() string {
	if o.config.sourceSummary != "" {
		return o.config.sourceSummary
	}
	return fmt.Sprintf("%s is ready for the deployment-scoped primary key", o.config.table)
}

func (o *onlinePrimaryKeyOperation) targetSummary() string {
	if o.config.targetSummary != "" {
		return o.config.targetSummary
	}
	return fmt.Sprintf("%s uses the deployment-scoped primary key", o.config.table)
}

func (o *onlinePrimaryKeyOperation) findingCode() string {
	if o.config.findingCode != "" {
		return o.config.findingCode
	}
	return "unexpected_primary_key_schema"
}

func (o *onlinePrimaryKeyOperation) remediation() string {
	if o.config.remediation != "" {
		return o.config.remediation
	}
	return "Restore the expected source primary key or reconcile the table before retrying"
}

func (o *onlinePrimaryKeyOperation) inspectReferences(
	ctx context.Context,
	access engine.ReadAccess,
) ([]primaryKeyReferenceInspection, error) {
	inspections := make([]primaryKeyReferenceInspection, len(o.config.references))
	for index, reference := range o.config.references {
		rows, err := access.Query(ctx, o.config.definition.StoreID, o.referenceQueries[index].inspect,
			reference.table, o.config.table)
		if err != nil {
			return nil, fmt.Errorf("inspect %s foreign keys to %s: %w", reference.table, o.config.table, err)
		}
		for _, row := range rows {
			name, nameErr := row.String("constraint_name")
			valid, validErr := row.Bool("is_valid")
			childColumns, childErr := row.String("child_columns")
			parentColumns, parentErr := row.String("parent_columns")
			deleteAction, deleteErr := row.String("delete_action")
			updateAction, updateErr := row.String("update_action")
			if err := errorsJoin(nameErr, validErr, childErr, parentErr, deleteErr, updateErr); err != nil {
				return nil, err
			}
			if deleteAction != mustPrimaryKeyReferenceActionCode(reference.onDelete) ||
				updateAction != mustPrimaryKeyReferenceActionCode(reference.onUpdate) {
				continue
			}
			switch {
			case childColumns == strings.Join(reference.legacyColumns, ",") &&
				parentColumns == strings.Join(o.config.legacyColumns, ","):
				if inspections[index].legacyConstraintName != "" {
					return nil, fmt.Errorf("inspect %s foreign keys: multiple legacy constraints found",
						reference.table)
				}
				inspections[index].legacyConstraintName = name
			case childColumns == strings.Join(reference.targetColumns, ",") &&
				parentColumns == strings.Join(o.config.targetColumns, ","):
				if inspections[index].targetPresent {
					return nil, fmt.Errorf("inspect %s foreign keys: multiple deployment-scoped constraints found",
						reference.table)
				}
				inspections[index].targetConstraintName = name
				inspections[index].targetPresent = true
				inspections[index].targetValid = valid
			}
		}
	}
	return inspections, nil
}

func (o *onlinePrimaryKeyOperation) invalid(summary string) onlinePrimaryKeyInspection {
	return onlinePrimaryKeyInspection{state: engine.VerificationInvalid, summary: summary}
}

func quotePrimaryKeyIdentifier(identifier string) string {
	return `"` + identifier + `"`
}

func legacyPrimaryKeyIndexName(table string) string {
	return "idx_" + strings.ToLower(table) + "_legacy_primary_key_unique"
}

func quoteDiscoveredPrimaryKeyIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func quotePrimaryKeyIdentifiers(identifiers []string) string {
	quoted := make([]string, len(identifiers))
	for index, identifier := range identifiers {
		quoted[index] = quotePrimaryKeyIdentifier(strings.ToLower(identifier))
	}
	return strings.Join(quoted, ", ")
}

func quotePrimaryKeyLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func boundedPrimaryKeyDDL(statement string) string {
	return fmt.Sprintf(`DO $migration$
BEGIN
	PERFORM set_config('lock_timeout', '2s', true);
	EXECUTE %s;
END;
$migration$;`, quotePrimaryKeyLiteral(statement))
}

func uniquePrimaryKeyColumns(groups ...[]string) []string {
	seen := make(map[string]struct{})
	columns := make([]string, 0)
	for _, group := range groups {
		for _, column := range group {
			if _, exists := seen[column]; exists {
				continue
			}
			seen[column] = struct{}{}
			columns = append(columns, column)
		}
	}
	return columns
}

func containsPrimaryKeyColumn(columns []string, target string) bool {
	for _, column := range columns {
		if column == target {
			return true
		}
	}
	return false
}

func primaryKeyTextArray(columns []string) string {
	return "{" + strings.Join(columns, ",") + "}"
}

func errorsJoin(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func primaryKeyReferenceActionCode(action string) (string, bool) {
	switch action {
	case "NO ACTION":
		return "a", true
	case "RESTRICT":
		return "r", true
	case "CASCADE":
		return "c", true
	default:
		return "", false
	}
}

func mustPrimaryKeyReferenceActionCode(action string) string {
	code, _ := primaryKeyReferenceActionCode(action)
	return code
}
