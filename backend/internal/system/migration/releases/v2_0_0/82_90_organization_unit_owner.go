// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// organizationUnitOwnerTable is a configuration table whose rows now record the organization unit that owns
// them. Rows created before ownership was recorded are owned by the system organization unit.
type organizationUnitOwnerTable struct {
	table   string
	slug    string
	addID   engine.OperationID
	fillID  engine.OperationID
	finalID engine.OperationID
}

var organizationUnitOwnerTables = []organizationUnitOwnerTable{
	{table: "FLOW", slug: "flow",
		addID:   "2.0.0/configdb/82_add_flow_ou_id",
		fillID:  "2.0.0/configdb/85_backfill_flow_ou_id",
		finalID: "2.0.0/configdb/88_finalize_flow_ou_id"},
	{table: "IDP", slug: "idp",
		addID:   "2.0.0/configdb/83_add_idp_ou_id",
		fillID:  "2.0.0/configdb/86_backfill_idp_ou_id",
		finalID: "2.0.0/configdb/89_finalize_idp_ou_id"},
	{table: "NOTIFICATION_SENDER", slug: "notification_sender",
		addID:   "2.0.0/configdb/84_add_notification_sender_ou_id",
		fillID:  "2.0.0/configdb/87_backfill_notification_sender_ou_id",
		finalID: "2.0.0/configdb/90_finalize_notification_sender_ou_id"},
}

// queryOrganizationUnitExists looks for an organization unit by its ID in any deployment: the system organization
// unit is named once for the server, and a deployment table is read only to see whether it exists.
var queryOrganizationUnitExists = dbmodel.DBQuery{
	ID:            "MIG-OWN-ou-exists",
	PostgresQuery: `SELECT COUNT(*) AS units FROM "ORGANIZATION_UNIT" WHERE OU_ID = $1`,
}

var ownerColumnNullable = columnSpec{"ou_id", "YES", "character varying", "36", "none"}
var ownerColumnRequired = columnSpec{"ou_id", "NO", "character varying", "36", "none"}

// newAddOrganizationUnitOwnerColumnOperation adds the owner column as nullable with no default, so the previous
// release, which never names it, keeps inserting rows without an owner.
func newAddOrganizationUnitOwnerColumnOperation(owner organizationUnitOwnerTable) (engine.Operation, error) {
	return newAddColumnOperation(addColumnSpec{
		id:          owner.addID,
		description: fmt.Sprintf("Add the owning organization unit to %s rows", owner.slug),
		storeID:     engine.StoreIDConfigDB,
		table:       owner.table,
		columns:     []columnSpec{ownerColumnNullable},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-ou-id", Query: fmt.Sprintf(`ALTER TABLE %q ADD COLUMN OU_ID VARCHAR(36)`, owner.table)},
		},
	})
}

// newBackfillOrganizationUnitOwnerOperation gives every row without an owner the system organization unit. It
// runs in migrate, after the previous release is retired, so no row can be inserted without an owner behind it.
// It is blocked until the system organization unit is declared, so no owner is ever guessed.
func newBackfillOrganizationUnitOwnerOperation(
	owner organizationUnitOwnerTable,
	declared *declarativeOrganizationUnitSource,
) (engine.Operation, error) {
	missing := dbmodel.DBQuery{
		ID: "MIG-OWN-" + owner.slug + "-missing",
		PostgresQuery: fmt.Sprintf(`SELECT COUNT(*) AS missing FROM %q
			WHERE OU_ID IS NULL AND ($1 = '' OR DEPLOYMENT_ID = $1)`, owner.table),
	}
	verify := func(ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		column, exists, err := inspectOwnerColumn(ctx, access, owner.table)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if !exists || column != ownerColumnNullable && column != ownerColumnRequired {
			return invalidVerification("unexpected_ou_id_column", fmt.Sprintf(
				"%s.OU_ID is missing or has an unexpected definition", owner.table)), nil
		}
		rows, err := access.Query(ctx, engine.StoreIDConfigDB, missing, target.DeploymentID)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("count %s rows without an owner: %w", owner.table, err)
		}
		if len(rows) != 1 {
			return engine.VerificationResult{}, fmt.Errorf("count %s rows without an owner: no result", owner.table)
		}
		count, err := rows[0].Int64("missing")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if count == 0 {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: fmt.Sprintf("Every %s row has an owning organization unit", owner.table)}, nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("%d %s rows have no owning organization unit", count, owner.table)}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               owner.fillID,
			Description:      fmt.Sprintf("Give %s rows without an owner the system organization unit", owner.slug),
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindDataChange,
			ReadStoreIDs:     []engine.StoreID{engine.StoreIDEntityDB},
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			_, exists, err := inspectOwnerColumn(ctx, access, owner.table)
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			if !exists {
				return engine.ReadinessResult{State: engine.ReadinessDeferred,
					Summary:       fmt.Sprintf("The %s owner column is not added yet", owner.table),
					DeferredUntil: []engine.OperationID{owner.addID}}, nil
			}
			return systemOrganizationUnitReadiness(ctx, access, declared)
		},
		TargetResolver: func(_ context.Context, _ engine.ReadAccess, selection engine.MigrationSelection) (
			[]engine.ExecutionTarget, error) {
			return deploymentExecutionTargets(selection), nil
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{{
				ID: "backfill-ou-id",
				Query: fmt.Sprintf(`UPDATE %q SET OU_ID = $2
					WHERE OU_ID IS NULL AND ($1 = '' OR DEPLOYMENT_ID = $1)`, owner.table),
				Args: func(target engine.ExecutionTarget) ([]interface{}, error) {
					return []interface{}{target.DeploymentID, declared.systemOrganizationUnitID()}, nil
				},
			}}},
		},
		PostVerifier: verify,
	})
}

// systemOrganizationUnitReadiness is ready when the deployment names a system organization unit and that unit
// exists, in the database or in the declarative resources. It is blocked with a remediation otherwise, so no
// owner is ever guessed.
func systemOrganizationUnitReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	declared *declarativeOrganizationUnitSource,
) (engine.ReadinessResult, error) {
	systemID := declared.systemOrganizationUnitID()
	if systemID == "" {
		summary := "The deployment configuration names no system organization unit in server.system_ou_id"
		return engine.ReadinessResult{State: engine.ReadinessBlocked, Summary: summary,
			Findings: []engine.Finding{{
				Code: "system_organization_unit_not_configured", Message: summary,
				Remediation: "Set server.system_ou_id to the ID of the system organization unit, then run the " +
					"migration again",
			}}}, nil
	}
	_, found, err := declared.systemOrganizationUnit(ctx)
	if err != nil {
		return engine.ReadinessResult{}, fmt.Errorf("read the declared system organization unit: %w", err)
	}
	if !found {
		rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryOrganizationUnitExists, systemID)
		if err != nil {
			return engine.ReadinessResult{}, fmt.Errorf("look up the system organization unit: %w", err)
		}
		if len(rows) == 1 {
			units, err := rows[0].Int64("units")
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			found = units > 0
		}
	}
	if found {
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The system organization unit exists"}, nil
	}
	summary := "The system organization unit does not exist in the database or in the declarative resources"
	return engine.ReadinessResult{State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "system_organization_unit_not_found", Message: summary,
			Remediation: "Create the organization unit " + systemID + ", or declare it in the " +
				"organization_units resource directory, then run the migration again",
		}}}, nil
}

// newFinalizeOrganizationUnitOwnerOperation makes the owner column required once every row has an owner.
func newFinalizeOrganizationUnitOwnerOperation(owner organizationUnitOwnerTable) (engine.Operation, error) {
	missing := dbmodel.DBQuery{
		ID:            "MIG-OWN-" + owner.slug + "-unowned",
		PostgresQuery: fmt.Sprintf(`SELECT COUNT(*) AS missing FROM %q WHERE OU_ID IS NULL`, owner.table),
	}
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		column, exists, err := inspectOwnerColumn(ctx, access, owner.table)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		switch {
		case !exists:
			return invalidVerification("unexpected_ou_id_column", owner.table+".OU_ID is missing"), nil
		case column == ownerColumnRequired:
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: owner.table + ".OU_ID is required"}, nil
		case column != ownerColumnNullable:
			return invalidVerification("unexpected_ou_id_column", owner.table+
				".OU_ID has an unexpected definition"), nil
		}
		rows, err := access.Query(ctx, engine.StoreIDConfigDB, missing)
		if err != nil {
			return engine.VerificationResult{}, fmt.Errorf("count %s rows without an owner: %w", owner.table, err)
		}
		if len(rows) != 1 {
			return engine.VerificationResult{}, fmt.Errorf("count %s rows without an owner: no result", owner.table)
		}
		count, err := rows[0].Int64("missing")
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if count > 0 {
			return invalidVerification("organization_unit_owner_missing", fmt.Sprintf(
				"%d %s rows still have no owning organization unit", count, owner.table)), nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: owner.table + ".OU_ID can be made required"}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               owner.finalID,
			Description:      fmt.Sprintf("Require an owning organization unit on %s rows", owner.slug),
			StoreID:          engine.StoreIDConfigDB,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			column, exists, err := inspectOwnerColumn(ctx, access, owner.table)
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			if !exists {
				return engine.ReadinessResult{State: engine.ReadinessDeferred,
					Summary:       fmt.Sprintf("The %s owner column is not added yet", owner.table),
					DeferredUntil: []engine.OperationID{owner.addID}}, nil
			}
			verification, err := verify(ctx, access, engine.ExecutionTarget{})
			if err != nil {
				return engine.ReadinessResult{}, err
			}
			if verification.State != engine.VerificationInvalid {
				return engine.ReadinessResult{State: engine.ReadinessReady, Summary: verification.Summary}, nil
			}
			if column == ownerColumnNullable {
				return engine.ReadinessResult{State: engine.ReadinessDeferred, Summary: verification.Summary,
					DeferredUntil: []engine.OperationID{owner.fillID}}, nil
			}
			return engine.ReadinessResult{State: engine.ReadinessBlocked, Summary: verification.Summary,
				Findings: verification.Findings}, nil
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "require-ou-id", Query: fmt.Sprintf(
					`ALTER TABLE %q ALTER COLUMN OU_ID SET NOT NULL`, owner.table)},
			}},
		},
		PostVerifier: verify,
	})
}

func inspectOwnerColumn(ctx context.Context, access engine.ReadAccess, table string) (columnSpec, bool, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryTableColumns, table)
	if err != nil {
		return columnSpec{}, false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	for _, row := range rows {
		column, err := readColumn(row)
		if err != nil {
			return columnSpec{}, false, err
		}
		if column.name == "ou_id" {
			return column, true, nil
		}
	}
	return columnSpec{}, false, nil
}
