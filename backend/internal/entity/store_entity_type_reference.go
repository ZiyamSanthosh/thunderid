// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// This file is temporary compatibility code for references that still hold entity type names. It is
// removed together with the entity type reference normalizer in a later release.

package entity

import (
	"context"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// normalizeEntityTypeReferences rewrites each entity's type to the handle of the entity type it refers to.
// Entities stored before entity types had handles reference their type by name. Values that do
// not resolve to a known entity type are left unchanged.
func (es *entityDBStore) normalizeEntityTypeReferences(ctx context.Context, entities []providers.Entity) error {
	if es.entityTypeNormalizer == nil {
		return nil
	}

	storedByCategory := make(map[providers.EntityCategory][]string)
	for _, entity := range entities {
		if usesEntityType(entity.Category) {
			storedByCategory[entity.Category] = append(storedByCategory[entity.Category], entity.Type)
		}
	}

	for category, stored := range storedByCategory {
		resolved, err := entitytype.ResolveReferences(
			ctx, es.entityTypeNormalizer, entitytype.TypeCategory(category), stored)
		if err != nil {
			return err
		}
		for i := range entities {
			if entities[i].Category != category {
				continue
			}
			if handle, ok := resolved[entities[i].Type]; ok {
				entities[i].Type = handle
			}
		}
	}
	return nil
}
