// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl // Mirrors the sibling reference operation; only the column it rewrites differs.
package operations

import (
	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const migrateIDPEntityTypeReferencesOperationID engine.OperationID = "configdb/" +
	"44_migrate_idp_entity_type_references"

var (
	queryIDPEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-27",
		PostgresQuery: `SELECT ID, ATTRIBUTE_CONFIGURATION, DEPLOYMENT_ID FROM "IDP"
			WHERE ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryIDPEntityTypeReferenceBatch = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-28",
		PostgresQuery: `SELECT ID, ATTRIBUTE_CONFIGURATION, DEPLOYMENT_ID FROM "IDP"
			WHERE ($3 = '' OR DEPLOYMENT_ID = $3)
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ID > $2))
			ORDER BY DEPLOYMENT_ID, ID LIMIT 100`,
	}
	queryUpdateIDPEntityTypeReferences = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-29",
		PostgresQuery: `UPDATE "IDP" SET ATTRIBUTE_CONFIGURATION = $1
			WHERE ID = $2 AND ATTRIBUTE_CONFIGURATION = $3 AND DEPLOYMENT_ID = $4`,
	}
)

// NewMigrateIDPEntityTypeReferencesOperation returns the migrate idp entity type references operation.
func NewMigrateIDPEntityTypeReferencesOperation(
	declared *declarativeEntityTypeSource,
) engine.Operation {
	return newEntityTypeReferenceOperation(withDeclaredEntityTypes(
		declared, idpEntityTypeReferencesOperationConfig(
			migrateIDPEntityTypeReferencesOperationID,
			"Replace entity type names with handles in identity provider configuration",
			entityTypeReferenceForward,
		)))
}

// NewRollbackIDPEntityTypeReferencesOperation returns the rollback operation that undoes idp entity type references.
func NewRollbackIDPEntityTypeReferencesOperation(
	declared *declarativeEntityTypeSource,
) engine.RollbackOperation {
	return newRollbackEntityTypeReferenceOperation(withDeclaredEntityTypes(
		declared, idpEntityTypeReferencesOperationConfig(
			"configdb/44_migrate_idp_entity_type_references_rollback",
			"Restore entity type names in identity provider configuration",
			entityTypeReferenceRollback,
		)), migrateIDPEntityTypeReferencesOperationID)
}

func idpEntityTypeReferencesOperationConfig(
	id engine.OperationID,
	description string,
	direction entityTypeReferenceDirection,
) entityTypeReferenceOperationConfig {
	return jsonEntityTypeReferencesOperationConfig(jsonEntityTypeReferencesConfig{
		id:              id,
		description:     description,
		direction:       direction,
		inspectQuery:    queryIDPEntityTypeReferences,
		batchQuery:      queryIDPEntityTypeReferenceBatch,
		updateQuery:     queryUpdateIDPEntityTypeReferences,
		payloadColumn:   "attribute_configuration",
		payloadOptional: true,
		transform:       transformIDPEntityTypeReferences,
		targetSummary:   "Every identity provider uses entity-type handles",
		sourceSummary:   "Some identity providers still use entity-type names",
		findingCode:     "unexpected_idp_entity_type_references",
	})
}
