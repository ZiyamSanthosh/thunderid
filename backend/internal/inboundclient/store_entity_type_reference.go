// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package inboundclient

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/entitytype"
	inboundmodel "github.com/thunder-id/thunderid/internal/inboundclient/model"
)

// normalizeEntityTypeReferences rewrites the allowed user and agent types of the clients to entity type
// handles. Clients stored before entity types had handles list the types by name.
func (st *store) normalizeEntityTypeReferences(ctx context.Context, clients []*inboundmodel.InboundClient) error {
	if st.entityTypeNormalizer == nil {
		return nil
	}

	var userTypes, agentTypes []string
	for _, c := range clients {
		userTypes = append(userTypes, c.AllowedUserTypes...)
		agentTypes = append(agentTypes, c.AllowedAgentTypes...)
	}

	userHandles, err := st.normalizeReferences(ctx, entitytype.TypeCategoryUser, userTypes)
	if err != nil {
		return err
	}
	agentHandles, err := st.normalizeReferences(ctx, entitytype.TypeCategoryAgent, agentTypes)
	if err != nil {
		return err
	}

	for _, c := range clients {
		c.AllowedUserTypes = replaceWithHandles(c.AllowedUserTypes, userHandles)
		c.AllowedAgentTypes = replaceWithHandles(c.AllowedAgentTypes, agentHandles)
	}
	return nil
}

func (st *store) normalizeReferences(
	ctx context.Context, category entitytype.TypeCategory, stored []string,
) (map[string]string, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	handles, err := st.entityTypeNormalizer.NormalizeEntityTypeReferences(ctx, category, stored)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize entity type references: %w", err)
	}
	return handles, nil
}

// replaceWithHandles swaps each value for its handle when one is known and drops the duplicates that
// result, keeping the original order. It returns the input unchanged when it is empty.
func replaceWithHandles(values []string, handles map[string]string) []string {
	if len(values) == 0 {
		return values
	}
	replaced := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if handle, ok := handles[value]; ok {
			value = handle
		}
		if !seen[value] {
			seen[value] = true
			replaced = append(replaced, value)
		}
	}
	return replaced
}
