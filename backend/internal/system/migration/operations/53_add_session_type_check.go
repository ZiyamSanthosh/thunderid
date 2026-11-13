// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// The constraint is added NOT VALID so the statement does not scan the table under its lock. The
// new binary never writes TYPE, and the column default keeps every row it creates within the check.
const addSessionTypeCheckStatement = `ALTER TABLE "SESSION" ADD CONSTRAINT "SESSION_type_check"
	CHECK (TYPE IN ('SSO', 'DIRECT')) NOT VALID`

// NewAddSessionTypeCheckOperation returns the add session type check operation.
func NewAddSessionTypeCheckOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               addSessionTypeCheckOperationID,
			Description:      "Restrict the session type to SSO and DIRECT without scanning existing sessions",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkAddSessionTypeCheckReadiness,
		PreVerifier:      verifyAddSessionTypeCheck,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "add-session-type-check", Query: addSessionTypeCheckStatement},
			}},
		},
		PostVerifier: verifyAddSessionTypeCheck,
	})
}

func checkAddSessionTypeCheckReadiness(
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
			"The session type check depends on the session table rename",
			renameSessionTablesOperationID), nil
	}
	verification, err := verifyAddSessionTypeCheck(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	return sessionReadiness(verification,
		"Reconcile the SESSION.TYPE column and the SESSION_type_check constraint before retrying"), nil
}

func verifyAddSessionTypeCheck(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	const code = "unexpected_session_type_check"
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !sessionTablesRenamed(relations) {
		return invalidSessionVerification(code, fmt.Sprintf(
			"The session tables are not renamed: %s", describeSessionRelations(relations))), nil
	}
	column, err := inspectSessionTypeColumn(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !column.matchesSessionType() {
		return invalidSessionVerification(code, fmt.Sprintf(
			"SESSION.TYPE has an unexpected definition: present=%t, nullable=%t, type=%q, length=%d",
			column.present, column.nullable, column.dataType, column.maxLength)), nil
	}
	check, err := inspectSessionTypeCheck(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	switch {
	case !check.present:
		return sessionVerification(engine.VerificationSource,
			"The session type check is ready to be added"), nil
	case !check.valid:
		return invalidSessionVerification(code,
			"SESSION_type_check does not restrict TYPE to SSO and DIRECT"), nil
	default:
		return sessionVerification(engine.VerificationTarget, "The session type check is present"), nil
	}
}
