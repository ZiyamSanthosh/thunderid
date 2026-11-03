// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package inboundclient

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/tests/mocks/entitytypemock"
)

func inboundClientRow(entityID string, userTypes, agentTypes []string) map[string]interface{} {
	blob, _ := json.Marshal(inboundClientJSONBlob{AllowedUserTypes: userTypes, AllowedAgentTypes: agentTypes})
	return map[string]interface{}{
		"entity_id":                    entityID,
		"auth_flow_id":                 "flow1",
		"registration_flow_id":         "reg1",
		"is_registration_flow_enabled": "0",
		"recovery_flow_id":             "recovery1",
		"is_recovery_flow_enabled":     "0",
		"theme_id":                     nil,
		"layout_id":                    nil,
		"properties":                   string(blob),
	}
}

func (suite *InboundClientStoreTestSuite) expectInboundClientQuery(row map[string]interface{}) {
	suite.mockDBProvider.On("GetConfigDBClient").Return(suite.mockDBClient, nil).Once()
	suite.mockDBClient.On("QueryContext", mock.Anything, queryGetInboundClientByEntityID, "app1", testServerID).
		Return([]map[string]interface{}{row}, nil).Once()
}

func (suite *InboundClientStoreTestSuite) TestGetInboundClientByEntityID_NormalizesEntityTypeReferences() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(suite.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer", "customer"}).
		Return(map[string]string{"Customer": "customer", "customer": "customer"}, nil).Once()
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryAgent,
		[]string{"Default"}).
		Return(map[string]string{"Default": "default"}, nil).Once()
	suite.store.entityTypeNormalizer = normalizer
	suite.expectInboundClientQuery(inboundClientRow("app1", []string{"Customer", "customer"}, []string{"Default"}))

	client, err := suite.store.GetInboundClientByEntityID(context.Background(), "app1")

	suite.NoError(err)
	suite.Equal([]string{"customer"}, client.AllowedUserTypes)
	suite.Equal([]string{"default"}, client.AllowedAgentTypes)
}

func (suite *InboundClientStoreTestSuite) TestGetInboundClientByEntityID_KeepsUnresolvedEntityTypes() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(suite.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer", "Gone"}).
		Return(map[string]string{"Customer": "customer"}, nil).Once()
	suite.store.entityTypeNormalizer = normalizer
	suite.expectInboundClientQuery(inboundClientRow("app1", []string{"Customer", "Gone"}, nil))

	client, err := suite.store.GetInboundClientByEntityID(context.Background(), "app1")

	suite.NoError(err)
	suite.Equal([]string{"customer", "Gone"}, client.AllowedUserTypes)
	suite.Empty(client.AllowedAgentTypes)
}

func (suite *InboundClientStoreTestSuite) TestGetInboundClientByEntityID_SkipsNormalizerWithoutTypes() {
	suite.store.entityTypeNormalizer = entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(suite.T())
	suite.expectInboundClientQuery(inboundClientRow("app1", nil, nil))

	client, err := suite.store.GetInboundClientByEntityID(context.Background(), "app1")

	suite.NoError(err)
	suite.Empty(client.AllowedUserTypes)
}

func (suite *InboundClientStoreTestSuite) TestGetInboundClientByEntityID_ReturnsNormalizerError() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(suite.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer"}).
		Return(nil, errors.New("lookup failed")).Once()
	suite.store.entityTypeNormalizer = normalizer
	suite.expectInboundClientQuery(inboundClientRow("app1", []string{"Customer"}, nil))

	client, err := suite.store.GetInboundClientByEntityID(context.Background(), "app1")

	suite.Error(err)
	suite.Nil(client)
}

func (suite *InboundClientStoreTestSuite) TestGetInboundClientList_NormalizesAllClientsWithOneLookupPerCategory() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(suite.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer", "Employee"}).
		Return(map[string]string{"Customer": "customer", "Employee": "employee"}, nil).Once()
	suite.store.entityTypeNormalizer = normalizer
	suite.mockDBProvider.On("GetConfigDBClient").Return(suite.mockDBClient, nil).Once()
	suite.mockDBClient.On("QueryContext", mock.Anything, queryGetInboundClientList, testServerID, mock.Anything).
		Return([]map[string]interface{}{
			inboundClientRow("app1", []string{"Customer"}, nil),
			inboundClientRow("app2", []string{"Employee"}, nil),
		}, nil).Once()

	clients, err := suite.store.GetInboundClientList(context.Background(), 100)

	suite.NoError(err)
	suite.Len(clients, 2)
	suite.Equal([]string{"customer"}, clients[0].AllowedUserTypes)
	suite.Equal([]string{"employee"}, clients[1].AllowedUserTypes)
}
