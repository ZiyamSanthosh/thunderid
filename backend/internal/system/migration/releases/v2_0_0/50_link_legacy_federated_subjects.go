// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"encoding/json"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	legacySubjectBatchSize = 100
	// legacySubjectAttribute holds the federated subject of a user that was stored before federated
	// identities were recorded as links. The link attribute names are fixed here on purpose: a release
	// definition describes the representation of its own release and must not follow later renames.
	legacySubjectAttribute = "sub"
	linkedIDsAttribute     = "linkedIds"
	linkedIdentifierPrefix = "linkedIds."

	linkLegacyFederatedSubjectsOperationID engine.OperationID = "2.0.0/entitydb/" +
		"50_link_legacy_federated_subjects"
	linkLegacyFederatedSubjectsCatchUpOperationID engine.OperationID = "2.0.0/entitydb/" +
		"51_link_legacy_federated_subjects_catch_up"
	linkLegacyFederatedSubjectsRollbackOperationID engine.OperationID = "2.0.0/entitydb/" +
		"50_link_legacy_federated_subjects_rollback"
)

var (
	queryLegacySubjectUsers = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-17",
		PostgresQuery: `SELECT ID, TYPE, ATTRIBUTES, SYSTEM_ATTRIBUTES, DEPLOYMENT_ID
			FROM "ENTITY" WHERE CATEGORY = 'user' AND ATTRIBUTES ->> 'sub' IS NOT NULL
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ID`,
	}
	queryLegacySubjectUserBatch = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-18",
		PostgresQuery: `SELECT ID, TYPE, ATTRIBUTES, SYSTEM_ATTRIBUTES, DEPLOYMENT_ID
			FROM "ENTITY" WHERE CATEGORY = 'user' AND ATTRIBUTES ->> 'sub' IS NOT NULL
			AND (DEPLOYMENT_ID > $1 OR (DEPLOYMENT_ID = $1 AND ID > $2))
			AND ($3 = '' OR DEPLOYMENT_ID = $3)
			ORDER BY DEPLOYMENT_ID, ID LIMIT 100`,
	}
	queryUpdateLegacySubjectLinks = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-19",
		PostgresQuery: `UPDATE "ENTITY" SET SYSTEM_ATTRIBUTES = $1
			WHERE ID = $2
			AND SYSTEM_ATTRIBUTES IS NOT DISTINCT FROM $3
			AND DEPLOYMENT_ID = $4`,
	}
	// The conflict target is omitted so that the statement works with the three column key of the source
	// release and with the four column key that operation 07 introduces.
	queryInsertLinkedSubjectIdentifier = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-20",
		PostgresQuery: `INSERT INTO "ENTITY_IDENTIFIER"
			(ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT, DEPLOYMENT_ID)
			VALUES ($1, $2, $3, 'system', NOW(), $4)
			ON CONFLICT DO NOTHING`,
	}
	queryLinkedSubjectIdentifiers = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-21",
		PostgresQuery: `SELECT ENTITY_ID, NAME, VALUE, SOURCE, DEPLOYMENT_ID
			FROM "ENTITY_IDENTIFIER" WHERE NAME LIKE 'linkedIds.%'
			AND ($1 = '' OR DEPLOYMENT_ID = $1)
			ORDER BY DEPLOYMENT_ID, ENTITY_ID, NAME, VALUE`,
	}
	queryDeleteLinkedSubjectIdentifier = dbmodel.DBQuery{
		ID: "MIG-ENT_MGT-22",
		PostgresQuery: `DELETE FROM "ENTITY_IDENTIFIER"
			WHERE ENTITY_ID = $1 AND DEPLOYMENT_ID = $2 AND NAME = $3 AND VALUE = $4 AND SOURCE = 'system'`,
	}
)

type legacySubjectDirection string

const (
	legacySubjectForward  legacySubjectDirection = "forward"
	legacySubjectRollback legacySubjectDirection = "rollback"
)

type legacySubjectCheckpoint struct {
	DeploymentID string `json:"deploymentID"`
	ID           string `json:"id"`
}

type legacySubjectRecord struct {
	id                       string
	entityType               string
	deploymentID             string
	attributes               map[string]interface{}
	systemAttributes         map[string]interface{}
	originalAttributes       interface{}
	originalSystemAttributes interface{}
}

// legacySubjectLink is the recorded link a user's legacy subject must have.
type legacySubjectLink struct {
	idpID   string
	subject string
}

func (l legacySubjectLink) identifierName() string {
	return linkedIdentifierPrefix + l.idpID
}

type legacySubjectIdentifier struct {
	name   string
	value  string
	source string
}

type legacySubjectOperation struct {
	id          engine.OperationID
	description string
	direction   legacySubjectDirection
	connections *declarativeConnectionSource
}

type rollbackLegacySubjectOperation struct {
	*legacySubjectOperation
}

func newLinkLegacyFederatedSubjectsOperation(connections *declarativeConnectionSource) engine.Operation {
	return &legacySubjectOperation{
		id:          linkLegacyFederatedSubjectsOperationID,
		description: "Link users holding a legacy federated subject to the connection that issued it",
		direction:   legacySubjectForward,
		connections: connections,
	}
}

// newLinkLegacyFederatedSubjectsCatchUpOperation repeats operation 50 after the source has been drained, for
// users the source created or changed after the first pass.
func newLinkLegacyFederatedSubjectsCatchUpOperation(connections *declarativeConnectionSource) engine.Operation {
	return &legacySubjectOperation{
		id:          linkLegacyFederatedSubjectsCatchUpOperationID,
		description: "Link users who gained a legacy federated subject after Expand",
		direction:   legacySubjectForward,
		connections: connections,
	}
}

func newRollbackLinkLegacyFederatedSubjectsOperation(
	connections *declarativeConnectionSource,
) engine.RollbackOperation {
	return &rollbackLegacySubjectOperation{&legacySubjectOperation{
		id:          linkLegacyFederatedSubjectsRollbackOperationID,
		description: "Remove the links recorded for legacy federated subjects",
		direction:   legacySubjectRollback,
		connections: connections,
	}}
}

func (*rollbackLegacySubjectOperation) ForwardOperationID() engine.OperationID {
	return linkLegacyFederatedSubjectsOperationID
}

func (o *legacySubjectOperation) Definition() engine.OperationDefinition {
	return engine.OperationDefinition{
		ID:               o.id,
		Description:      o.description,
		StoreID:          engine.StoreIDEntityDB,
		Kind:             engine.OperationKindDataChange,
		InterruptionMode: engine.InterruptionModeResumable,
	}
}

func (*legacySubjectOperation) ResolveTargets(
	_ context.Context,
	_ engine.ReadAccess,
	selection engine.MigrationSelection,
) ([]engine.ExecutionTarget, error) {
	return deploymentExecutionTargets(selection), nil
}

// deploymentExecutionTargets returns one target per selected deployment, or a single store-wide target when no
// deployment is selected.
func deploymentExecutionTargets(selection engine.MigrationSelection) []engine.ExecutionTarget {
	if len(selection.DeploymentIDs) == 0 {
		return []engine.ExecutionTarget{{}}
	}
	targets := make([]engine.ExecutionTarget, 0, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		targets = append(targets, engine.ExecutionTarget{DeploymentID: deploymentID})
	}
	return targets
}

func (o *legacySubjectOperation) CheckReadiness(
	ctx context.Context,
	access engine.ReadAccess,
	_ engine.PlanContext,
) (engine.ReadinessResult, error) {
	verification, err := o.verify(ctx, access, engine.ExecutionTarget{})
	if err != nil {
		return engine.ReadinessResult{}, err
	}
	if verification.State == engine.VerificationInvalid {
		return engine.ReadinessResult{
			State: engine.ReadinessBlocked, Summary: verification.Summary, Findings: verification.Findings,
		}, nil
	}
	return engine.ReadinessResult{
		State: engine.ReadinessReady, Summary: "Legacy federated subjects are ready to be linked",
	}, nil
}

func (o *legacySubjectOperation) PreVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *legacySubjectOperation) PostVerify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	return o.verify(ctx, access, target)
}

func (o *legacySubjectOperation) ExecuteBatch(
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
	connectionIDs, err := o.connections.idsByProvider(ctx)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("read declarative connections: %w", err)
	}
	for _, row := range rows {
		record, err := readLegacySubjectRecord(row)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		link, err := legacySubjectLinkFor(record, connectionIDs)
		if err != nil {
			return engine.ResumableBatchResult{}, err
		}
		if err := o.applyLink(ctx, execution, record, link); err != nil {
			return engine.ResumableBatchResult{}, err
		}
		checkpoint = legacySubjectCheckpoint{DeploymentID: record.deploymentID, ID: record.id}
	}
	return legacySubjectBatchResult(checkpoint, len(rows) < legacySubjectBatchSize)
}

// applyLink brings one user's recorded link and its identifier row to the state of the operation direction.
// The attribute update compares the stored value with the one read, so a concurrent change aborts the batch.
func (o *legacySubjectOperation) applyLink(
	ctx context.Context,
	execution engine.OperationExecution,
	record legacySubjectRecord,
	link legacySubjectLink,
) error {
	var changed bool
	var err error
	if o.direction == legacySubjectForward {
		changed, err = addLegacySubjectLink(record.systemAttributes, link)
	} else {
		changed, err = removeLegacySubjectLink(record.systemAttributes, link)
	}
	if err != nil {
		return fmt.Errorf("user %q has an invalid linked identity representation: %w", record.id, err)
	}
	if changed {
		systemAttributes, err := json.Marshal(record.systemAttributes)
		if err != nil {
			return fmt.Errorf("encode user %q system attributes: %w", record.id, err)
		}
		affected, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB, queryUpdateLegacySubjectLinks,
			string(systemAttributes), record.id, record.originalSystemAttributes, record.deploymentID)
		if err != nil {
			return fmt.Errorf("update user %q linked identities: %w", record.id, err)
		}
		if affected != 1 {
			return fmt.Errorf("update user %q linked identities: source value changed concurrently", record.id)
		}
	}
	if o.direction == legacySubjectForward {
		if _, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB, queryInsertLinkedSubjectIdentifier,
			record.id, link.identifierName(), link.subject, record.deploymentID); err != nil {
			return fmt.Errorf("insert user %q linked identity identifier: %w", record.id, err)
		}
		return nil
	}
	if _, err := execution.Access().Execute(ctx, engine.StoreIDEntityDB, queryDeleteLinkedSubjectIdentifier,
		record.id, record.deploymentID, link.identifierName(), link.subject); err != nil {
		return fmt.Errorf("delete user %q linked identity identifier: %w", record.id, err)
	}
	return nil
}

func (o *legacySubjectOperation) verify(
	ctx context.Context,
	access engine.ReadAccess,
	target engine.ExecutionTarget,
) (engine.VerificationResult, error) {
	records, err := readLegacySubjectRecords(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if len(records) == 0 {
		return engine.VerificationResult{
			State: engine.VerificationTarget, Summary: "No user holds a legacy federated subject",
		}, nil
	}
	connectionIDs, err := o.connections.idsByProvider(ctx)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("read declarative connections: %w", err)
	}
	identifiers, err := readLinkedSubjectIdentifiers(ctx, access, target.DeploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}

	needsWork := 0
	for _, record := range records {
		link, err := legacySubjectLinkFor(record, connectionIDs)
		if err != nil {
			return invalidLegacySubjectVerification(fmt.Sprintf("Legacy federated subject check failed: %v", err)), nil
		}
		hasLink, err := hasLegacySubjectLink(record.systemAttributes, link)
		if err != nil {
			return invalidLegacySubjectVerification(fmt.Sprintf(
				"User %q has an invalid linked identity representation: %v", record.id, err)), nil
		}
		hasIdentifier := hasLinkedSubjectIdentifier(identifiers[legacySubjectKey(record.deploymentID, record.id)], link)
		if o.direction == legacySubjectForward && (!hasLink || !hasIdentifier) ||
			o.direction == legacySubjectRollback && (hasLink || hasIdentifier) {
			needsWork++
		}
	}
	if needsWork > 0 {
		return engine.VerificationResult{
			State: engine.VerificationSource, Summary: fmt.Sprintf(
				"%d of %d users with a legacy federated subject still require work", needsWork, len(records)),
		}, nil
	}
	return engine.VerificationResult{
		State: engine.VerificationTarget, Summary: "Every legacy federated subject has the required representation",
	}, nil
}

// legacySubjectLinkFor derives the link a user's legacy subject must have. It fails for a subject that is not a
// string, one that fits no provider, and one whose provider has no unique declarative connection, so that no
// link is ever guessed.
func legacySubjectLinkFor(
	record legacySubjectRecord,
	connectionIDs map[legacySubjectProvider][]string,
) (legacySubjectLink, error) {
	subject, ok := record.attributes[legacySubjectAttribute].(string)
	if !ok {
		return legacySubjectLink{}, fmt.Errorf("user %q has a %s attribute that is not a string",
			record.id, legacySubjectAttribute)
	}
	provider, err := classifyLegacySubject(subject)
	if err != nil {
		return legacySubjectLink{}, fmt.Errorf("user %q: %w", record.id, err)
	}
	ids := connectionIDs[provider]
	switch len(ids) {
	case 0:
		return legacySubjectLink{}, fmt.Errorf(
			"user %q holds a %s subject but no declarative %s connection is defined", record.id, provider, provider)
	case 1:
		return legacySubjectLink{idpID: ids[0], subject: subject}, nil
	default:
		return legacySubjectLink{}, fmt.Errorf(
			"user %q holds a %s subject but %d declarative %s connections are defined",
			record.id, provider, len(ids), provider)
	}
}

// objectEntry returns the object stored under key. It returns nil when the key is absent or null and create is
// false, and creates the object when create is set. A value of any other shape is an error, since replacing it
// would silently drop what it holds.
func objectEntry(parent map[string]interface{}, key string, create bool) (map[string]interface{}, error) {
	raw, exists := parent[key]
	if !exists || raw == nil {
		if !create {
			return nil, nil
		}
		child := map[string]interface{}{}
		parent[key] = child
		return child, nil
	}
	child, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not an object", key)
	}
	return child, nil
}

func hasLegacySubjectLink(systemAttributes map[string]interface{}, link legacySubjectLink) (bool, error) {
	links, err := objectEntry(systemAttributes, linkedIDsAttribute, false)
	if err != nil || links == nil {
		return false, err
	}
	subjects, err := objectEntry(links, link.idpID, false)
	if err != nil || subjects == nil {
		return false, err
	}
	_, exists := subjects[link.subject]
	return exists, nil
}

// addLegacySubjectLink records the link in the system attributes and reports whether it changed them.
func addLegacySubjectLink(systemAttributes map[string]interface{}, link legacySubjectLink) (bool, error) {
	exists, err := hasLegacySubjectLink(systemAttributes, link)
	if err != nil || exists {
		return false, err
	}
	links, err := objectEntry(systemAttributes, linkedIDsAttribute, true)
	if err != nil {
		return false, err
	}
	subjects, err := objectEntry(links, link.idpID, true)
	if err != nil {
		return false, err
	}
	subjects[link.subject] = map[string]interface{}{}
	return true, nil
}

// removeLegacySubjectLink removes the link from the system attributes and reports whether it changed them.
// Objects left empty by the removal are removed too, which restores the source representation.
func removeLegacySubjectLink(systemAttributes map[string]interface{}, link legacySubjectLink) (bool, error) {
	exists, err := hasLegacySubjectLink(systemAttributes, link)
	if err != nil || !exists {
		return false, err
	}
	links, _ := objectEntry(systemAttributes, linkedIDsAttribute, false)
	subjects, _ := objectEntry(links, link.idpID, false)
	delete(subjects, link.subject)
	if len(subjects) == 0 {
		delete(links, link.idpID)
	}
	if len(links) == 0 {
		delete(systemAttributes, linkedIDsAttribute)
	}
	return true, nil
}

func readLegacySubjectRecords(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) ([]legacySubjectRecord, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryLegacySubjectUsers, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect legacy federated subjects: %w", err)
	}
	records := make([]legacySubjectRecord, 0, len(rows))
	for _, row := range rows {
		record, err := readLegacySubjectRecord(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func readLegacySubjectRecord(row engine.QueryRow) (legacySubjectRecord, error) {
	id, err := row.String("id")
	if err != nil {
		return legacySubjectRecord{}, err
	}
	entityType, err := row.String("type")
	if err != nil {
		return legacySubjectRecord{}, err
	}
	deploymentID, err := row.String("deployment_id")
	if err != nil {
		return legacySubjectRecord{}, err
	}
	attributes, err := readAgentNameJSONObject(row, "attributes")
	if err != nil {
		return legacySubjectRecord{}, fmt.Errorf("read user %q attributes: %w", id, err)
	}
	systemAttributes, err := readAgentNameJSONObject(row, "system_attributes")
	if err != nil {
		return legacySubjectRecord{}, fmt.Errorf("read user %q system attributes: %w", id, err)
	}
	originalAttributes, hasAttributes := row["attributes"]
	if !hasAttributes {
		return legacySubjectRecord{}, fmt.Errorf("read user %q attributes: column is missing", id)
	}
	originalSystemAttributes, hasSystemAttributes := row["system_attributes"]
	if !hasSystemAttributes {
		return legacySubjectRecord{}, fmt.Errorf("read user %q system attributes: column is missing", id)
	}
	return legacySubjectRecord{
		id: id, entityType: entityType, deploymentID: deploymentID, attributes: attributes,
		systemAttributes: systemAttributes, originalAttributes: originalAttributes,
		originalSystemAttributes: originalSystemAttributes,
	}, nil
}

func readLinkedSubjectIdentifiers(
	ctx context.Context,
	access engine.ReadAccess,
	deploymentID string,
) (map[string][]legacySubjectIdentifier, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryLinkedSubjectIdentifiers, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("inspect linked identity identifiers: %w", err)
	}
	result := make(map[string][]legacySubjectIdentifier)
	for _, row := range rows {
		entityID, err := row.String("entity_id")
		if err != nil {
			return nil, err
		}
		rowDeploymentID, err := row.String("deployment_id")
		if err != nil {
			return nil, err
		}
		name, err := row.String("name")
		if err != nil {
			return nil, err
		}
		value, err := row.String("value")
		if err != nil {
			return nil, err
		}
		source, err := row.String("source")
		if err != nil {
			return nil, err
		}
		key := legacySubjectKey(rowDeploymentID, entityID)
		result[key] = append(result[key], legacySubjectIdentifier{name: name, value: value, source: source})
	}
	return result, nil
}

// hasLinkedSubjectIdentifier reports whether the identifier index holds the link, as the sign-in lookup reads it.
func hasLinkedSubjectIdentifier(identifiers []legacySubjectIdentifier, link legacySubjectLink) bool {
	for _, identifier := range identifiers {
		if identifier.name == link.identifierName() && identifier.value == link.subject &&
			identifier.source == "system" {
			return true
		}
	}
	return false
}

func invalidLegacySubjectVerification(summary string) engine.VerificationResult {
	return engine.VerificationResult{
		State:   engine.VerificationInvalid,
		Summary: summary,
		Findings: []engine.Finding{{
			Code:        "invalid_legacy_federated_subject_state",
			Message:     summary,
			Remediation: "Reconcile the legacy subjects and the declarative connections before retrying",
		}},
	}
}

func legacySubjectBatchResult(
	checkpoint legacySubjectCheckpoint,
	completed bool,
) (engine.ResumableBatchResult, error) {
	checkpointData, err := json.Marshal(checkpoint)
	if err != nil {
		return engine.ResumableBatchResult{}, fmt.Errorf("encode legacy subject checkpoint: %w", err)
	}
	return engine.ResumableBatchResult{CheckpointData: checkpointData, Completed: completed}, nil
}

func legacySubjectKey(deploymentID string, entityID string) string {
	return deploymentID + "\x00" + entityID
}
