// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const migrateEntityTypeReferencesOperationID engine.OperationID = "2.0.0/entitydb/43_migrate_entity_type_references"

var (
	queryEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-08",
		PostgresQuery: `SELECT ID, CATEGORY, TYPE, DEPLOYMENT_ID FROM "ENTITY"
			WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryEntityTypeReferenceBatch = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-09",
		PostgresQuery: `SELECT ID, CATEGORY, TYPE, DEPLOYMENT_ID FROM "ENTITY"
			WHERE ($3 = '' OR DEPLOYMENT_ID = $3)
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ID > $2))
			ORDER BY DEPLOYMENT_ID, ID LIMIT 100`,
	}
	queryUpdateEntityTypeReference = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-10",
		PostgresQuery: `UPDATE "ENTITY" SET TYPE = $1
			WHERE ID = $2 AND DEPLOYMENT_ID = $3`,
	}
)

func newMigrateEntityTypeReferencesOperation() engine.Operation {
	return newEntityTypeReferenceOperation(entityTypeReferencesOperationConfig(
		migrateEntityTypeReferencesOperationID,
		"Replace entity type names with handles in entity records",
		entityTypeReferenceForward,
	))
}

func newRollbackEntityTypeReferencesOperation() engine.RollbackOperation {
	return newRollbackEntityTypeReferenceOperation(entityTypeReferencesOperationConfig(
		"2.0.0/entitydb/43_migrate_entity_type_references_rollback",
		"Restore entity type names in entity records",
		entityTypeReferenceRollback,
	), migrateEntityTypeReferencesOperationID)
}

func entityTypeReferencesOperationConfig(
	id engine.OperationID,
	description string,
	direction entityTypeReferenceDirection,
) entityTypeReferenceOperationConfig {
	return entityTypeReferenceOperationConfig{
		definition: engine.OperationDefinition{
			ID:               id,
			Description:      description,
			StoreID:          engine.StoreIDEntityDB,
			ReadStoreIDs:     []engine.StoreID{engine.StoreIDConfigDB},
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeResumable,
		},
		direction:     direction,
		dependency:    backfillEntityTypeHandleOperationID,
		inspectQuery:  queryEntityTypeReferences,
		batchQuery:    queryEntityTypeReferenceBatch,
		updateQuery:   queryUpdateEntityTypeReference,
		payloadColumn: "type",
		batchArgs:     simpleEntityTypeReferenceBatchArgs,
		readCursor:    simpleEntityTypeReferenceCursor,
		updateArgs: func(updated string, row engine.QueryRow) ([]interface{}, error) {
			id, err := row.String("id")
			if err != nil {
				return nil, err
			}
			deploymentID, err := row.String("deployment_id")
			if err != nil {
				return nil, err
			}
			return []interface{}{updated, id, deploymentID}, nil
		},
		transform: func(payload string, deploymentID string, row engine.QueryRow, mapping entityTypeReferenceMapping,
			direction entityTypeReferenceDirection,
		) (string, bool, error) {
			if payload == "" {
				return "", false, fmt.Errorf("ENTITY.TYPE is empty")
			}
			category, err := row.String("category")
			if err != nil {
				return "", false, err
			}
			return mapping.translate(deploymentID, category, payload, direction)
		},
		targetSummary: "Every entity record uses an entity-type handle",
		sourceSummary: "Some entity records still use entity-type names",
		findingCode:   "unexpected_entity_type_references",
	}
}
