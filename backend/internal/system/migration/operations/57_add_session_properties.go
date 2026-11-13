// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const addSessionPropertiesOperationID engine.OperationID = "runtime_persistent/57_add_session_properties"

var sessionPropertiesColumn = columnSpec{"properties", "YES", "jsonb", "none", "none"}

// NewAddSessionPropertiesOperation adds the nullable column that holds what a session records about its device
// and address. The previous release reads and writes sessions through the compatibility views, which list the
// columns it knows, so it never sees the column.
func NewAddSessionPropertiesOperation() (engine.Operation, error) {
	spec := addColumnSpec{
		id:          addSessionPropertiesOperationID,
		description: "Add the properties a session records about its device and address",
		storeID:     engine.StoreIDRuntimePersistent,
		table:       sessionTable,
		columns:     []columnSpec{sessionPropertiesColumn},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-properties", Query: `ALTER TABLE "SESSION" ADD COLUMN PROPERTIES JSONB`},
		},
	}
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		relations, err := inspectSessionRelations(ctx, access)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if !sessionTablesRenamed(relations) {
			return invalidSessionVerification("unexpected_session_table_schema", fmt.Sprintf(
				"The session tables are not renamed: %s", describeSessionRelations(relations))), nil
		}
		return verifyAddedColumns(ctx, access, spec)
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID: spec.id, Description: spec.description, StoreID: spec.storeID,
			Kind: engine.OperationKindSchemaChange, InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			relations, err := inspectSessionRelations(ctx, access)
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			if !sessionTablesRenamed(relations) && relations[legacySessionTable] == relationKindTable {
				return deferredSessionReadiness(
					"The session properties depend on the session table rename", renameSessionTablesOperationID), nil
			}
			verification, err := verify(ctx, access, engine.ExecutionTarget{})
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			return sessionReadiness(verification,
				"Reconcile the SESSION.PROPERTIES column before retrying"), nil
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: spec.statements},
		},
		PostVerifier: verify,
	})
}
