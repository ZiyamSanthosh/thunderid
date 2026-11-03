// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const removeLegacySubjectAttributeOperationID engine.OperationID = "2.0.0/entitydb/52_remove_legacy_subject_attribute"

var (
	queryUserTypeSubDeclarations = dbmodel.DBQuery{
		ID: "MIG-CFG_MGT-39",
		PostgresQuery: `SELECT NAME, HANDLE, SCHEMA_DEF, DEPLOYMENT_ID
			FROM "ENTITY_TYPES" WHERE CATEGORY = 'user'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)`,
	}
	queryRemoveLegacySubjectAttribute = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-23",
		PostgresQuery: `UPDATE "ENTITY" SET ATTRIBUTES = $1
			WHERE ID = $2
			AND ATTRIBUTES IS NOT DISTINCT FROM $3
			AND DEPLOYMENT_ID = $4`,
	}
	queryDeleteLegacySubjectIdentifiers = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-24",
		PostgresQuery: `DELETE FROM "ENTITY_IDENTIFIER"
			WHERE ENTITY_ID = $1 AND DEPLOYMENT_ID = $2 AND NAME = 'sub' AND SOURCE = 'attribute'`,
	}
)

// removeLegacySubjectAttributeOperation removes the legacy sub attribute, and its identifier row, from users whose
// user type no longer declares it. It runs after the source has gone, so nothing reads sub any more.
type removeLegacySubjectAttributeOperation struct {
	connections *declarativeConnectionSource
	entityTypes *declarativeEntityTypeSource
}

func newRemoveLegacySubjectAttributeOperation(
	connections *declarativeConnectionSource,
	entityTypes *declarativeEntityTypeSource,
) engine.Operation {
	return &removeLegacySubjectAttributeOperation{connections: connections, entityTypes: entityTypes}
}

func (*removeLegacySubjectAttributeOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               removeLegacySubjectAttributeOperationID,
		Description:      "Remove the legacy sub attribute from users whose user type no longer declares it",
		StoreID:          engine.StoreIDEntityDB,
		ReadStoreIDs:     []engine.StoreID{engine.StoreIDConfigDB},
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (*removeLegacySubjectAttributeOperation) ResolveTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return deploymentExecutionTargets(selection), nil
}

func (o *removeLegacySubjectAttributeOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	columns, err := inspectEntityTypeMigrationColumns(ctx, access)
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if _, exists := columns["HANDLE"]; !exists {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Removing the legacy subject attribute depends on the entity-type handle column",
			DeferredUntil: []engine.OperationID{addEntityTypeHandleOperationID},
		}, nil
	}
	_, complete, err := readEntityTypeReferenceMapping(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if !complete {
		return engine.ReadinessResult{
			State:         engine.ReadinessDeferred,
			Summary:       "Removing the legacy subject attribute depends on entity-type handles",
			DeferredUntil: []engine.OperationID{backfillEntityTypeHandleOperationID},
		}, nil
	}
	inspection, err := o.inspect(ctx, access, "")
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if inspection.problem != "" {
		verification := invalidLegacySubjectVerification(inspection.problem)
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary, Findings: verification.Findings,
		}, nil
	}
	if inspection.missingLinks > 0 {
		return engine.ReadinessResult{
			State: engine.ReadinessDeferred,
			Summary: "Removing the legacy subject attribute depends on the links recorded for " +
				"legacy federated subjects",
			DeferredUntil: []engine.OperationID{
				linkLegacyFederatedSubjectsOperationID, linkLegacyFederatedSubjectsCatchUpOperationID,
			},
		}, nil
	}
	return engine.ReadinessResult{
		State: engine.ReadinessReady, Summary: "The legacy subject attribute is ready to be removed",
	}, nil
}

func (o *removeLegacySubjectAttributeOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *removeLegacySubjectAttributeOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *removeLegacySubjectAttributeOperation) ExecuteBatch(
	ctx context.Context,
	execution engine.OperationExecution,
	checkpointData json.RawMessage,
) (engine.ResumableBatchResult, error) {
	checkpoint := legacySubjectCheckpoint{}
	if len(checkpointData) > 0 {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return engine.ResumableBatchResult{}, fmt.Errorf("decode legacy subject checkpoint: %w", err)
		}
	}
	rows, err := execution.Access().Query(ctx, engine.StoreIDEntityDB, queryLegacySubjectUserBatch,
		checkpoint.DeploymentID, checkpoint.ID, execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read legacy subject batch: %w", err)
	}
	if len(rows) == 0 {
		return legacySubjectBatchResult(checkpoint, true)
	}
	declarations, err := readUserTypeSubDeclarations(
		ctx, execution.Access(), execution.Target().DeploymentID, o.entityTypes)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	connectionIDs, err := o.connections.idsByProvider(ctx)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read declarative connections: %w", err)
	}
	identifiers, err := readLinkedSubjectIdentifiers(ctx, execution.Access(), execution.Target().DeploymentID)
	if err != nil {
		return engine.ResumableBatchResult{}, err
	}
	for _, row := range rows {
		record, err := readLegacySubjectRecord(row)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		if legacySubjectRemovable(record, declarations) {
			if err := removeLegacySubject(ctx, execution, record, connectionIDs, identifiers); err != nil {
				return engine.ResumableBatchResult{}, err
			}
		}
		checkpoint = legacySubjectCheckpoint{DeploymentID: record.deploymentID, ID: record.id}
	}
	return legacySubjectBatchResult(checkpoint, len(rows) < legacySubjectBatchSize)
}

// removeLegacySubject drops sub from one user and deletes the identifier row that indexed it. It refuses a user
// without the recorded link, since removing sub would then leave the user without any federated identity. The
// attribute update compares the stored value with the one read, so a concurrent change aborts the batch.
func removeLegacySubject(
	ctx context.Context,
	execution engine.OperationExecution,
	record legacySubjectRecord,
	connectionIDs map[legacySubjectProvider][]string,
	identifiers map[string][]legacySubjectIdentifier,
) error {
	if err := legacySubjectLinkRecorded(record, connectionIDs, identifiers); err != nil {
		return err
	}
	delete(record.attributes, legacySubjectAttribute)
	attributes, err := json.Marshal(record.attributes)
	if err != nil {
		return fmt.Errorf("encode user %q attributes: %w", record.id, err)
	}
	affected, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB, queryRemoveLegacySubjectAttribute,
		string(attributes), record.id, record.originalAttributes, record.deploymentID)
	if err != nil {
		return fmt.Errorf("remove user %q legacy subject: %w", record.id, err)
	}
	if affected != 1 {
		return fmt.Errorf("remove user %q legacy subject: source value changed concurrently", record.id)
	}
	if _, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB, queryDeleteLegacySubjectIdentifiers,
		record.id, record.deploymentID); err != nil {
		return fmt.Errorf("delete user %q legacy subject identifier: %w", record.id, err)
	}
	return nil
}

// legacySubjectLinkRecorded fails unless the user holds the link its legacy subject needs, both in the system
// attributes and in the identifier index.
func legacySubjectLinkRecorded(
	record legacySubjectRecord,
	connectionIDs map[legacySubjectProvider][]string,
	identifiers map[string][]legacySubjectIdentifier,
) error {
	link, err := legacySubjectLinkFor(record, connectionIDs)
	if err != nil {
		return err
	}
	hasLink, err := hasLegacySubjectLink(record.systemAttributes, link)
	if err != nil {
		return fmt.Errorf("user %q has an invalid linked identity representation: %w", record.id, err)
	}
	if !hasLink || !hasLinkedSubjectIdentifier(identifiers[legacySubjectKey(record.deploymentID, record.id)], link) {
		return fmt.Errorf("user %q has no recorded link for its legacy subject", record.id)
	}
	return nil
}

// legacySubjectRemovalInspection summarizes the users whose legacy subject this operation removes.
type legacySubjectRemovalInspection struct {
	// removable counts the users whose user type does not declare sub.
	removable int
	// missingLinks counts the removable users that lack the recorded link or its identifier row.
	missingLinks int
	// problem describes the first condition that makes removal unsafe, when there is one.
	problem string
}

func (o *removeLegacySubjectAttributeOperation) inspect(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (legacySubjectRemovalInspection, error) {
	records, err := readLegacySubjectRecords(ctx, access, deploymentID)
	if err != nil {
		return legacySubjectRemovalInspection{}, err
	}
	if len(records) == 0 {
		return legacySubjectRemovalInspection{}, nil
	}
	declarations, err := readUserTypeSubDeclarations(ctx, access, deploymentID, o.entityTypes)
	if err != nil {
		return legacySubjectRemovalInspection{}, err
	}
	var removable []legacySubjectRecord
	for _, record := range records {
		if legacySubjectRemovable(record, declarations) {
			removable = append(removable, record)
		}
	}
	if len(removable) == 0 {
		return legacySubjectRemovalInspection{}, nil
	}
	connectionIDs, err := o.connections.idsByProvider(ctx)
	if err != nil {
		return legacySubjectRemovalInspection{}, fmt.Errorf("read declarative connections: %w", err)
	}
	identifiers, err := readLinkedSubjectIdentifiers(ctx, access, deploymentID)
	if err != nil {
		return legacySubjectRemovalInspection{}, err
	}

	inspection := legacySubjectRemovalInspection{removable: len(removable)}
	for _, record := range removable {
		link, err := legacySubjectLinkFor(record, connectionIDs)
		if err != nil {
			inspection.problem = fmt.Sprintf("Legacy federated subject check failed: %v", err)
			return inspection, nil
		}
		hasLink, err := hasLegacySubjectLink(record.systemAttributes, link)
		if err != nil {
			inspection.problem = fmt.Sprintf(
				"User %q has an invalid linked identity representation: %v", record.id, err)
			return inspection, nil
		}
		recorded := identifiers[legacySubjectKey(record.deploymentID, record.id)]
		if !hasLink || !hasLinkedSubjectIdentifier(recorded, link) {
			inspection.missingLinks++
		}
	}
	return inspection, nil
}

func (o *removeLegacySubjectAttributeOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	inspection, err := o.inspect(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if inspection.problem != "" {
		return invalidLegacySubjectVerification(inspection.problem), nil
	}
	if inspection.missingLinks > 0 {
		return invalidLegacySubjectVerification(fmt.Sprintf(
			"%d users have no recorded link for their legacy subject, so removing it would leave them without a "+
				"federated identity", inspection.missingLinks)), nil
	}
	if inspection.removable > 0 {
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: fmt.Sprintf(
				"%d users still hold a legacy subject their user type does not declare", inspection.removable),
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationTarget, Summary: "No user holds a legacy subject its user type does not declare",
	}, nil
}

// legacySubjectRemovable reports whether sub is removed from the user. That is only the case when the user's type
// is known and does not declare sub. A user whose type declares sub keeps it as an ordinary attribute, and a user
// whose type cannot be found is left alone because removal cannot be undone.
func legacySubjectRemovable(record legacySubjectRecord, declarations map[string]bool) bool {
	declares, known := declarations[userTypeKey(record.deploymentID, record.entityType)]
	if !known {
		declares, known = declarations[userTypeKey("", record.entityType)]
	}
	return known && !declares
}

// readUserTypeSubDeclarations reports, for every user type in the database and in the declarative resources,
// whether its schema declares the sub attribute. Types are keyed by deployment and by handle, and by name when no
// handle already claims that key. Declarative types are not scoped to a deployment and are keyed by an empty one.
func readUserTypeSubDeclarations(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
	declared *declarativeEntityTypeSource,
) (map[string]bool, error) {
	rows, err := access.Query(ctx, engine.StoreIDConfigDB, queryUserTypeSubDeclarations, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect user type schemas: %w", err)
	}
	byName := make(map[string]bool, len(rows))
	byHandle := make(map[string]bool, len(rows))
	for _, row := range rows {
		rowDeploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		schema, err := readAgentNameJSONObject(row, "schema_def")
		if err != nil {
			return nil, err
		}
		_, declares := schema[legacySubjectAttribute]
		if name, err := row.String("name"); err == nil {
			byName[userTypeKey(rowDeploymentID, name)] = declares
		}
		if handle, err := row.String("handle"); err == nil {
			byHandle[userTypeKey(rowDeploymentID, handle)] = declares
		}
	}
	declarations := make(map[string]bool, len(byName)+len(byHandle))
	for key, declares := range byName {
		declarations[key] = declares
	}
	for key, declares := range byHandle {
		declarations[key] = declares
	}

	declaredTypes, err := declared.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("read declarative user type schemas: %w", err)
	}
	for _, entityType := range declaredTypes {
		if !strings.EqualFold(entityType.Category, "user") {
			continue
		}
		if _, claimed := declarations[userTypeKey("", entityType.DisplayName)]; !claimed {
			declarations[userTypeKey("", entityType.DisplayName)] = entityType.DeclaresSubAttribute
		}
		declarations[userTypeKey("", entityType.Handle)] = entityType.DeclaresSubAttribute
	}
	return declarations, nil
}

func userTypeKey(deploymentID string, entityType string) string {
	return deploymentID + "\x00" + entityType
}
