// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type jsonEntityTypeReferencesConfig struct {
	id              engine.OperationID
	description     string
	direction       entityTypeReferenceDirection
	inspectQuery    dbmodel.DBQuery
	batchQuery      dbmodel.DBQuery
	updateQuery     dbmodel.DBQuery
	payloadColumn   string
	payloadOptional bool
	transform       func(string, string, entityTypeReferenceMapping, entityTypeReferenceDirection) (string, bool, error)
	targetSummary   string
	sourceSummary   string
	findingCode     string
}

func jsonEntityTypeReferencesOperationConfig(config jsonEntityTypeReferencesConfig) entityTypeReferenceOperationConfig {
	return entityTypeReferenceOperationConfig{
		definition: engine.OperationDefinition{
			ID:               config.id,
			Description:      config.description,
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeResumable,
		},
		direction:       config.direction,
		dependency:      backfillEntityTypeHandleOperationID,
		inspectQuery:    config.inspectQuery,
		batchQuery:      config.batchQuery,
		updateQuery:     config.updateQuery,
		payloadColumn:   config.payloadColumn,
		payloadOptional: config.payloadOptional,
		batchArgs:       simpleEntityTypeReferenceBatchArgs,
		readCursor:      simpleEntityTypeReferenceCursor,
		updateArgs: func(updated string, original string, row engine.QueryRow) ([]interface{}, error) {
			id, err := row.String("id")
			if err != nil {
				return nil, err
			}
			deploymentID, err := row.String("deployment_id")
			if err != nil {
				return nil, err
			}
			return []interface{}{updated, id, original, deploymentID}, nil
		},
		transform: func(payload string, deploymentID string, _ engine.QueryRow,
			mapping entityTypeReferenceMapping, direction entityTypeReferenceDirection,
		) (string, bool, error) {
			return config.transform(payload, deploymentID, mapping, direction)
		},
		targetSummary: config.targetSummary,
		sourceSummary: config.sourceSummary,
		findingCode:   config.findingCode,
	}
}
