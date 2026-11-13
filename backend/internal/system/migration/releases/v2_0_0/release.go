// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package v2_0_0 contains the immutable migration definition shipped with ThunderID 2.0.0.
package v2_0_0

import (
	_ "embed"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/internal/system/migration/operations"
)

//go:embed release-manifest.json
var manifestData []byte

// Definition returns the manifest and plan owned by ThunderID 2.0.0.
func Definition() (*engine.ReleaseDefinition, error) {
	return DefinitionWithDependencies(operations.Dependencies{})
}

// DefinitionWithDependencies returns the release definition constructed with the supplied dependencies.
func DefinitionWithDependencies(dependencies operations.Dependencies) (*engine.ReleaseDefinition, error) {
	plan, err := migrationPlan(dependencies)
	if err != nil {
		return nil, err
	}
	return engine.NewReleaseDefinition(manifestData, plan)
}

//nolint:gocyclo // The plan lists every operation of the release in order, one construction each.
func migrationPlan(dependencies operations.Dependencies) (engine.MigrationPlan, error) {
	declaredEntityTypes := operations.NewDeclarativeEntityTypeSource(dependencies.DeclarativeEntityTypes)
	declaredConnections := operations.NewDeclarativeConnectionSource(dependencies.DeclarativeConnections)
	declaredOrganizationUnits := operations.NewDeclarativeOrganizationUnitSource(
		dependencies.DeclarativeOrganizationUnits, dependencies.SystemOrganizationUnitID)
	createAuthorizationPDPConnection, err := operations.NewCreateAuthorizationPDPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityTypeHandle, err := operations.NewAddEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityTypeLength, err := operations.NewExpandEntityTypeLengthOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityIdentifierPrimaryKey, err := operations.NewExpandEntityIdentifierPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createEntityRuntimeData, err := operations.NewCreateEntityRuntimeDataOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityStateDefault, err := operations.NewAddEntityStateDefaultOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createNotificationTemplate, err := operations.NewCreateNotificationTemplateOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createVariable, err := operations.NewCreateVariableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createSecret, err := operations.NewCreateSecretOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGateway, err := operations.NewCreateGatewayOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createConfigurationVersion, err := operations.NewCreateConfigurationVersionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayAppliedVersion, err := operations.NewCreateGatewayAppliedVersionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayExcludedResource, err := operations.NewCreateGatewayExcludedResourceOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicy, err := operations.NewCreateResourceSharingPolicyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicyUnit, err := operations.NewCreateResourceSharingPolicyUnitOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceSharingPolicyOverlayRule, err := operations.NewCreateResourceSharingPolicyOverlayRuleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createResourceOverlayValue, err := operations.NewCreateResourceOverlayValueOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityTypesPrimaryKey, err := operations.NewExpandEntityTypesPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandRolePrimaryKey, err := operations.NewExpandRolePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandThemePrimaryKey, err := operations.NewExpandThemePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandLayoutPrimaryKey, err := operations.NewExpandLayoutPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandInboundClientPrimaryKey, err := operations.NewExpandInboundClientPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandIDPPrimaryKey, err := operations.NewExpandIDPPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandNotificationSenderPrimaryKey, err := operations.NewExpandNotificationSenderPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCertificatePrimaryKey, err := operations.NewExpandCertificatePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandResourceServerPrimaryKey, err := operations.NewExpandResourceServerPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandResourcePrimaryKey, err := operations.NewExpandResourcePrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandActionPrimaryKey, err := operations.NewExpandActionPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandFlowPrimaryKey, err := operations.NewExpandFlowPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandPresentationDefinitionPrimaryKey, err := operations.NewExpandPresentationDefinitionPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCredentialConfigurationPrimaryKey, err := operations.NewExpandCredentialConfigurationPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandOrganizationUnitPrimaryKey, err := operations.NewExpandOrganizationUnitPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandEntityPrimaryKey, err := operations.NewExpandEntityPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandGroupPrimaryKey, err := operations.NewExpandGroupPrimaryKeyOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandCertificateReferenceID, err := operations.NewExpandCertificateReferenceIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	expandRevocationCriterionValue, err := operations.NewExpandRevocationCriterionValueOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSSOSessionParticipantApplicationIndex, err := operations.NewAddSSOSessionParticipantApplicationIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	renameSessionTables, err := operations.NewRenameSessionTablesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	renameSessionIndexes, err := operations.NewRenameSessionIndexesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionTypeCheck, err := operations.NewAddSessionTypeCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	validateSessionTypeCheck, err := operations.NewValidateSessionTypeCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionSubjectIndex, err := operations.NewAddSessionSubjectIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addSessionProperties, err := operations.NewAddSessionPropertiesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createManagedResource, err := operations.NewCreateManagedResourceOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	convertAgentOwner, err := operations.NewConvertAgentOwnerOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	rollbackConvertAgentOwner, err := operations.NewRollbackConvertAgentOwnerOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	var addProjectColumns, addProjectIndexes []engine.Operation
	for _, scoped := range operations.ProjectScopedTables {
		column, err := operations.NewAddProjectIDColumnOperation(scoped)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		addProjectColumns = append(addProjectColumns, column)
		addProjectIndexes = append(addProjectIndexes, operations.NewAddProjectIndexOperation(scoped))
	}
	finalizeSessionTables, err := operations.NewFinalizeSessionTablesOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addInboundClientDeviceGrantFlowID, err := operations.NewAddInboundClientDeviceGrantFlowIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createRuntimeStoreDeviceReqPartition, err := operations.NewCreateRuntimeStoreDeviceReqPartitionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addRoleAssignmentSharingPolicyID, err := operations.NewAddRoleAssignmentSharingPolicyIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	backfillRoleAssignment, err := operations.NewBackfillRoleAssignmentOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeRoleAssignment, err := operations.NewFinalizeRoleAssignmentOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createUserShareMigrationTable, err := operations.NewCreateUserShareMigrationTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayMaster, err := operations.NewCreateGatewayMasterOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createProjectTable, err := operations.NewCreateProjectTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addEntityProjectID, err := operations.NewAddEntityProjectIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	addOrganizationUnitProjectID, err := operations.NewAddOrganizationUnitProjectIDOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createHTTPConnection, err := operations.NewCreateHTTPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createAuthorizationDetailType, err := operations.NewCreateAuthorizationDetailTypeOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createGatewayApplyLease, err := operations.NewCreateGatewayApplyLeaseOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	dropUserShareMigrationTable, err := operations.NewDropUserShareMigrationTableOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	dropGroupMemberLookupIndex, err := operations.NewDropGroupMemberLookupIndexOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	var addOwnerColumns, backfillOwners, finalizeOwners []engine.Operation
	for _, owner := range operations.OrganizationUnitOwnerTables {
		add, err := operations.NewAddOrganizationUnitOwnerColumnOperation(owner)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		backfill, err := operations.NewBackfillOrganizationUnitOwnerOperation(owner, declaredOrganizationUnits)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		finalize, err := operations.NewFinalizeOrganizationUnitOwnerOperation(owner)
		if err != nil {
			return engine.MigrationPlan{}, err
		}
		addOwnerColumns = append(addOwnerColumns, add)
		backfillOwners = append(backfillOwners, backfill)
		finalizeOwners = append(finalizeOwners, finalize)
	}
	primaryKeyContractOperations, err := operations.NewLegacyPrimaryKeyContractOperations([]engine.Operation{
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
	backfillEntityTypeHandle := operations.NewBackfillEntityTypeHandleOperation(declaredEntityTypes)
	backfillEntityTypeHandleCatchUp := operations.NewBackfillEntityTypeHandleCatchUpOperation(declaredEntityTypes)
	addEntityTypeHandleUniqueness, err := operations.NewAddEntityTypeHandleUniquenessOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeEntityTypeHandle, err := operations.NewFinalizeEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	linkLegacyFederatedSubjects := operations.NewLinkLegacyFederatedSubjectsOperation(declaredConnections)
	linkLegacyFederatedSubjectsCatchUp := operations.NewLinkLegacyFederatedSubjectsCatchUpOperation(declaredConnections)
	removeLegacySubjectAttribute := operations.NewRemoveLegacySubjectAttributeOperation(
		declaredConnections, declaredEntityTypes)
	migrateEntityTypeReferences := operations.NewMigrateEntityTypeReferencesOperation(declaredEntityTypes)
	migrateInboundClientEntityTypeReferences := operations.NewMigrateInboundClientEntityTypeReferencesOperation(
		declaredEntityTypes,
	)
	migrateIDPEntityTypeReferences := operations.NewMigrateIDPEntityTypeReferencesOperation(declaredEntityTypes)
	migrateFlowEntityTypeReferences := operations.NewMigrateFlowEntityTypeReferencesOperation(declaredEntityTypes)
	migrateDefaultAgentNameSchema := operations.NewMigrateDefaultAgentNameSchemaOperation()
	migrateAgentNamesToAttributes := operations.NewMigrateAgentNamesToAttributesOperation(declaredEntityTypes)
	removeEntityState, err := operations.NewRemoveEntityStateOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	rollbackEntityTypeHandleBackfill := operations.NewRollbackEntityTypeHandleBackfillOperation()
	rollbackLinkLegacyFederatedSubjects := operations.NewRollbackLinkLegacyFederatedSubjectsOperation(
		declaredConnections)
	rollbackEntityTypeReferences := operations.NewRollbackEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackInboundClientEntityTypeReferences := operations.NewRollbackInboundClientEntityTypeReferencesOperation(
		declaredEntityTypes,
	)
	rollbackIDPEntityTypeReferences := operations.NewRollbackIDPEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackFlowEntityTypeReferences := operations.NewRollbackFlowEntityTypeReferencesOperation(declaredEntityTypes)
	rollbackDefaultAgentNameSchema := operations.NewRollbackDefaultAgentNameSchemaOperation()
	rollbackAgentNamesToAttributes := operations.NewRollbackAgentNamesToAttributesOperation(declaredEntityTypes)
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
			operations.NewAddEntityProjectIndexOperation(),
			operations.NewAddEntityOwnerIndexOperation(),
			operations.NewAddOrganizationUnitProjectIndexOperation(),
			operations.NewAddCredentialConfigurationOUIndexOperation(),
			operations.NewAddPresentationDefinitionOUIndexOperation(),
			operations.NewAddEntityKindIndexOperation(),
			createManagedResource,
			createGatewayMaster,
			createHTTPConnection,
			createAuthorizationDetailType,
			createGatewayApplyLease,
			addInboundClientDeviceGrantFlowID,
			createRuntimeStoreDeviceReqPartition,
			addRoleAssignmentSharingPolicyID,
			operations.NewWidenRoleAssignmentPrimaryKeyOperation(),
			operations.NewAddRoleAssignmentAssigneeIndexOperation(),
			createUserShareMigrationTable,
			operations.NewSnapshotDuplicateUsersOperation(),
			operations.NewCreateOriginalUserSharePoliciesOperation(),
			operations.NewAddOriginalsToDuplicateGroupsOperation(),
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
			operations.NewRehomeOriginalUsersOperation(),
			operations.NewMergeDuplicateLinkedAccountsOperation(),
		},
		Contract: append([]engine.Operation{
			finalizeEntityTypeHandle,
			removeLegacySubjectAttribute,
			removeEntityState,
		}, append(primaryKeyContractOperations, finalizeSessionTables)...),
		Rollback: []engine.RollbackOperation{
			rollbackConvertAgentOwner,
			operations.NewRollbackMergeDuplicateLinkedAccountsOperation(),
			operations.NewRollbackRehomeOriginalUsersOperation(),
			rollbackAgentNamesToAttributes,
			rollbackDefaultAgentNameSchema,
			rollbackFlowEntityTypeReferences,
			rollbackIDPEntityTypeReferences,
			rollbackInboundClientEntityTypeReferences,
			rollbackEntityTypeReferences,
			rollbackLinkLegacyFederatedSubjects,
			rollbackEntityTypeHandleBackfill,
			operations.NewRollbackAddOriginalsToDuplicateGroupsOperation(),
			operations.NewRollbackCreateOriginalUserSharePoliciesOperation(),
			operations.NewRollbackSnapshotDuplicateUsersOperation(),
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
		operations.NewDeleteDuplicateUserSessionsOperation(),
		operations.NewDeleteDuplicateUserAssignmentsOperation(),
		operations.NewDeleteDuplicateUsersOperation(),
		dropUserShareMigrationTable,
		dropGroupMemberLookupIndex,
		operations.NewAddAgentTypeOrganizationUnitIndexOperation(),
	)
	return plan, nil
}
