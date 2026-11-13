// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	dropGroupMemberLookupIndexOperationID engine.OperationID = "entitydb/96_drop_group_member_lookup_index"

	groupMemberLookupIndex = "idx_group_member_lookup"
)

// NewDropGroupMemberLookupIndexOperation removes the member lookup index the previous release created on
// GROUP_MEMBER_REFERENCE and the current schema no longer has. The previous release may still use it, so it is
// dropped in contract, after that release is retired.
func NewDropGroupMemberLookupIndexOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryOnlineIndex, groupMemberLookupIndex)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("inspect index %s: %w", groupMemberLookupIndex, err)
		}
		if len(rows) == 0 {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "The group member lookup index is removed"}, nil
		}
		table, err := rows[0].String("table_name")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if table != "GROUP_MEMBER_REFERENCE" {
			return invalidVerification("unexpected_index_definition", fmt.Sprintf(
				"Index %s belongs to %s, not GROUP_MEMBER_REFERENCE", groupMemberLookupIndex, table)), nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: "The group member lookup index is ready to be removed"}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               dropGroupMemberLookupIndexOperationID,
			Description:      "Remove the group member lookup index the current schema no longer has",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			return readinessFromVerification(ctx, access,
				func(ctx context.Context, access engine.ReadAccess, _ string) (engine.VerificationResult, error) {
					return verify(ctx, access, engine.ExecutionTarget{})
				})
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "drop-group-member-lookup-index", Query: `DROP INDEX "idx_group_member_lookup"`},
			}},
		},
		PostVerifier: verify,
	})
}
