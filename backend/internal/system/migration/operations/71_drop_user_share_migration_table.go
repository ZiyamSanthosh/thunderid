// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const dropUserShareMigrationTableOperationID engine.OperationID = "entitydb/" +
	"71_drop_user_share_migration_table"

// NewDropUserShareMigrationTableOperation drops the working table once every operation that reads it has
// completed. It is the last operation of the release.
func NewDropUserShareMigrationTableOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		exists, err := relationExists(ctx, access, engine.StoreIDEntityDB, userShareMigrationTableName)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if exists {
			return engine.VerificationResult{
				State: engine.VerificationSource, Summary: "The user share migration table is ready to be dropped",
			}, nil
		}
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "The user share migration table is absent",
		}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               dropUserShareMigrationTableOperationID,
			Description:      "Drop the working table that paired duplicate users with their original",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "drop-user-share-migration", Query: `DROP TABLE "USER_SHARE_MIGRATION"`},
			}},
		},
		PostVerifier: verify,
	})
}
