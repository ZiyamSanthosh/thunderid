// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type sessionSubjectIndexOperation struct {
	definition       engine.OperationDefinition
	createIndexQuery dbmodel.DBQuery
	dropIndexQuery   dbmodel.DBQuery
}

type sessionSubjectIndexInspection struct {
	present bool
	valid   bool
}

func newAddSessionSubjectIndexOperation() (engine.Operation, error) {
	return &sessionSubjectIndexOperation{
		definition: engine.OperationDefinition{
			ID:               addSessionSubjectIndexOperationID,
			Description:      "Add the deployment-scoped session subject lookup index",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		createIndexQuery: dbmodel.DBQuery{
			ID: "MIG-RTP_MGT-18",
			PostgresQuery: `CREATE INDEX CONCURRENTLY "idx_session_subject"
				ON "SESSION" (DEPLOYMENT_ID, SUBJECT_ID)`,
		},
		dropIndexQuery: dbmodel.DBQuery{
			ID:            "MIG-RTP_MGT-19",
			PostgresQuery: `DROP INDEX CONCURRENTLY "idx_session_subject"`,
		},
	}, nil
}

func (o *sessionSubjectIndexOperation) Definition() engine.OperationDefinition {
	return o.definition
}

func (o *sessionSubjectIndexOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !sessionTablesRenamed(relations) && relations[legacySessionTable] == relationKindTable {
		return deferredSessionReadiness(
			"The session subject index depends on the session table rename",
			renameSessionTablesOperationID), nil
	}
	verification, err := verifySessionSubjectIndex(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	return sessionReadiness(verification,
		"Restore the expected SESSION columns or remove the conflicting idx_session_subject index"), nil
}

func (*sessionSubjectIndexOperation) ResolveTargets(
	context.Context,
	engine.ReadAccess,
	engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return []engine.ExecutionTarget{{}}, nil
}

func (*sessionSubjectIndexOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifySessionSubjectIndex(ctx, access, target)
}

func (o *sessionSubjectIndexOperation) Execute(
	ctx context.Context,
	execution engine.OperationExecution,
) error {
	verification, err := verifySessionSubjectIndex(ctx, execution.Access(), execution.Target())
	if err != nil {
		return err
	}
	if verification.State == engine.VerificationTarget {
		return nil
	}
	if verification.State != engine.VerificationSource {
		return fmt.Errorf("prepare session subject index: %s", verification.Summary)
	}
	inspection, invalidSummary, err := inspectSessionSubjectIndex(ctx, execution.Access())
	if err != nil {
		return err
	}
	if invalidSummary != "" {
		return fmt.Errorf("prepare session subject index: %s", invalidSummary)
	}
	if inspection.present {
		if inspection.valid {
			return nil
		}
		if _, err := execution.Access().Execute(
			ctx, engine.StoreIDRuntimePersistent, o.dropIndexQuery); err != nil {
			return fmt.Errorf("drop invalid session subject index: %w", err)
		}
	}
	if _, err := execution.Access().Execute(
		ctx, engine.StoreIDRuntimePersistent, o.createIndexQuery); err != nil {
		return fmt.Errorf("create session subject index concurrently: %w", err)
	}
	return nil
}

func (*sessionSubjectIndexOperation) Finalize(
	context.Context,
	engine.OperationExecution,
) error {
	return nil
}

func (*sessionSubjectIndexOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return verifySessionSubjectIndex(ctx, access, target)
}

func verifySessionSubjectIndex(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	const code = "unexpected_session_subject_index"
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !sessionTablesRenamed(relations) {
		return invalidSessionVerification(code, fmt.Sprintf(
			"The session tables are not renamed: %s", describeSessionRelations(relations))), nil
	}
	columnRows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionSubjectColumns)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect SESSION subject lookup columns: %w", err)
	}
	if len(columnRows) != 2 {
		return invalidSessionVerification(code, fmt.Sprintf(
			"Expected two SESSION subject lookup columns, found %d", len(columnRows))), nil
	}
	expectedColumns := []string{"DEPLOYMENT_ID", "SUBJECT_ID"}
	for index, row := range columnRows {
		columnName, columnNameErr := row.String("column_name")
		nullable, nullableErr := row.String("is_nullable")
		if err := errorsJoin(columnNameErr, nullableErr); err != nil {
			return engine.VerificationResult{}, err
		}
		if columnName != expectedColumns[index] || nullable != "NO" {
			return invalidSessionVerification(code, fmt.Sprintf(
				"Unexpected SESSION subject lookup column: name=%q, nullable=%q", columnName, nullable)), nil
		}
	}

	inspection, invalidSummary, err := inspectSessionSubjectIndex(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if invalidSummary != "" {
		return invalidSessionVerification(code, invalidSummary), nil
	}
	switch {
	case inspection.present && inspection.valid:
		return sessionVerification(engine.VerificationTarget,
			"The session subject lookup index is present and valid"), nil
	case inspection.present:
		return sessionVerification(engine.VerificationSource,
			"The interrupted session subject lookup index is ready to be rebuilt"), nil
	default:
		return sessionVerification(engine.VerificationSource,
			"The session subject lookup index is ready to be created"), nil
	}
}

func inspectSessionSubjectIndex(
	ctx context.Context,
	access engine.ReadAccess,
) (sessionSubjectIndexInspection, string, error) {
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionSubjectIndex)
	if err != nil {
		return sessionSubjectIndexInspection{}, "", fmt.Errorf("inspect session subject index: %w", err)
	}
	if len(rows) == 0 {
		return sessionSubjectIndexInspection{}, "", nil
	}
	if len(rows) != 1 {
		return sessionSubjectIndexInspection{}, fmt.Sprintf(
			"Expected at most one session subject index, found %d", len(rows)), nil
	}
	isUnique, uniqueErr := rows[0].Bool("is_unique")
	isValid, validErr := rows[0].Bool("is_valid")
	isReady, readyErr := rows[0].Bool("is_ready")
	hasPredicate, predicateErr := rows[0].Bool("has_predicate")
	accessMethod, accessMethodErr := rows[0].String("access_method")
	columns, columnsErr := rows[0].String("column_names")
	if err := errorsJoin(uniqueErr, validErr, readyErr, predicateErr, accessMethodErr, columnsErr); err != nil {
		return sessionSubjectIndexInspection{}, "", err
	}
	if isUnique || hasPredicate || accessMethod != "btree" || columns != sessionSubjectIndexColumns {
		return sessionSubjectIndexInspection{}, fmt.Sprintf(
			"Unexpected idx_session_subject definition: unique=%t, predicate=%t, method=%q, columns=%q",
			isUnique, hasPredicate, accessMethod, columns), nil
	}
	return sessionSubjectIndexInspection{present: true, valid: isValid && isReady}, "", nil
}
