// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	_ "embed"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
)

const (
	metadataStateTable    = "MIGRATION_STATE"
	metadataProgressTable = "MIGRATION_PROGRESS"
)

// SchemaStatus describes whether the metadata tables exist in a database namespace.
type SchemaStatus string

const (
	// SchemaAbsent means neither metadata table exists.
	SchemaAbsent SchemaStatus = "absent"
	// SchemaReady means both metadata tables exist.
	SchemaReady SchemaStatus = "ready"
	// SchemaPartial means exactly one metadata table exists.
	SchemaPartial SchemaStatus = "partial"
)

//go:embed postgres_metadata.sql
var postgresMetadataSchema string

var (
	queryInspectMetadataSchema = dbmodel.DBQuery{
		ID: "MIG-METADATA_MGT-07",
		PostgresQuery: `SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
            WHERE TABLE_SCHEMA = CURRENT_SCHEMA()
            AND TABLE_NAME IN ('MIGRATION_STATE', 'MIGRATION_PROGRESS')`,
	}
	queryCreateMetadataSchema = dbmodel.DBQuery{
		ID:            "MIG-METADATA_MGT-08",
		PostgresQuery: postgresMetadataSchema,
	}
)

// InspectSchema reports the presence of the two migration metadata tables.
func (r *MetadataStore) InspectSchema(ctx context.Context) (SchemaStatus, error) {
	rows, err := r.dbClient.QueryContext(ctx, r.queries.inspectSchema)
	if err != nil {
		return "", fmt.Errorf("inspect migration metadata schema: %w", err)
	}

	found := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		tableName, err := requiredString(row, "table_name")
		if err != nil {
			return "", fmt.Errorf("inspect migration metadata schema: %w", err)
		}
		found[tableName] = struct{}{}
	}

	_, hasState := found[metadataStateTable]
	_, hasProgress := found[metadataProgressTable]
	switch {
	case !hasState && !hasProgress:
		return SchemaAbsent, nil
	case hasState && hasProgress:
		return SchemaReady, nil
	default:
		return SchemaPartial, nil
	}
}

// CreateSchema creates both migration metadata tables using the configured database implementation.
func (r *MetadataStore) CreateSchema(ctx context.Context) error {
	if _, err := r.dbClient.ExecuteContext(ctx, r.queries.createSchema); err != nil {
		return fmt.Errorf("create migration metadata schema: %w", err)
	}
	return nil
}
