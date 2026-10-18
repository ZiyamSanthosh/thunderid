// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const migrateInboundClientEntityTypeReferencesOperationID engine.OperationID = "2.0.0/configdb/44_migrate_inbound_client_entity_type_references"

var (
	queryInboundClientEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-24",
		PostgresQuery: `SELECT ENTITY_ID AS ID, PROPERTIES, DEPLOYMENT_ID FROM "INBOUND_CLIENT"
			WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ENTITY_ID`,
	}
	queryInboundClientEntityTypeReferenceBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-25",
		PostgresQuery: `SELECT ENTITY_ID AS ID, PROPERTIES, DEPLOYMENT_ID FROM "INBOUND_CLIENT"
			WHERE ($3 = '' OR DEPLOYMENT_ID = $3)
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ENTITY_ID > $2))
			ORDER BY DEPLOYMENT_ID, ENTITY_ID LIMIT 100`,
	}
	queryUpdateInboundClientEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-26",
		PostgresQuery: `UPDATE "INBOUND_CLIENT" SET PROPERTIES = $1
			WHERE ENTITY_ID = $2 AND DEPLOYMENT_ID = $3`,
	}
)

func newMigrateInboundClientEntityTypeReferencesOperation() engine.Operation {
	return newEntityTypeReferenceOperation(inboundClientEntityTypeReferencesOperationConfig(
		migrateInboundClientEntityTypeReferencesOperationID,
		"Replace entity type names with handles in inbound client configuration",
		entityTypeReferenceForward,
	))
}

func newRollbackInboundClientEntityTypeReferencesOperation() engine.RollbackOperation {
	return newRollbackEntityTypeReferenceOperation(inboundClientEntityTypeReferencesOperationConfig(
		"2.0.0/configdb/44_migrate_inbound_client_entity_type_references_rollback",
		"Restore entity type names in inbound client configuration",
		entityTypeReferenceRollback,
	), migrateInboundClientEntityTypeReferencesOperationID)
}

func inboundClientEntityTypeReferencesOperationConfig(
	id engine.OperationID,
	description string,
	direction entityTypeReferenceDirection,
) entityTypeReferenceOperationConfig {
	return jsonEntityTypeReferencesOperationConfig(jsonEntityTypeReferencesConfig{
		id:              id,
		description:     description,
		direction:       direction,
		inspectQuery:    queryInboundClientEntityTypeReferences,
		batchQuery:      queryInboundClientEntityTypeReferenceBatch,
		updateQuery:     queryUpdateInboundClientEntityTypeReferences,
		payloadColumn:   "properties",
		payloadOptional: true,
		transform:       transformInboundClientEntityTypeReferences,
		targetSummary:   "Every inbound client uses entity-type handles",
		sourceSummary:   "Some inbound clients still use entity-type names",
		findingCode:     "unexpected_inbound_client_entity_type_references",
	})
}
