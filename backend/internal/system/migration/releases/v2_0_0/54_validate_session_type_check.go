// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// VALIDATE CONSTRAINT scans the table but does not block reads or writes, and running it on a validated
// constraint does nothing, so a repeated run is safe.
const validateSessionTypeCheckStatement = `ALTER TABLE "SESSION" VALIDATE CONSTRAINT "SESSION_type_check"`

func newValidateSessionTypeCheckOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               validateSessionTypeCheckOperationID,
			Description:      "Validate the session type check against existing sessions",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeOnline,
		},
		ReadinessChecker: checkValidateSessionTypeCheckReadiness,
		PreVerifier:      verifyValidateSessionTypeCheck,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "validate-session-type-check", Query: validateSessionTypeCheckStatement},
			}},
		},
		PostVerifier: verifyValidateSessionTypeCheck,
	})
}

func checkValidateSessionTypeCheckReadiness(
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
			"Validating the session type check depends on the session table rename and the check itself",
			renameSessionTablesOperationID, addSessionTypeCheckOperationID), nil
	}
	check, err := inspectSessionTypeCheck(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if sessionTablesRenamed(relations) && !check.present {
		return deferredSessionReadiness(
			"Validating the session type check depends on the operation that adds it",
			addSessionTypeCheckOperationID), nil
	}
	verification, err := verifyValidateSessionTypeCheck(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	return sessionReadiness(verification,
		"Reconcile the SESSION_type_check constraint before retrying"), nil
}

func verifyValidateSessionTypeCheck(
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
	check, err := inspectSessionTypeCheck(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	switch {
	case !check.present:
		return invalidSessionVerification(code, "SESSION_type_check is absent"), nil
	case !check.valid:
		return invalidSessionVerification(code,
			"SESSION_type_check does not restrict TYPE to SSO and DIRECT"), nil
	case check.validated:
		return sessionVerification(engine.VerificationTarget, "The session type check is validated"), nil
	default:
		return sessionVerification(engine.VerificationSource,
			"The session type check is ready to be validated"), nil
	}
}
