// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const addAgentTypeOrganizationUnitIndexOperationID engine.OperationID = "configdb/" +
	"115_add_agent_type_organization_unit_index"

var queryOrganizationUnitsWithSeveralAgentTypes = dbmodel.DBQuery{
	ID: "MIG-OIX-AGT-01",
	PostgresQuery: `SELECT DEPLOYMENT_ID AS deployment_id, OU_ID AS ou_id, COUNT(*) AS agent_types
		FROM "ENTITY_TYPES" WHERE CATEGORY = 'agent'
		GROUP BY DEPLOYMENT_ID, OU_ID HAVING COUNT(*) > 1
		ORDER BY DEPLOYMENT_ID, OU_ID LIMIT 20`,
}

// NewAddAgentTypeOrganizationUnitIndexOperation keeps an organization unit to one agent type. The rule would
// refuse what the previous release lets an administrator create, so it is added in contract, after that release
// is retired. Data that already breaks it blocks the operation with the organization units named, since choosing
// which agent type to keep is the administrator's decision.
func NewAddAgentTypeOrganizationUnitIndexOperation() engine.Operation {
	return newOnlineIndexOperation(onlineIndexSpec{
		id:          addAgentTypeOrganizationUnitIndexOperationID,
		description: "Allow one agent type per organization unit",
		storeID:     engine.StoreIDConfigDB,
		table:       "ENTITY_TYPES",
		name:        "idx_entity_types_agent_deployment_ou",
		definition:  "(deployment_id, ou_id) WHERE ((category)::text = 'agent'::text)",
		createSQL: `CREATE UNIQUE INDEX CONCURRENTLY "idx_entity_types_agent_deployment_ou"
			ON "ENTITY_TYPES" (DEPLOYMENT_ID, OU_ID) WHERE CATEGORY = 'agent'`,
		unique:       true,
		partial:      true,
		precondition: checkOneAgentTypePerOrganizationUnit,
	}, "MIG-OIX-AGT-IDX")
}

func checkOneAgentTypePerOrganizationUnit(
	ctx context.Context,
	access engine.ReadAccess,
) (*engine.ReadinessResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryOrganizationUnitsWithSeveralAgentTypes)
	if err != nil {
		return nil, fmt.Errorf("inspect agent types per organization unit: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	findings := make([]engine.Finding, 0, len(rows))
	for _, row := range rows {
		deployment, deploymentErr := row.String("deployment_id")
		unit, unitErr := row.String("ou_id")
		count, countErr := row.Int64("agent_types")
		if err := errorsJoin(deploymentErr, unitErr, countErr); err != nil {
			return nil, fmt.Errorf("read agent types per organization unit: %w", err)
		}
		findings = append(findings, engine.Finding{
			Code: "several_agent_types_in_organization_unit",
			Message: fmt.Sprintf("Organization unit %q of deployment %q holds %d agent types",
				unit, deployment, count),
			Remediation: "Delete or move the agent types until the organization unit holds at most one, then " +
				"retry",
		})
	}
	return &engine.ReadinessResult{
		State:    engine.ReadinessBlocked,
		Summary:  "An organization unit holds more than one agent type",
		Findings: findings,
	}, nil
}
