// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package v2_0_0 contains the immutable migration definition shipped with ThunderID 2.0.0.
package v2_0_0

import (
	_ "embed"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

//go:embed release-manifest.json
var manifestData []byte

// Dependencies contains the dependencies available while constructing this release's operations.
// Add narrowly typed providers only when a concrete operation requires them.
type Dependencies struct {
	// DeclarativeEntityTypes lazily provides the read-only entity types defined in declarative resource
	// files, which have no ENTITY_TYPES row. It is used by entity-type reference operations 42 to 44
	// and 47. When nil, those operations map entity types from the database alone.
	DeclarativeEntityTypes func() (DeclarativeEntityTypes, error)
	// DeclarativeConnections lazily provides the connections defined in declarative resource files. It is
	// used by operation 48 and 49 to find the connection that issued each legacy federated subject. When
	// nil, those operations cannot link a user and report the missing connection.
	DeclarativeConnections func() (DeclarativeConnections, error)
	// DeclarativeOrganizationUnits lazily provides the organization units defined in declarative resource
	// files, which have no ORGANIZATION_UNIT row. The organization unit owner operations use it to find
	// the system organization unit, and are blocked until it is declared.
	DeclarativeOrganizationUnits func() (DeclarativeOrganizationUnits, error)
	// SystemOrganizationUnitID is the organization unit the deployment names in server.system_ou_id. It becomes
	// the owner of the rows that had none, and the owner operations are blocked while it is empty or not
	// declared.
	SystemOrganizationUnitID string
}

// Definition returns the manifest and plan owned by ThunderID 2.0.0.
func Definition() (*engine.ReleaseDefinition, error) {
	return DefinitionWithDependencies(Dependencies{})
}

// DefinitionWithDependencies returns the release definition constructed with the supplied dependencies.
func DefinitionWithDependencies(dependencies Dependencies) (*engine.ReleaseDefinition, error) {
	plan, err := migrationPlan(dependencies)
	if err != nil {
		return nil, err
	}
	return engine.NewReleaseDefinition(manifestData, plan)
}

func migrationPlan(dependencies Dependencies) (engine.MigrationPlan, error) {
	declaredEntityTypes := newDeclarativeEntityTypeSource(dependencies.DeclarativeEntityTypes)
	declaredConnections := newDeclarativeConnectionSource(dependencies.DeclarativeConnections)
	declaredOrganizationUnits := newDeclarativeOrganizationUnitSource(
		dependencies.DeclarativeOrganizationUnits, dependencies.SystemOrganizationUnitID)
	createAuthorizationPDPConnection, err := newCreateAuthorizationPDPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityTypeHandle, err := newAddEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityTypeLength, err := newExpandEntityTypeLengthOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityIdentifierPrimaryKey, err := newExpandEntityIdentifierPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createEntityRuntimeData, err := newCreateEntityRuntimeDataOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityStateDefault, err := newAddEntityStateDefaultOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createNotificationTemplate, err := newCreateNotificationTemplateOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createVariable, err := newCreateVariableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createSecret, err := newCreateSecretOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGateway, err := newCreateGatewayOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createConfigurationVersion, err := newCreateConfigurationVersionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayAppliedVersion, err := newCreateGatewayAppliedVersionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayExcludedResource, err := newCreateGatewayExcludedResourceOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicy, err := newCreateResourceSharingPolicyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicyUnit, err := newCreateResourceSharingPolicyUnitOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicyOverlayRule, err := newCreateResourceSharingPolicyOverlayRuleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceOverlayValue, err := newCreateResourceOverlayValueOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityTypesPrimaryKey, err := newExpandEntityTypesPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandRolePrimaryKey, err := newExpandRolePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandThemePrimaryKey, err := newExpandThemePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandLayoutPrimaryKey, err := newExpandLayoutPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandInboundClientPrimaryKey, err := newExpandInboundClientPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandIDPPrimaryKey, err := newExpandIDPPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandNotificationSenderPrimaryKey, err := newExpandNotificationSenderPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCertificatePrimaryKey, err := newExpandCertificatePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandResourceServerPrimaryKey, err := newExpandResourceServerPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandResourcePrimaryKey, err := newExpandResourcePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandActionPrimaryKey, err := newExpandActionPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandFlowPrimaryKey, err := newExpandFlowPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandPresentationDefinitionPrimaryKey, err := newExpandPresentationDefinitionPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCredentialConfigurationPrimaryKey, err := newExpandCredentialConfigurationPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandOrganizationUnitPrimaryKey, err := newExpandOrganizationUnitPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityPrimaryKey, err := newExpandEntityPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandGroupPrimaryKey, err := newExpandGroupPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCertificateReferenceID, err := newExpandCertificateReferenceIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandRevocationCriterionValue, err := newExpandRevocationCriterionValueOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSSOSessionParticipantApplicationIndex, err := newAddSSOSessionParticipantApplicationIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	renameSessionTables, err := newRenameSessionTablesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	renameSessionIndexes, err := newRenameSessionIndexesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionTypeCheck, err := newAddSessionTypeCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	validateSessionTypeCheck, err := newValidateSessionTypeCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionSubjectIndex, err := newAddSessionSubjectIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionProperties, err := newAddSessionPropertiesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createManagedResource, err := newCreateManagedResourceOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	convertAgentOwner, err := newConvertAgentOwnerOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	rollbackConvertAgentOwner, err := newRollbackConvertAgentOwnerOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	var addProjectColumns, addProjectIndexes []engine.Operation
	for _, scoped := range projectScopedTables {
		column, err := newAddProjectIDColumnOperation(scoped)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		addProjectColumns = append(addProjectColumns, column)
		addProjectIndexes = append(addProjectIndexes, newAddProjectIndexOperation(scoped))
	}
	finalizeSessionTables, err := newFinalizeSessionTablesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addInboundClientDeviceGrantFlowID, err := newAddInboundClientDeviceGrantFlowIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createRuntimeStoreDeviceReqPartition, err := newCreateRuntimeStoreDeviceReqPartitionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addRoleAssignmentSharingPolicyID, err := newAddRoleAssignmentSharingPolicyIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	backfillRoleAssignment, err := newBackfillRoleAssignmentOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeRoleAssignment, err := newFinalizeRoleAssignmentOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createUserShareMigrationTable, err := newCreateUserShareMigrationTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayMaster, err := newCreateGatewayMasterOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createProjectTable, err := newCreateProjectTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityProjectID, err := newAddEntityProjectIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addOrganizationUnitProjectID, err := newAddOrganizationUnitProjectIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createHTTPConnection, err := newCreateHTTPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createAuthorizationDetailType, err := newCreateAuthorizationDetailTypeOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayApplyLease, err := newCreateGatewayApplyLeaseOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	dropUserShareMigrationTable, err := newDropUserShareMigrationTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	dropGroupMemberLookupIndex, err := newDropGroupMemberLookupIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	var addOwnerColumns, backfillOwners, finalizeOwners []engine.Operation
	for _, owner := range organizationUnitOwnerTables {
		add, err := newAddOrganizationUnitOwnerColumnOperation(owner)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		backfill, err := newBackfillOrganizationUnitOwnerOperation(owner, declaredOrganizationUnits)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		finalize, err := newFinalizeOrganizationUnitOwnerOperation(owner)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		addOwnerColumns = append(addOwnerColumns, add)
		backfillOwners = append(backfillOwners, backfill)
		finalizeOwners = append(finalizeOwners, finalize)
	}
	primaryKeyContractOperations, err := newLegacyPrimaryKeyContractOperations([]engine.Operation{
		expandEntityTypesPrimaryKey,
		expandRolePrimaryKey,
		expandThemePrimaryKey,
		expandLayoutPrimaryKey,
		expandInboundClientPrimaryKey,
		expandIDPPrimaryKey,
		expandNotificationSenderPrimaryKey,
		expandCertificatePrimaryKey,
		expandResourceServerPrimaryKey,
		expandResourcePrimaryKey,
		expandActionPrimaryKey,
		expandFlowPrimaryKey,
		expandPresentationDefinitionPrimaryKey,
		expandCredentialConfigurationPrimaryKey,
		expandOrganizationUnitPrimaryKey,
		expandEntityPrimaryKey,
		expandGroupPrimaryKey,
	})
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	backfillEntityTypeHandle := newBackfillEntityTypeHandleOperation(declaredEntityTypes)
	backfillEntityTypeHandleCatchUp := newBackfillEntityTypeHandleCatchUpOperation(declaredEntityTypes)
	addEntityTypeHandleUniqueness, err := newAddEntityTypeHandleUniquenessOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeEntityTypeHandle, err := newFinalizeEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	linkLegacyFederatedSubjects := newLinkLegacyFederatedSubjectsOperation(declaredConnections)
	linkLegacyFederatedSubjectsCatchUp := newLinkLegacyFederatedSubjectsCatchUpOperation(declaredConnections)
	removeLegacySubjectAttribute := newRemoveLegacySubjectAttributeOperation(
		declaredConnections, declaredEntityTypes)
	migrateEntityTypeReferences := newMigrateEntityTypeReferencesOperation(declaredEntityTypes)
	migrateInboundClientEntityTypeReferences := newMigrateInboundClientEntityTypeReferencesOperation(declaredEntityTypes)
	migrateIDPEntityTypeReferences := newMigrateIDPEntityTypeReferencesOperation(declaredEntityTypes)
	migrateFlowEntityTypeReferences := newMigrateFlowEntityTypeReferencesOperation(declaredEntityTypes)
	migrateDefaultAgentNameSchema := newMigrateDefaultAgentNameSchemaOperation()
	migrateAgentNamesToAttributes := newMigrateAgentNamesToAttributesOperation(declaredEntityTypes)
	removeEntityState, err := newRemoveEntityStateOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	rollbackEntityTypeHandleBackfill := newRollbackEntityTypeHandleBackfillOperation()
	rollbackLinkLegacyFederatedSubjects := newRollbackLinkLegacyFederatedSubjectsOperation(declaredConnections)
	rollbackEntityTypeReferences := newRollbackEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackInboundClientEntityTypeReferences := newRollbackInboundClientEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackIDPEntityTypeReferences := newRollbackIDPEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackFlowEntityTypeReferences := newRollbackFlowEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackDefaultAgentNameSchema := newRollbackDefaultAgentNameSchemaOperation()
	rollbackAgentNamesToAttributes := newRollbackAgentNamesToAttributesOperation(declaredEntityTypes)
	plan := engine.MigrationPlan{
		TargetVersion: "2.0.0-m1",
		Expand: []engine.Operation{
			createAuthorizationPDPConnection,
			addEntityTypeHandle,
			expandEntityTypeLength,
			expandEntityIdentifierPrimaryKey,
			createEntityRuntimeData,
			addEntityStateDefault,
			createNotificationTemplate,
			createVariable,
			createSecret,
			createGateway,
			createConfigurationVersion,
			createGatewayAppliedVersion,
			createGatewayExcludedResource,
			createResourceSharingPolicy,
			createResourceSharingPolicyUnit,
			createResourceSharingPolicyOverlayRule,
			createResourceOverlayValue,
			expandEntityTypesPrimaryKey,
			expandRolePrimaryKey,
			expandThemePrimaryKey,
			expandLayoutPrimaryKey,
			expandInboundClientPrimaryKey,
			expandIDPPrimaryKey,
			expandNotificationSenderPrimaryKey,
			expandCertificatePrimaryKey,
			expandResourceServerPrimaryKey,
			expandResourcePrimaryKey,
			expandActionPrimaryKey,
			expandFlowPrimaryKey,
			expandPresentationDefinitionPrimaryKey,
			expandCredentialConfigurationPrimaryKey,
			expandOrganizationUnitPrimaryKey,
			expandEntityPrimaryKey,
			expandGroupPrimaryKey,
			expandCertificateReferenceID,
			expandRevocationCriterionValue,
			addSSOSessionParticipantApplicationIndex,
			renameSessionTables,
			renameSessionIndexes,
			addSessionTypeCheck,
			validateSessionTypeCheck,
			addSessionSubjectIndex,
			addSessionProperties,
			createProjectTable,
			addEntityProjectID,
			addOrganizationUnitProjectID,
			newAddEntityProjectIndexOperation(),
			newAddEntityOwnerIndexOperation(),
			newAddOrganizationUnitProjectIndexOperation(),
			newAddCredentialConfigurationOUIndexOperation(),
			newAddPresentationDefinitionOUIndexOperation(),
			newAddEntityKindIndexOperation(),
			createManagedResource,
			createGatewayMaster,
			createHTTPConnection,
			createAuthorizationDetailType,
			createGatewayApplyLease,
			addInboundClientDeviceGrantFlowID,
			createRuntimeStoreDeviceReqPartition,
			addRoleAssignmentSharingPolicyID,
			newWidenRoleAssignmentPrimaryKeyOperation(),
			newAddRoleAssignmentAssigneeIndexOperation(),
			createUserShareMigrationTable,
			newSnapshotDuplicateUsersOperation(),
			newCreateOriginalUserSharePoliciesOperation(),
			newAddOriginalsToDuplicateGroupsOperation(),
			backfillEntityTypeHandle,
			addEntityTypeHandleUniqueness,
			linkLegacyFederatedSubjects,
		},
		Migrate: []engine.Operation{
			backfillEntityTypeHandleCatchUp,
			linkLegacyFederatedSubjectsCatchUp,
			migrateEntityTypeReferences,
			migrateInboundClientEntityTypeReferences,
			migrateIDPEntityTypeReferences,
			migrateFlowEntityTypeReferences,
			migrateDefaultAgentNameSchema,
			migrateAgentNamesToAttributes,
			newRehomeOriginalUsersOperation(),
			newMergeDuplicateLinkedAccountsOperation(),
		},
		Contract: append([]engine.Operation{
			finalizeEntityTypeHandle,
			removeLegacySubjectAttribute,
			removeEntityState,
		}, append(primaryKeyContractOperations, finalizeSessionTables)...),
		Rollback: []engine.RollbackOperation{
			rollbackConvertAgentOwner,
			newRollbackMergeDuplicateLinkedAccountsOperation(),
			newRollbackRehomeOriginalUsersOperation(),
			rollbackAgentNamesToAttributes,
			rollbackDefaultAgentNameSchema,
			rollbackFlowEntityTypeReferences,
			rollbackIDPEntityTypeReferences,
			rollbackInboundClientEntityTypeReferences,
			rollbackEntityTypeReferences,
			rollbackLinkLegacyFederatedSubjects,
			rollbackEntityTypeHandleBackfill,
			newRollbackAddOriginalsToDuplicateGroupsOperation(),
			newRollbackCreateOriginalUserSharePoliciesOperation(),
			newRollbackSnapshotDuplicateUsersOperation(),
		},
	}
	plan.Expand = append(plan.Expand, addOwnerColumns...)
	plan.Expand = append(plan.Expand, addProjectColumns...)
	plan.Expand = append(plan.Expand, addProjectIndexes...)
	plan.Migrate = append(plan.Migrate, backfillOwners...)
	plan.Migrate = append(plan.Migrate, backfillRoleAssignment, convertAgentOwner)
	plan.Contract = append(plan.Contract, finalizeOwners...)
	plan.Contract = append(plan.Contract,
		finalizeRoleAssignment,
		newDeleteDuplicateUserSessionsOperation(),
		newDeleteDuplicateUserAssignmentsOperation(),
		newDeleteDuplicateUsersOperation(),
		dropUserShareMigrationTable,
		dropGroupMemberLookupIndex,
		newAddAgentTypeOrganizationUnitIndexOperation(),
	)
	return plan, nil
}
