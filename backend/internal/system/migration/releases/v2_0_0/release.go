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
type Dependencies struct{}

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

func migrationPlan(Dependencies) (engine.MigrationPlan, error) {
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
	// Operations 08 through 10 must remain disabled until the entity runtime-data product change is merged.
	// createEntityRuntimeData, err := newCreateEntityRuntimeDataOperation()
	// if err != nil {
	// 	return engine.MigrationPlan{}, err
	// }
	// addEntityStateDefault, err := newAddEntityStateDefaultOperation()
	// if err != nil {
	// 	return engine.MigrationPlan{}, err
	// }
	// Operation 11 must remain disabled until the SSO session properties product change is merged.
	// addSSOSessionProperties, err := newAddSSOSessionPropertiesOperation()
	// if err != nil {
	// 	return engine.MigrationPlan{}, err
	// }
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
	backfillEntityTypeHandle := newBackfillEntityTypeHandleOperation()
	addEntityTypeHandleUniqueness, err := newAddEntityTypeHandleUniquenessOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	finalizeEntityTypeHandle, err := newFinalizeEntityTypeHandleOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	migrateEntityTypeReferences := newMigrateEntityTypeReferencesOperation()
	migrateInboundClientEntityTypeReferences := newMigrateInboundClientEntityTypeReferencesOperation()
	migrateIDPEntityTypeReferences := newMigrateIDPEntityTypeReferencesOperation()
	migrateAuthorizationPDPEntityTypeReferences := newMigrateAuthorizationPDPEntityTypeReferencesOperation()
	migrateFlowEntityTypeReferences := newMigrateFlowEntityTypeReferencesOperation()
	// Enable operation 10 with operations 08 and 09 after the entity runtime-data product change is merged.
	// removeEntityState, err := newRemoveEntityStateOperation()
	// if err != nil {
	// 	return engine.MigrationPlan{}, err
	// }
	rollbackEntityTypeHandleBackfill := newRollbackEntityTypeHandleBackfillOperation()
	rollbackEntityTypeReferences := newRollbackEntityTypeReferencesOperation()
	rollbackInboundClientEntityTypeReferences := newRollbackInboundClientEntityTypeReferencesOperation()
	rollbackIDPEntityTypeReferences := newRollbackIDPEntityTypeReferencesOperation()
	rollbackAuthorizationPDPEntityTypeReferences := newRollbackAuthorizationPDPEntityTypeReferencesOperation()
	rollbackFlowEntityTypeReferences := newRollbackFlowEntityTypeReferencesOperation()
	return engine.MigrationPlan{
		TargetVersion: "2.0.0",
		Expand: []engine.Operation{
			createAuthorizationPDPConnection,
			addEntityTypeHandle,
			expandEntityTypeLength,
			expandEntityIdentifierPrimaryKey,
			// Enable operations 08 and 09 after the entity runtime-data product change is merged.
			// createEntityRuntimeData,
			// addEntityStateDefault,
			// Enable operation 11 after the SSO session properties product change is merged.
			// addSSOSessionProperties,
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
		},
		Migrate: []engine.Operation{
			backfillEntityTypeHandle,
			addEntityTypeHandleUniqueness,
			migrateEntityTypeReferences,
			migrateInboundClientEntityTypeReferences,
			migrateIDPEntityTypeReferences,
			migrateAuthorizationPDPEntityTypeReferences,
			migrateFlowEntityTypeReferences,
		},
		Contract: append([]engine.Operation{
			finalizeEntityTypeHandle,
			// Enable operation 10 with operations 08 and 09 after the product change is merged.
			// removeEntityState,
		}, primaryKeyContractOperations...),
		Rollback: []engine.RollbackOperation{
			rollbackFlowEntityTypeReferences,
			rollbackAuthorizationPDPEntityTypeReferences,
			rollbackIDPEntityTypeReferences,
			rollbackInboundClientEntityTypeReferences,
			rollbackEntityTypeReferences,
			rollbackEntityTypeHandleBackfill,
		},
	}, nil
}
