// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package entitytype

import (
	"context"
	"errors"
	"fmt"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
)

// EntityTypeReferenceNormalizer converts entity-type references read from storage to entity type handles.
// Before entity types had handles, other records referenced a type by its name. A store that reads such a
// record uses this to present the reference as a handle, so the rest of the product only sees handles.
// It is a lookup over stored data and performs no caller authorization.
type EntityTypeReferenceNormalizer interface {
	// NormalizeEntityTypeReferences maps each stored value to a handle. A value that is already a handle
	// maps to itself. Otherwise it maps to the handle of the single entity type whose display name equals
	// the value. Values that match no type, or more than one, are left out of the result.
	NormalizeEntityTypeReferences(
		ctx context.Context, category TypeCategory, stored []string,
	) (map[string]string, error)
}

type entityTypeReferenceNormalizer struct {
	store entityTypeStoreInterface
}

func newEntityTypeReferenceNormalizer(store entityTypeStoreInterface) EntityTypeReferenceNormalizer {
	return &entityTypeReferenceNormalizer{store: store}
}

// NormalizeEntityTypeReferences implements EntityTypeReferenceNormalizer.
func (n *entityTypeReferenceNormalizer) NormalizeEntityTypeReferences(
	ctx context.Context, category TypeCategory, stored []string,
) (map[string]string, error) {
	if !category.IsValid() {
		return nil, fmt.Errorf("invalid entity type category %q", category)
	}

	resolved := make(map[string]string, len(stored))
	var legacy []string
	for _, value := range stored {
		if _, done := resolved[value]; done || value == "" {
			continue
		}
		_, err := n.store.GetEntityTypeByHandle(ctx, category, value)
		switch {
		case err == nil:
			resolved[value] = value
		case errors.Is(err, ErrEntityTypeNotFound):
			legacy = append(legacy, value)
		default:
			return nil, fmt.Errorf("failed to look up entity type by handle: %w", err)
		}
	}
	if len(legacy) == 0 {
		return resolved, nil
	}

	handlesByName, err := n.handlesByDisplayName(ctx, category)
	if err != nil {
		return nil, err
	}
	for _, value := range legacy {
		if handles := handlesByName[value]; len(handles) == 1 {
			resolved[value] = handles[0]
		}
	}
	return resolved, nil
}

// handlesByDisplayName lists every entity type of the category and groups their handles by display name.
// Types without a handle, such as one created by the source release after the handle backfill, are skipped.
func (n *entityTypeReferenceNormalizer) handlesByDisplayName(
	ctx context.Context, category TypeCategory,
) (map[string][]string, error) {
	handlesByName := make(map[string][]string)
	for offset := 0; ; offset += serverconst.MaxPageSize {
		page, err := n.store.GetEntityTypeList(ctx, category, serverconst.MaxPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to list entity types: %w", err)
		}
		for _, item := range page {
			if item.Handle == "" {
				continue
			}
			handlesByName[item.DisplayName] = append(handlesByName[item.DisplayName], item.Handle)
		}
		if len(page) < serverconst.MaxPageSize {
			return handlesByName, nil
		}
	}
}
