// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// This file is temporary compatibility code for references that still hold entity type names. It is
// removed in a later release, together with the stores' entity type reference files.

package entitytype

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/cache"
	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
)

// EntityTypeReferenceNormalizerInterface converts entity-type references read from storage to entity type handles.
// Before entity types had handles, other records referenced a type by its name. A store that reads such a
// record uses this to present the reference as a handle, so the rest of the product only sees handles.
// It is a lookup over stored data and performs no caller authorization.
type EntityTypeReferenceNormalizerInterface interface {
	// NormalizeEntityTypeReferences maps each stored value to a handle. A value that is already a handle
	// maps to itself. Otherwise it maps to the handle of the single entity type whose display name equals
	// the value. Values that match no type, or more than one, are left out of the result.
	NormalizeEntityTypeReferences(
		ctx context.Context, category TypeCategory, stored []string,
	) (map[string]string, error)
}

// ResolveReferences maps the stored entity type references of a category to handles through the
// normalizer. It returns no mapping for an empty input without calling the normalizer.
func ResolveReferences(
	ctx context.Context, normalizer EntityTypeReferenceNormalizerInterface, category TypeCategory, stored []string,
) (map[string]string, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	handles, err := normalizer.NormalizeEntityTypeReferences(ctx, category, stored)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize entity type references: %w", err)
	}
	return handles, nil
}

// ReplaceWithHandles swaps each value for its handle when one is known and drops the duplicates that
// result, keeping the original order. It returns the input unchanged when it is empty.
func ReplaceWithHandles(values []string, handles map[string]string) []string {
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

// entityTypeReferenceSnapshot is the handles and display names of the entity types of one category, from
// which stored references are resolved without reading the store. Its fields are exported so that a
// shared cache can serialize it.
type entityTypeReferenceSnapshot struct {
	Handles       map[string]struct{}
	HandlesByName map[string][]string
}

type entityTypeReferenceNormalizer struct {
	store         entityTypeStoreInterface
	snapshotCache cache.CacheInterface[*entityTypeReferenceSnapshot]
}

func newEntityTypeReferenceNormalizer(
	store entityTypeStoreInterface, snapshotCache cache.CacheInterface[*entityTypeReferenceSnapshot],
) EntityTypeReferenceNormalizerInterface {
	return &entityTypeReferenceNormalizer{store: store, snapshotCache: snapshotCache}
}

// NormalizeEntityTypeReferences implements EntityTypeReferenceNormalizerInterface.
func (n *entityTypeReferenceNormalizer) NormalizeEntityTypeReferences(
	ctx context.Context, category TypeCategory, stored []string,
) (map[string]string, error) {
	if !category.IsValid() {
		return nil, fmt.Errorf("invalid entity type category %q", category)
	}

	resolved := make(map[string]string, len(stored))
	if len(stored) == 0 {
		return resolved, nil
	}

	snapshot, err := n.snapshotFor(ctx, category)
	if err != nil {
		return nil, err
	}
	for _, value := range stored {
		if _, done := resolved[value]; done || value == "" {
			continue
		}
		if _, isHandle := snapshot.Handles[value]; isHandle {
			resolved[value] = value
		} else if handles := snapshot.HandlesByName[value]; len(handles) == 1 {
			resolved[value] = handles[0]
		}
	}
	return resolved, nil
}

// snapshotFor returns the category's snapshot, reading it from the store when the cache has none. The
// cache entry lives for the configured cache lifetime, so a type created or renamed within that time is
// seen only afterwards.
func (n *entityTypeReferenceNormalizer) snapshotFor(
	ctx context.Context, category TypeCategory,
) (*entityTypeReferenceSnapshot, error) {
	key := cache.CacheKey{Key: string(category)}
	if cached, ok := n.snapshotCache.Get(ctx, key); ok && cached != nil {
		return cached, nil
	}

	snapshot, err := n.buildSnapshot(ctx, category)
	if err != nil {
		return nil, err
	}
	// A cache write failure only costs the next call a rebuild.
	_ = n.snapshotCache.Set(ctx, key, snapshot)
	return snapshot, nil
}

// buildSnapshot lists every entity type of the category. Types without a handle, such as one created by
// the source release after the handle backfill, are skipped.
func (n *entityTypeReferenceNormalizer) buildSnapshot(
	ctx context.Context, category TypeCategory,
) (*entityTypeReferenceSnapshot, error) {
	snapshot := &entityTypeReferenceSnapshot{
		Handles:       make(map[string]struct{}),
		HandlesByName: make(map[string][]string),
	}
	for offset := 0; ; offset += serverconst.MaxPageSize {
		page, err := n.store.GetEntityTypeList(ctx, category, serverconst.MaxPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to list entity types: %w", err)
		}
		for _, item := range page {
			if item.Handle == "" {
				continue
			}
			snapshot.Handles[item.Handle] = struct{}{}
			snapshot.HandlesByName[item.DisplayName] = append(snapshot.HandlesByName[item.DisplayName], item.Handle)
		}
		if len(page) < serverconst.MaxPageSize {
			return snapshot, nil
		}
	}
}
