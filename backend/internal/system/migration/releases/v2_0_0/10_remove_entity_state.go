// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const removeEntityStateOperationID engine.OperationID = "2.0.0/entitydb/10_remove_entity_state"

func newRemoveEntityStateOperation() (engine.Operation, error) {
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               removeEntityStateOperationID,
			Description:      "Remove the legacy entity state column",
			StoreID:          engine.StoreIDEntityDB,
			ReadStoreIDs:     []engine.StoreID{engine.StoreIDRuntimePersistent},
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: checkRemoveEntityStateReadiness,
		PreVerifier:      verifyEntityStateRemoval,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {
				Statements: []engine.SQLStatement{
					{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
					{ID: "drop-state", Query: `ALTER TABLE "ENTITY" DROP COLUMN STATE`},
				},
			},
		},
		PostVerifier: verifyEntityStateRemoval,
	})
}

func checkRemoveEntityStateReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	runtimeInspection, err := inspectEntityRuntimeData(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if runtimeInspection.state != engine.VerificationTarget {
		return blockedEntityStateContractReadiness(runtimeInspection.summary), nil
	}
	stateInspection, err := inspectEntityState(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	switch stateInspection.state {
	case engine.VerificationSource:
		return blockedEntityStateContractReadiness(
			"ENTITY.STATE does not have the temporary ACTIVE default required before contract"), nil
	case engine.VerificationTarget:
		if stateInspection.absent {
			return engine.ReadinessResult{State: engine.ReadinessReady,
				Summary: "The legacy entity state column has already been removed"}, nil
		}
		return engine.ReadinessResult{State: engine.ReadinessReady,
			Summary: "The legacy entity state column is ready to be removed"}, nil
	default:
		return blockedEntityStateContractReadiness(stateInspection.summary), nil
	}
}

func blockedEntityStateContractReadiness(summary string) engine.ReadinessResult {
	return engine.ReadinessResult{
		State: engine.ReadinessBlocked, Summary: summary,
		Findings: []engine.Finding{{
			Code: "entity_state_contract_not_ready", Message: summary,
			Remediation: "Complete and verify the entity runtime expand operations before retrying contract",
		}},
	}
}

func verifyEntityStateRemoval(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	runtimeInspection, err := inspectEntityRuntimeData(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if runtimeInspection.state != engine.VerificationTarget {
		return invalidEntityStateRemoval(runtimeInspection.summary), nil
	}
	stateInspection, err := inspectEntityState(ctx, access)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if stateInspection.state == engine.VerificationSource {
		return invalidEntityStateRemoval(
			"ENTITY.STATE does not have the temporary ACTIVE default required before contract"), nil
	}
	if stateInspection.state == engine.VerificationInvalid {
		return invalidEntityStateRemoval(stateInspection.summary), nil
	}
	if stateInspection.absent {
		return engine.VerificationResult{State: engine.VerificationTarget,
			Summary: "The legacy ENTITY.STATE column is absent"}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource,
		Summary: "The legacy ENTITY.STATE column defaults to ACTIVE and is ready to be removed"}, nil
}

func invalidEntityStateRemoval(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State: engine.VerificationInvalid, Summary: summary,
		Findings: []engine.Finding{{Code: "entity_state_contract_not_ready", Message: summary}},
	}
}
