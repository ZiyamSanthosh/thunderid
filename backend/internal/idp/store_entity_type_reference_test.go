// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"context"
	"errors"

	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/entitytypemock"
)

const legacyAttributeConfiguration = `{
	"userTypeResolution":{
		"default":"Customer",
		"externalAttribute":"group",
		"valueMapping":{"staff":"Employee","guest":"Gone"}
	},
	"userTypeAttributeMappings":[
		{"userType":"Customer","attributes":[]},
		{"userType":"Employee","attributes":[]}
	]
}`

func idpRow(id, attributeConfiguration string) map[string]interface{} {
	return map[string]interface{}{
		"id":                      id,
		"name":                    "IDP " + id,
		"description":             "",
		"type":                    "OIDC",
		"properties":              `{"client_id":{"value":"test","isSecret":false}}`,
		"attribute_configuration": attributeConfiguration,
	}
}

func (s *IDPStoreTestSuite) assertNormalized(configuration *providers.AttributeConfiguration) {
	s.NotNil(configuration)
	s.Equal("customer", configuration.UserTypeResolution.Default)
	// Only the values name user types. The keys are values of the external attribute.
	s.Equal(map[string]string{"staff": "employee", "guest": "Gone"}, configuration.UserTypeResolution.ValueMapping)
	s.Equal("customer", configuration.UserTypeAttributeMappings[0].UserType)
	s.Equal("employee", configuration.UserTypeAttributeMappings[1].UserType)
}

func (s *IDPStoreTestSuite) expectLegacyNormalization() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, mock.Anything).
		Return(map[string]string{"Customer": "customer", "Employee": "employee"}, nil).Once()
	s.store.entityTypeNormalizer = normalizer
}

func (s *IDPStoreTestSuite) TestGetIdentityProvider_NormalizesEntityTypeReferences() {
	s.expectLegacyNormalization()
	s.mockDBProvider.On("GetConfigDBClient").Return(s.mockDBClient, nil)
	s.mockDBClient.On("QueryContext", context.Background(), queryGetIdentityProviderByID,
		"idp-1", testDeploymentID).
		Return([]map[string]interface{}{idpRow("idp-1", legacyAttributeConfiguration)}, nil)

	idp, err := s.store.GetIdentityProvider(context.Background(), "idp-1")

	s.NoError(err)
	s.assertNormalized(idp.AttributeConfiguration)
}

func (s *IDPStoreTestSuite) TestGetIdentityProviderByName_NormalizesEntityTypeReferences() {
	s.expectLegacyNormalization()
	s.mockDBProvider.On("GetConfigDBClient").Return(s.mockDBClient, nil)
	s.mockDBClient.On("QueryContext", context.Background(), queryGetIdentityProviderByName,
		"IDP idp-1", testDeploymentID).
		Return([]map[string]interface{}{idpRow("idp-1", legacyAttributeConfiguration)}, nil)

	idp, err := s.store.GetIdentityProviderByName(context.Background(), "IDP idp-1")

	s.NoError(err)
	s.assertNormalized(idp.AttributeConfiguration)
}

func (s *IDPStoreTestSuite) TestGetIdentityProvidersByProperty_NormalizesAllProvidersWithOneLookup() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, mock.Anything).
		Return(map[string]string{"Customer": "customer", "Employee": "employee"}, nil).Once()
	s.store.entityTypeNormalizer = normalizer
	s.mockDBProvider.On("GetConfigDBClient").Return(s.mockDBClient, nil)
	s.mockDBClient.On("QueryContext", context.Background(), queryGetIdentityProvidersByProperty,
		"issuer", "https://idp.example.com", testDeploymentID).
		Return([]map[string]interface{}{
			idpRow("idp-1", legacyAttributeConfiguration),
			idpRow("idp-2", `{"userTypeResolution":{"default":"Employee"}}`),
		}, nil)

	idps, err := s.store.GetIdentityProvidersByProperty(context.Background(), "issuer", "https://idp.example.com")

	s.NoError(err)
	s.Len(idps, 2)
	s.assertNormalized(idps[0].AttributeConfiguration)
	s.Equal("employee", idps[1].AttributeConfiguration.UserTypeResolution.Default)
}

func (s *IDPStoreTestSuite) TestGetIdentityProvider_SkipsNormalizerWithoutUserTypes() {
	s.store.entityTypeNormalizer = entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	s.mockDBProvider.On("GetConfigDBClient").Return(s.mockDBClient, nil)
	s.mockDBClient.On("QueryContext", context.Background(), queryGetIdentityProviderByID,
		"idp-1", testDeploymentID).
		Return([]map[string]interface{}{idpRow("idp-1", "")}, nil)

	idp, err := s.store.GetIdentityProvider(context.Background(), "idp-1")

	s.NoError(err)
	s.Nil(idp.AttributeConfiguration)
}

func (s *IDPStoreTestSuite) TestGetIdentityProvider_ReturnsNormalizerError() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, mock.Anything).
		Return(nil, errors.New("lookup failed")).Once()
	s.store.entityTypeNormalizer = normalizer
	s.mockDBProvider.On("GetConfigDBClient").Return(s.mockDBClient, nil)
	s.mockDBClient.On("QueryContext", context.Background(), queryGetIdentityProviderByID,
		"idp-1", testDeploymentID).
		Return([]map[string]interface{}{idpRow("idp-1", legacyAttributeConfiguration)}, nil)

	idp, err := s.store.GetIdentityProvider(context.Background(), "idp-1")

	s.Error(err)
	s.Nil(idp)
}
