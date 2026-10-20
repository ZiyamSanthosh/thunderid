// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const migrateFlowEntityTypeReferencesOperationID engine.OperationID = "2.0.0/configdb/47_migrate_flow_entity_type_references"

var (
	queryFlowEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-33",
		PostgresQuery: `SELECT FLOW_ID AS ID, VERSION, NODES, DEPLOYMENT_ID FROM "FLOW_VERSION"
			WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, FLOW_ID, VERSION`,
	}
	queryFlowEntityTypeReferenceBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-34",
		PostgresQuery: `SELECT FLOW_ID AS ID, VERSION, NODES, DEPLOYMENT_ID FROM "FLOW_VERSION"
			WHERE ($4 = '' OR DEPLOYMENT_ID = $4)
			AND (DEPLOYMENT_ID > $1
				OR (DEPLOYMENT_ID = $1 AND FLOW_ID > $2)
				OR (DEPLOYMENT_ID = $1 AND FLOW_ID = $2 AND VERSION > $3))
			ORDER BY DEPLOYMENT_ID, FLOW_ID, VERSION LIMIT 100`,
	}
	queryUpdateFlowEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-35",
		PostgresQuery: `UPDATE "FLOW_VERSION" SET NODES = $1
			WHERE FLOW_ID = $2 AND VERSION = $3 AND NODES = $4 AND DEPLOYMENT_ID = $5`,
	}
)

func newMigrateFlowEntityTypeReferencesOperation(
	declared *declarativeEntityTypeSource,
) engine.Operation {
	return newEntityTypeReferenceOperation(withDeclaredEntityTypes(
		declared, flowEntityTypeReferencesOperationConfig(
			migrateFlowEntityTypeReferencesOperationID,
			"Replace entity type names with handles in every flow version",
			entityTypeReferenceForward,
		)))
}

func newRollbackFlowEntityTypeReferencesOperation(
	declared *declarativeEntityTypeSource,
) engine.RollbackOperation {
	return newRollbackEntityTypeReferenceOperation(withDeclaredEntityTypes(
		declared, flowEntityTypeReferencesOperationConfig(
			"2.0.0/configdb/47_migrate_flow_entity_type_references_rollback",
			"Restore entity type names in every flow version",
			entityTypeReferenceRollback,
		)), migrateFlowEntityTypeReferencesOperationID)
}

func flowEntityTypeReferencesOperationConfig(
	id engine.OperationID,
	description string,
	direction entityTypeReferenceDirection,
) entityTypeReferenceOperationConfig {
	return entityTypeReferenceOperationConfig{
		definition: engine.OperationDefinition{
			ID:               id,
			Description:      description,
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeResumable,
		},
		direction:     direction,
		dependency:    backfillEntityTypeHandleOperationID,
		inspectQuery:  queryFlowEntityTypeReferences,
		batchQuery:    queryFlowEntityTypeReferenceBatch,
		updateQuery:   queryUpdateFlowEntityTypeReferences,
		payloadColumn: "nodes",
		batchArgs:     flowEntityTypeReferenceBatchArgs,
		readCursor:    flowEntityTypeReferenceCursor,
		updateArgs: func(updated string, original string, row engine.QueryRow) ([]interface{}, error) {
			id, err := row.String("id")
			if err != nil {
				return nil, err
			}
			version, err := row.Int64("version")
			if err != nil {
				return nil, err
			}
			deploymentID, err := row.String("deployment_id")
			if err != nil {
				return nil, err
			}
			return []interface{}{updated, id, version, original, deploymentID}, nil
		},
		transform: func(payload string, deploymentID string, _ engine.QueryRow,
			mapping entityTypeReferenceMapping, direction entityTypeReferenceDirection,
		) (string, bool, error) {
			return transformFlowEntityTypeReferences(payload, deploymentID, mapping, direction)
		},
		targetSummary: "Every flow version uses entity-type handles",
		sourceSummary: "Some flow versions still use entity-type names",
		findingCode:   "unexpected_flow_entity_type_references",
	}
}
