// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type sessionIndexRename struct {
	table string
	from  string
	to    string
}

// The primary key constraints are renamed with their backing indexes. The new binary relies on the
// column lists of its ON CONFLICT targets, not on index names, so the rename is invisible to it.
var sessionIndexRenames = []sessionIndexRename{
	{sessionTable, "SSO_SESSION_pkey", "SESSION_pkey"},
	{sessionTable, "idx_sso_session_handle_id", "idx_session_handle_id"},
	{sessionTable, "idx_sso_session_flow_execution", "idx_session_flow_execution"},
	{sessionTable, "idx_sso_session_absolute_expires_at", "idx_session_absolute_expires_at"},
	{sessionParticipantTable, "SSO_SESSION_PARTICIPANT_pkey", "SESSION_PARTICIPANT_pkey"},
	{sessionParticipantTable, "idx_sso_session_participant_app", "idx_session_participant_app"},
}

// NewRenameSessionIndexesOperation returns the rename session indexes operation.
func NewRenameSessionIndexesOperation() (engine.Operation, error) {
	statements := []engine.SQLStatement{{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"}}
	for _, rename := range sessionIndexRenames {
		statements = append(statements, engine.SQLStatement{
			ID:    "rename-" + rename.to,
			Query: fmt.Sprintf(`ALTER INDEX "%s" RENAME TO "%s"`, rename.from, rename.to),
		})
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               renameSessionIndexesOperationID,
			Description:      "Rename the session indexes and primary keys to match the renamed tables",
			StoreID:          engine.StoreIDRuntimePersistent,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkRenameSessionIndexesReadiness,
		PreVerifier:      verifyRenameSessionIndexes,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: statements},
		},
		PostVerifier: verifyRenameSessionIndexes,
	})
}

func checkRenameSessionIndexesReadiness(
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
			"Renaming the session indexes depends on the session table rename",
			renameSessionTablesOperationID), nil
	}
	verification, err := verifyRenameSessionIndexes(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	return sessionReadiness(verification,
		"Restore the expected session index names before retrying"), nil
}

func verifyRenameSessionIndexes(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	const code = "unexpected_session_index_names"
	relations, err := inspectSessionRelations(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if !sessionTablesRenamed(relations) {
		return invalidSessionVerification(code, fmt.Sprintf(
			"The session tables are not renamed: %s", describeSessionRelations(relations))), nil
	}
	rows, err := access.Query(ctx, engine.StoreIDRuntimePersistent, querySessionIndexNames)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect session index names: %w", err)
	}
	present := make(map[string]bool, len(rows))
	for _, row := range rows {
		table, tableErr := row.String("table_name")
		index, indexErr := row.String("index_name")
		if err := errorsJoin(tableErr, indexErr); err != nil {
			return engine.VerificationResult{}, fmt.Errorf("inspect session index names: %w", err)
		}
		present[table+"."+index] = true
	}

	var legacy, renamed int
	var missing []string
	for _, rename := range sessionIndexRenames {
		switch {
		case present[rename.table+"."+rename.from] && !present[rename.table+"."+rename.to]:
			legacy++
		case present[rename.table+"."+rename.to] && !present[rename.table+"."+rename.from]:
			renamed++
		default:
			missing = append(missing, rename.from)
		}
	}
	switch {
	case renamed == len(sessionIndexRenames):
		return sessionVerification(engine.VerificationTarget, "The session indexes are renamed"), nil
	case legacy == len(sessionIndexRenames):
		return sessionVerification(engine.VerificationSource, "The session indexes are ready to be renamed"), nil
	default:
		sort.Strings(missing)
		return invalidSessionVerification(code, fmt.Sprintf(
			"The session indexes are partly renamed or missing: legacy=%d, renamed=%d, unrecognised=%s",
			legacy, renamed, strings.Join(missing, ","))), nil
	}
}
