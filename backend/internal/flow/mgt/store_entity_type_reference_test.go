// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowmgt

import (
	"context"
	"errors"

	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/entitytypemock"
)

const (
	resolverNodesJSON = `[
		{"id":"START","type":"START"},
		{"id":"users","type":"TASK_EXECUTION","executor":{"name":"UserTypeResolver"},
		 "properties":{"allowedUserTypes":["Customer","customer","Gone"]}},
		{"id":"agents","type":"TASK_EXECUTION","executor":{"name":"AgentTypeResolver"},
		 "properties":{"allowedAgentTypes":["Default"]}},
		{"id":"other","type":"TASK_EXECUTION","executor":{"name":"OtherExecutor"},
		 "properties":{"allowedUserTypes":["Customer"]}}
	]`
)

func flowRow(nodes string) map[string]interface{} {
	return map[string]interface{}{
		colFlowID:        "flow-123",
		colHandle:        "test-handle",
		colName:          "Test Flow",
		colFlowType:      string(providers.FlowTypeAuthentication),
		colActiveVersion: int64(1),
		colVersion:       int64(1),
		colNodes:         nodes,
		colCreatedAt:     "2025-01-01T00:00:00Z",
		colUpdatedAt:     "2025-01-01T00:00:00Z",
	}
}

func (s *FlowStoreTestSuite) expectResolverNormalization() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer", "customer", "Gone"}).
		Return(map[string]string{"Customer": "customer", "customer": "customer"}, nil).Once()
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryAgent,
		[]string{"Default"}).
		Return(map[string]string{"Default": "default"}, nil).Once()
	s.store.entityTypeNormalizer = normalizer
}

func (s *FlowStoreTestSuite) assertResolverNodesNormalized(nodes []providers.NodeDefinition) {
	s.Len(nodes, 4)
	s.Equal([]interface{}{"customer", "Gone"}, nodes[1].Properties["allowedUserTypes"])
	s.Equal([]interface{}{"default"}, nodes[2].Properties["allowedAgentTypes"])
	// Only the type resolver executors hold entity type references.
	s.Equal([]interface{}{"Customer"}, nodes[3].Properties["allowedUserTypes"])
}

func (s *FlowStoreTestSuite) TestGetFlowByID_NormalizesEntityTypeReferences() {
	s.expectResolverNormalization()
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryGetFlow, "flow-123", "test-deployment").
		Return([]map[string]interface{}{flowRow(resolverNodesJSON)}, nil).Once()

	flow, err := s.store.GetFlowByID(context.Background(), "flow-123")

	s.NoError(err)
	s.assertResolverNodesNormalized(flow.Nodes)
}

func (s *FlowStoreTestSuite) TestGetFlowByHandle_NormalizesEntityTypeReferences() {
	s.expectResolverNormalization()
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryGetFlowByHandle, "test-handle",
		string(providers.FlowTypeAuthentication), "test-deployment").
		Return([]map[string]interface{}{flowRow(resolverNodesJSON)}, nil).Once()

	flow, err := s.store.GetFlowByHandle(context.Background(), "test-handle", providers.FlowTypeAuthentication)

	s.NoError(err)
	s.assertResolverNodesNormalized(flow.Nodes)
}

func (s *FlowStoreTestSuite) TestGetFlowVersion_NormalizesEntityTypeReferences() {
	s.expectResolverNormalization()
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryGetFlowVersionWithMetadata,
		"flow-123", 1, "test-deployment").
		Return([]map[string]interface{}{flowRow(resolverNodesJSON)}, nil).Once()

	version, err := s.store.GetFlowVersion(context.Background(), "flow-123", 1)

	s.NoError(err)
	s.assertResolverNodesNormalized(version.Nodes)
}

func (s *FlowStoreTestSuite) TestListActiveFlowsWithNodes_NormalizesAllFlowsWithOneLookupPerCategory() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		[]string{"Customer", "Employee"}).
		Return(map[string]string{"Customer": "customer", "Employee": "employee"}, nil).Once()
	s.store.entityTypeNormalizer = normalizer
	first := `[{"id":"u","type":"TASK_EXECUTION","executor":{"name":"UserTypeResolver"},
		"properties":{"allowedUserTypes":["Customer"]}}]`
	second := `[{"id":"u","type":"TASK_EXECUTION","executor":{"name":"UserTypeResolver"},
		"properties":{"allowedUserTypes":["Employee"]}}]`
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryListActiveFlowsWithNodes, "test-deployment").
		Return([]map[string]interface{}{flowRow(first), flowRow(second)}, nil).Once()

	flows, err := s.store.ListActiveFlowsWithNodes(context.Background())

	s.NoError(err)
	s.Len(flows, 2)
	s.Equal([]interface{}{"customer"}, flows[0].Nodes[0].Properties["allowedUserTypes"])
	s.Equal([]interface{}{"employee"}, flows[1].Nodes[0].Properties["allowedUserTypes"])
}

func (s *FlowStoreTestSuite) TestGetFlowByID_SkipsNormalizerWithoutResolverNodes() {
	s.store.entityTypeNormalizer = entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryGetFlow, "flow-123", "test-deployment").
		Return([]map[string]interface{}{flowRow(`[{"id":"START","type":"START"}]`)}, nil).Once()

	flow, err := s.store.GetFlowByID(context.Background(), "flow-123")

	s.NoError(err)
	s.Len(flow.Nodes, 1)
}

func (s *FlowStoreTestSuite) TestGetFlowByID_ReturnsNormalizerError() {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(s.T())
	normalizer.On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
		mock.Anything).Return(nil, errors.New("lookup failed")).Once()
	s.store.entityTypeNormalizer = normalizer
	s.mockDBProvider.EXPECT().GetConfigDBClient().Return(s.mockDBClient, nil)
	s.mockDBClient.EXPECT().QueryContext(mock.Anything, queryGetFlow, "flow-123", "test-deployment").
		Return([]map[string]interface{}{flowRow(resolverNodesJSON)}, nil).Once()

	flow, err := s.store.GetFlowByID(context.Background(), "flow-123")

	s.Error(err)
	s.Nil(flow)
}
