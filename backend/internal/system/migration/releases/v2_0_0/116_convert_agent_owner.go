// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	convertAgentOwnerOperationID         engine.OperationID = "2.0.0/entitydb/116_convert_agent_owner"
	convertAgentOwnerRollbackOperationID engine.OperationID = "2.0.0/entitydb/116_convert_agent_owner_rollback"
)

var (
	// queryLegacyAgentOwners counts the agents whose owner is still the bare entity ID the previous release stored.
	queryLegacyAgentOwners = dbmodel.DBQuery{
		ID: "MIG-AGO-01",
		PostgresQuery: `SELECT COUNT(*) AS owners FROM "ENTITY"
			WHERE CATEGORY = 'agent' AND JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'string'
				AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
	// queryRevertibleAgentOwners counts the owner objects a bare entity ID can represent: a local owner named by
	// its entity ID.
	queryRevertibleAgentOwners = dbmodel.DBQuery{
		ID: "MIG-AGO-02",
		PostgresQuery: `SELECT COUNT(*) AS owners FROM "ENTITY"
			WHERE CATEGORY = 'agent' AND JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'object'
				AND COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,idType}', 'uuid') = 'uuid'
				AND COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,isLocal}', 'true') = 'true'
				AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
	// queryUnrevertibleAgentOwners counts the owner objects a bare entity ID cannot represent, such as an owner
	// managed in another identity system.
	queryUnrevertibleAgentOwners = dbmodel.DBQuery{
		ID: "MIG-AGO-03",
		PostgresQuery: `SELECT COUNT(*) AS owners FROM "ENTITY"
			WHERE CATEGORY = 'agent' AND JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'object'
				AND (COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,idType}', 'uuid') <> 'uuid'
					OR COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,isLocal}', 'true') <> 'true')
				AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
)

// agentOwnerCount reads one of the owner counts above.
func agentOwnerCount(
	ctx context.Context,
	access engine.ReadAccess,
	query dbmodel.DBQuery,
	deploymentID string,
) (int64, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, query, deploymentID)
	if err != nil {
		return 0, fmt.Errorf("count agent owners: %w", err)
	}
	if len(rows) != 1 {
		return 0, errors.New("count agent owners: no result")
	}
	return rows[0].Int64("owners")
}

func agentOwnerTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return deploymentExecutionTargets(selection), nil
}

func agentOwnerStatementArgs(target engine.ExecutionTarget) ([]interface{}, error) {
	return []interface{}{target.DeploymentID}, nil
}

// newConvertAgentOwnerOperation stores each agent's owner as the object the 2.0.0 server reads, which names an
// id, an id type and whether the owner is local. The previous release stored the owner's entity ID as text, and
// the new server reads that as no owner, so without this the agent loses its owner. Every existing owner is a
// local user named by its entity ID. It runs in migrate, after the previous release is retired, so no agent can
// still be created with the old form.
func newConvertAgentOwnerOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		legacy, err := agentOwnerCount(ctx, access, queryLegacyAgentOwners, target.DeploymentID)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if legacy == 0 {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "Every agent owner is stored as an object"}, nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("%d agents have an owner stored as an entity ID", legacy)}, nil
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               convertAgentOwnerOperationID,
			Description:      "Store each agent's owner as an object that names its id, id type and locality",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		TargetResolver: agentOwnerTargets,
		PreVerifier:    verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{{
				ID: "convert-agent-owner",
				Query: `UPDATE "ENTITY" SET SYSTEM_ATTRIBUTES = JSONB_SET(SYSTEM_ATTRIBUTES, '{owner}',
						JSONB_BUILD_OBJECT('id', SYSTEM_ATTRIBUTES ->> 'owner', 'idType', 'uuid', 'isLocal', TRUE))
					WHERE CATEGORY = 'agent' AND JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'string'
						AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
				Args: agentOwnerStatementArgs,
			}}},
		},
		PostVerifier: verify,
	})
}

// rollbackSQLOperation is an atomic SQL operation associated with the forward operation it undoes.
type rollbackSQLOperation struct {
	*engine.SQLOperation
	forwardID engine.OperationID
}

func (o *rollbackSQLOperation) ForwardOperationID() engine.OperationID { return o.forwardID }

// newRollbackConvertAgentOwnerOperation writes each local owner back as the entity ID the previous release
// reads. An owner it cannot represent, such as one managed in another identity system, blocks the rollback with
// the count, since writing it back would lose the owner.
func newRollbackConvertAgentOwnerOperation() (engine.RollbackOperation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, target engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		unrevertible, err := agentOwnerCount(ctx, access, queryUnrevertibleAgentOwners, target.DeploymentID)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if unrevertible > 0 {
			return invalidVerification("agent_owner_not_representable", fmt.Sprintf(
				"%d agents have an owner that the previous release cannot represent", unrevertible)), nil
		}
		revertible, err := agentOwnerCount(ctx, access, queryRevertibleAgentOwners, target.DeploymentID)
		if err != nil {
			return engine.VerificationResult{}, err
		}
		if revertible == 0 {
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: "Every agent owner is stored as an entity ID"}, nil
		}
		return engine.VerificationResult{State: engine.VerificationSource,
			Summary: fmt.Sprintf("%d agent owners are stored as objects", revertible)}, nil
	}
	operation, err := engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               convertAgentOwnerRollbackOperationID,
			Description:      "Store each agent's owner as the entity ID the previous release reads",
			StoreID:          engine.StoreIDEntityDB,
			Kind:             engine.OperationKindDataChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		TargetResolver: agentOwnerTargets,
		PreVerifier:    verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{{
				ID: "revert-agent-owner",
				Query: `UPDATE "ENTITY" SET SYSTEM_ATTRIBUTES = JSONB_SET(SYSTEM_ATTRIBUTES, '{owner}',
						TO_JSONB(SYSTEM_ATTRIBUTES #>> '{owner,id}'))
					WHERE CATEGORY = 'agent' AND JSONB_TYPEOF(SYSTEM_ATTRIBUTES -> 'owner') = 'object'
						AND COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,idType}', 'uuid') = 'uuid'
						AND COALESCE(SYSTEM_ATTRIBUTES #>> '{owner,isLocal}', 'true') = 'true'
						AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
				Args: agentOwnerStatementArgs,
			}}},
		},
		PostVerifier: verify,
	})
	if err != nil {
		return nil, err
	}
	return &rollbackSQLOperation{SQLOperation: operation, forwardID: convertAgentOwnerOperationID}, nil
}
