// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// This file is temporary compatibility code for references that still hold entity type names. It is
// removed together with the entity type reference normalizer in a later release.

package inboundclient

import (
	"context"

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

	userHandles, err := entitytype.ResolveReferences(ctx, st.entityTypeNormalizer, entitytype.TypeCategoryUser, userTypes)
	if err != nil {
		return err
	}
	agentHandles, err := entitytype.ResolveReferences(ctx, st.entityTypeNormalizer, entitytype.TypeCategoryAgent, agentTypes)
	if err != nil {
		return err
	}

	for _, c := range clients {
		c.AllowedUserTypes = entitytype.ReplaceWithHandles(c.AllowedUserTypes, userHandles)
		c.AllowedAgentTypes = entitytype.ReplaceWithHandles(c.AllowedAgentTypes, agentHandles)
	}
	return nil
}
