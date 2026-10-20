// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowmgt

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const (
	userTypeResolverExecutorName  = "UserTypeResolver"
	agentTypeResolverExecutorName = "AgentTypeResolver"
	allowedUserTypesProperty      = "allowedUserTypes"
	allowedAgentTypesProperty     = "allowedAgentTypes"
)

// normalizeEntityTypeReferences rewrites the allowed user and agent types on the type resolver nodes to
// entity type handles. Flows stored before entity types had handles list the types by name. The nodes of
// every given flow are handled together so that each category needs a single lookup.
func (s *flowStore) normalizeEntityTypeReferences(ctx context.Context, flowNodes ...[]providers.NodeDefinition) error {
	if s.entityTypeNormalizer == nil {
		return nil
	}

	var userTypes, agentTypes []string
	for _, nodes := range flowNodes {
		for _, node := range nodes {
			switch resolverProperty(node) {
			case allowedUserTypesProperty:
				userTypes = append(userTypes, allowedTypes(node, allowedUserTypesProperty)...)
			case allowedAgentTypesProperty:
				agentTypes = append(agentTypes, allowedTypes(node, allowedAgentTypesProperty)...)
			}
		}
	}

	userHandles, err := s.normalizeReferences(ctx, entitytype.TypeCategoryUser, userTypes)
	if err != nil {
		return err
	}
	agentHandles, err := s.normalizeReferences(ctx, entitytype.TypeCategoryAgent, agentTypes)
	if err != nil {
		return err
	}

	for _, nodes := range flowNodes {
		for _, node := range nodes {
			switch resolverProperty(node) {
			case allowedUserTypesProperty:
				replaceAllowedTypes(node, allowedUserTypesProperty, userHandles)
			case allowedAgentTypesProperty:
				replaceAllowedTypes(node, allowedAgentTypesProperty, agentHandles)
			}
		}
	}
	return nil
}

func (s *flowStore) normalizeReferences(
	ctx context.Context, category entitytype.TypeCategory, stored []string,
) (map[string]string, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	handles, err := s.entityTypeNormalizer.NormalizeEntityTypeReferences(ctx, category, stored)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize entity type references: %w", err)
	}
	return handles, nil
}

// resolverProperty returns the node property that lists the allowed entity types, or an empty string
// when the node is not a type resolver.
func resolverProperty(node providers.NodeDefinition) string {
	if node.Executor == nil {
		return ""
	}
	switch node.Executor.Name {
	case userTypeResolverExecutorName:
		return allowedUserTypesProperty
	case agentTypeResolverExecutorName:
		return allowedAgentTypesProperty
	}
	return ""
}

func allowedTypes(node providers.NodeDefinition, property string) []string {
	items, _ := node.Properties[property].([]interface{})
	types := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok && value != "" {
			types = append(types, value)
		}
	}
	return types
}

// replaceAllowedTypes swaps each listed type for its handle when one is known and drops the duplicates
// that result, keeping the original order. Entries that are not strings are left in place.
func replaceAllowedTypes(node providers.NodeDefinition, property string, handles map[string]string) {
	items, ok := node.Properties[property].([]interface{})
	if !ok || len(items) == 0 {
		return
	}
	replaced := make([]interface{}, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		value, isString := item.(string)
		if !isString {
			replaced = append(replaced, item)
			continue
		}
		if handle, found := handles[value]; found {
			value = handle
		}
		if !seen[value] {
			seen[value] = true
			replaced = append(replaced, value)
		}
	}
	node.Properties[property] = replaced
}
