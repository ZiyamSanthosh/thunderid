// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const migrateAuthorizationPDPEntityTypeReferencesOperationID engine.OperationID = "2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references"

var (
	queryAuthorizationPDPEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-30",
		PostgresQuery: `SELECT ID, PROPERTIES, DEPLOYMENT_ID FROM "AUTHORIZATION_PDP_CONNECTION"
			WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryAuthorizationPDPEntityTypeReferenceBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-31",
		PostgresQuery: `SELECT ID, PROPERTIES, DEPLOYMENT_ID FROM "AUTHORIZATION_PDP_CONNECTION"
			WHERE ($3 = '' OR DEPLOYMENT_ID = $3)
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ID > $2))
			ORDER BY DEPLOYMENT_ID, ID LIMIT 100`,
	}
	queryUpdateAuthorizationPDPEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-32",
		PostgresQuery: `UPDATE "AUTHORIZATION_PDP_CONNECTION" SET PROPERTIES = $1
			WHERE ID = $2 AND DEPLOYMENT_ID = $3`,
	}
)

func newMigrateAuthorizationPDPEntityTypeReferencesOperation() engine.Operation {
	return newEntityTypeReferenceOperation(authorizationPDPEntityTypeReferencesOperationConfig(
		migrateAuthorizationPDPEntityTypeReferencesOperationID,
		"Replace entity type names with handles in authorization PDP configuration",
		entityTypeReferenceForward,
	))
}

func newRollbackAuthorizationPDPEntityTypeReferencesOperation() engine.RollbackOperation {
	return newRollbackEntityTypeReferenceOperation(authorizationPDPEntityTypeReferencesOperationConfig(
		"2.0.0/configdb/46_migrate_authorization_pdp_entity_type_references_rollback",
		"Restore entity type names in authorization PDP configuration",
		entityTypeReferenceRollback,
	), migrateAuthorizationPDPEntityTypeReferencesOperationID)
}

func authorizationPDPEntityTypeReferencesOperationConfig(
	id engine.OperationID,
	description string,
	direction entityTypeReferenceDirection,
) entityTypeReferenceOperationConfig {
	return jsonEntityTypeReferencesOperationConfig(jsonEntityTypeReferencesConfig{
		id:              id,
		description:     description,
		direction:       direction,
		inspectQuery:    queryAuthorizationPDPEntityTypeReferences,
		batchQuery:      queryAuthorizationPDPEntityTypeReferenceBatch,
		updateQuery:     queryUpdateAuthorizationPDPEntityTypeReferences,
		payloadColumn:   "properties",
		payloadOptional: false,
		transform:       transformAuthorizationPDPEntityTypeReferences,
		targetSummary:   "Every authorization PDP connection uses entity-type handles",
		sourceSummary:   "Some authorization PDP connections still use entity-type names",
		findingCode:     "unexpected_authorization_pdp_entity_type_references",
	})
}
