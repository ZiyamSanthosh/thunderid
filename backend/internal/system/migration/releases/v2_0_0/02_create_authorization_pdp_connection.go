// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const authorizationPDPConnectionIndex = "idx_authorization_pdp_connection_type_name_deployment"

var (
	postgresQueryAuthorizationPDPConnectionTable = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-01",
		PostgresQuery: `SELECT TABLE_NAME
            FROM INFORMATION_SCHEMA.TABLES
            WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'AUTHORIZATION_PDP_CONNECTION'`,
	}
	postgresQueryAuthorizationPDPConnectionColumns = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-02",
		PostgresQuery: `SELECT COLUMN_NAME, IS_NULLABLE, DATA_TYPE,
                COALESCE(CHARACTER_MAXIMUM_LENGTH::TEXT, 'none') AS MAXIMUM_LENGTH,
                CASE WHEN COLUMN_DEFAULT IS NULL THEN 'no' ELSE 'yes' END AS HAS_DEFAULT
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = CURRENT_SCHEMA() AND TABLE_NAME = 'AUTHORIZATION_PDP_CONNECTION'
            ORDER BY ORDINAL_POSITION`,
	}
	postgresQueryAuthorizationPDPConnectionPrimaryKey = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-03",
		PostgresQuery: `SELECT kcu.COLUMN_NAME, kcu.ORDINAL_POSITION
            FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
            INNER JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
                ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
                AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
                AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
                AND tc.TABLE_NAME = kcu.TABLE_NAME
            WHERE tc.TABLE_SCHEMA = CURRENT_SCHEMA()
                AND tc.TABLE_NAME = 'AUTHORIZATION_PDP_CONNECTION'
                AND tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
            ORDER BY kcu.ORDINAL_POSITION`,
	}
	postgresQueryAuthorizationPDPConnectionIndex = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-04",
		PostgresQuery: `SELECT i.INDISUNIQUE::TEXT AS IS_UNIQUE, a.ATTNAME AS COLUMN_NAME,
                key_position.ORDINALITY::BIGINT AS COLUMN_POSITION
            FROM pg_index i
            INNER JOIN pg_class table_class ON table_class.oid = i.indrelid
            INNER JOIN pg_namespace n ON n.oid = table_class.relnamespace
            INNER JOIN pg_class index_class ON index_class.oid = i.indexrelid
            CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_position(attnum, ordinality)
            INNER JOIN pg_attribute a ON a.attrelid = table_class.oid AND a.attnum = key_position.attnum
            WHERE n.nspname = CURRENT_SCHEMA()
                AND table_class.relname = 'AUTHORIZATION_PDP_CONNECTION'
                AND index_class.relname = 'idx_authorization_pdp_connection_type_name_deployment'
            ORDER BY key_position.ORDINALITY`,
	}
)

type authorizationPDPConnectionInspection struct {
	state   engine.VerificationState
	summary string
}

type authorizationPDPConnectionColumn struct {
	name          string
	nullable      string
	dataType      string
	maximumLength string
	hasDefault    string
}

var authorizationPDPConnectionColumns = []authorizationPDPConnectionColumn{
	{name: "deployment_id", nullable: "NO", dataType: "character varying", maximumLength: "255", hasDefault: "no"},
	{name: "id", nullable: "NO", dataType: "character varying", maximumLength: "36", hasDefault: "no"},
	{name: "name", nullable: "NO", dataType: "character varying", maximumLength: "255", hasDefault: "no"},
	{name: "description", nullable: "YES", dataType: "character varying", maximumLength: "500", hasDefault: "no"},
	{name: "type", nullable: "NO", dataType: "character varying", maximumLength: "50", hasDefault: "no"},
	{name: "properties", nullable: "NO", dataType: "jsonb", maximumLength: "none", hasDefault: "no"},
	{name: "created_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", hasDefault: "yes"},
	{name: "updated_at", nullable: "YES", dataType: "timestamp with time zone", maximumLength: "none", hasDefault: "yes"},
}

func newCreateAuthorizationPDPConnectionOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               "2.0.0/configdb/02_create_authorization_pdp_connection",
			Description:      "Create the authorization PDP connection table",
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		PreVerifier: func(
			ctx context.Context,
			access engine.ReadAccess,
			_ engine.ExecutionTarget,
		) (engine.VerificationResult, error) {
			return verifyAuthorizationPDPConnection(ctx, access)
		},
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "create-table", Query: postgresCreateAuthorizationPDPConnectionTableSQL},
					{ID: "create-index", Query: postgresCreateAuthorizationPDPConnectionIndexSQL},
				},
			},
		},
		PostVerifier: func(
			ctx context.Context,
			access engine.ReadAccess,
			_ engine.ExecutionTarget,
		) (engine.VerificationResult, error) {
			return verifyAuthorizationPDPConnection(ctx, access)
		},
	})
}

func verifyAuthorizationPDPConnection(
	ctx context.Context,
	access engine.ReadAccess,
) (engine.VerificationResult, error) {
	inspection, err := inspectAuthorizationPDPConnection(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	result := engine.VerificationResult{State: inspection.state, Summary: inspection.summary}
	if inspection.state == engine.VerificationInvalid {
		result.Findings = []engine.Finding{{
			Code: "unexpected_authorization_pdp_connection_schema", Message: inspection.summary,
		}}
	}
	return result, nil
}

func inspectAuthorizationPDPConnection(
	ctx context.Context,
	access engine.ReadAccess,
) (authorizationPDPConnectionInspection, error) {
	tableRows, err := access.Query(ctx, engine.StoreIDConfigDB, postgresQueryAuthorizationPDPConnectionTable)
	if err != nil {
		return authorizationPDPConnectionInspection{}, fmt.Errorf("inspect authorization PDP connection table: %w", err)
	}
	if len(tableRows) == 0 {
		return authorizationPDPConnectionInspection{
			state: engine.VerificationSource, summary: "The authorization PDP connection table is absent",
		}, nil
	}
	if len(tableRows) != 1 {
		return invalidAuthorizationPDPConnection(
			fmt.Sprintf("Expected one AUTHORIZATION_PDP_CONNECTION table, found %d", len(tableRows))), nil
	}

	columnRows, err := access.Query(ctx, engine.StoreIDConfigDB, postgresQueryAuthorizationPDPConnectionColumns)
	if err != nil {
		return authorizationPDPConnectionInspection{}, fmt.Errorf("inspect authorization PDP connection columns: %w", err)
	}
	if err := validateAuthorizationPDPConnectionColumns(columnRows); err != nil {
		return invalidAuthorizationPDPConnection(err.Error()), nil
	}

	primaryKeyRows, err := access.Query(
		ctx, engine.StoreIDConfigDB, postgresQueryAuthorizationPDPConnectionPrimaryKey)
	if err != nil {
		return authorizationPDPConnectionInspection{}, fmt.Errorf("inspect authorization PDP connection primary key: %w", err)
	}
	if err := validateAuthorizationPDPConnectionPrimaryKey(primaryKeyRows); err != nil {
		return invalidAuthorizationPDPConnection(err.Error()), nil
	}

	indexRows, err := access.Query(ctx, engine.StoreIDConfigDB, postgresQueryAuthorizationPDPConnectionIndex)
	if err != nil {
		return authorizationPDPConnectionInspection{}, fmt.Errorf("inspect authorization PDP connection index: %w", err)
	}
	if err := validateAuthorizationPDPConnectionIndex(indexRows); err != nil {
		return invalidAuthorizationPDPConnection(err.Error()), nil
	}

	return authorizationPDPConnectionInspection{
		state:   engine.VerificationTarget,
		summary: "The authorization PDP connection table and index match the target schema",
	}, nil
}

func invalidAuthorizationPDPConnection(summary string) authorizationPDPConnectionInspection {
	return authorizationPDPConnectionInspection{state: engine.VerificationInvalid, summary: summary}
}

func validateAuthorizationPDPConnectionColumns(rows []engine.QueryRow) error {
	if len(rows) != len(authorizationPDPConnectionColumns) {
		return fmt.Errorf("expected %d AUTHORIZATION_PDP_CONNECTION columns, found %d",
			len(authorizationPDPConnectionColumns), len(rows))
	}
	for index, expected := range authorizationPDPConnectionColumns {
		actualName, err := rows[index].String("column_name")
		if err != nil {
			return err
		}
		actualNullable, err := rows[index].String("is_nullable")
		if err != nil {
			return err
		}
		actualType, err := rows[index].String("data_type")
		if err != nil {
			return err
		}
		actualLength, err := rows[index].String("maximum_length")
		if err != nil {
			return err
		}
		actualDefault, err := rows[index].String("has_default")
		if err != nil {
			return err
		}
		if actualName != expected.name || actualNullable != expected.nullable || actualType != expected.dataType ||
			actualLength != expected.maximumLength || actualDefault != expected.hasDefault {
			return fmt.Errorf("AUTHORIZATION_PDP_CONNECTION column %d does not match the target schema", index+1)
		}
	}
	return nil
}

func validateAuthorizationPDPConnectionPrimaryKey(rows []engine.QueryRow) error {
	if len(rows) != 1 {
		return fmt.Errorf("expected a one-column AUTHORIZATION_PDP_CONNECTION primary key, found %d columns", len(rows))
	}
	column, err := rows[0].String("column_name")
	if err != nil {
		return err
	}
	position, err := rows[0].Int64("ordinal_position")
	if err != nil {
		return err
	}
	if column != "id" || position != 1 {
		return fmt.Errorf("AUTHORIZATION_PDP_CONNECTION primary key does not match the target schema")
	}
	return nil
}

func validateAuthorizationPDPConnectionIndex(rows []engine.QueryRow) error {
	expectedColumns := []string{"deployment_id", "type", "name"}
	if len(rows) != len(expectedColumns) {
		return fmt.Errorf("expected %d columns in index %s, found %d",
			len(expectedColumns), authorizationPDPConnectionIndex, len(rows))
	}
	for index, expectedColumn := range expectedColumns {
		unique, err := rows[index].String("is_unique")
		if err != nil {
			return err
		}
		column, err := rows[index].String("column_name")
		if err != nil {
			return err
		}
		position, err := rows[index].Int64("column_position")
		if err != nil {
			return err
		}
		if unique != "true" || column != expectedColumn || position != int64(index+1) {
			return fmt.Errorf("index %s does not match the target schema", authorizationPDPConnectionIndex)
		}
	}
	return nil
}

const postgresCreateAuthorizationPDPConnectionTableSQL = `CREATE TABLE "AUTHORIZATION_PDP_CONNECTION" (
    DEPLOYMENT_ID VARCHAR(255) NOT NULL,
    ID VARCHAR(36) PRIMARY KEY,
    NAME VARCHAR(255) NOT NULL,
    DESCRIPTION VARCHAR(500),
    TYPE VARCHAR(50) NOT NULL,
    PROPERTIES JSONB NOT NULL,
    CREATED_AT TIMESTAMPTZ DEFAULT NOW(),
    UPDATED_AT TIMESTAMPTZ DEFAULT NOW()
)`

const postgresCreateAuthorizationPDPConnectionIndexSQL = `CREATE UNIQUE INDEX idx_authorization_pdp_connection_type_name_deployment
    ON "AUTHORIZATION_PDP_CONNECTION" (DEPLOYMENT_ID, TYPE, NAME)`
