// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// This file is temporary compatibility code for references that still hold entity type names. It is
// removed together with the entity type reference normalizer in a later release.

package flowmgt

import (
	"context"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/internal/flow/executormeta"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const (
	allowedUserTypesProperty  = "allowedUserTypes"
	allowedAgentTypesProperty = "allowedAgentTypes"
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

	userHandles, err := entitytype.ResolveReferences(ctx, s.entityTypeNormalizer, entitytype.TypeCategoryUser, userTypes)
	if err != nil {
		return err
	}
	agentHandles, err := entitytype.ResolveReferences(ctx, s.entityTypeNormalizer, entitytype.TypeCategoryAgent, agentTypes)
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

// resolverProperty returns the node property that lists the allowed entity types, or an empty string
// when the node is not a type resolver.
func resolverProperty(node providers.NodeDefinition) string {
	if node.Executor == nil {
		return ""
	}
	switch node.Executor.Name {
	case executormeta.ExecutorNameUserTypeResolver:
		return allowedUserTypesProperty
	case executormeta.ExecutorNameAgentTypeResolver:
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
// that result, keeping the original order. Entries that are not strings are kept after the types.
func replaceAllowedTypes(node providers.NodeDefinition, property string, handles map[string]string) {
	items, ok := node.Properties[property].([]interface{})
	if !ok || len(items) == 0 {
		return
	}
	types := make([]string, 0, len(items))
	var others []interface{}
	for _, item := range items {
		if value, isString := item.(string); isString {
			types = append(types, value)
		} else {
			others = append(others, item)
		}
	}

	replaced := make([]interface{}, 0, len(items))
	for _, value := range entitytype.ReplaceWithHandles(types, handles) {
		replaced = append(replaced, value)
	}
	node.Properties[property] = append(replaced, others...)
}
