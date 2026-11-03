// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/entitytype"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

// fakeEntityTypeService implements only the list method the migration lister calls.
type fakeEntityTypeService struct {
	entitytype.EntityTypeServiceInterface
	byCategory map[entitytype.TypeCategory][]entitytype.EntityTypeListItem
	schemas    map[string]json.RawMessage
	err        *tidcommon.ServiceError
}

func (f fakeEntityTypeService) GetEntityTypeList(
	_ context.Context, category entitytype.TypeCategory, limit, offset int, _ bool,
) (*entitytype.EntityTypeListResponse, *tidcommon.ServiceError) {
	if f.err != nil {
		return nil, f.err
	}
	all := f.byCategory[category]
	end := min(offset+limit, len(all))
	if offset > len(all) {
		offset = len(all)
	}
	return &entitytype.EntityTypeListResponse{Types: all[offset:end]}, nil
}

func (f fakeEntityTypeService) GetEntityTypeSchema(
	_ context.Context, category entitytype.TypeCategory, handle string,
) (*entitytype.EntityType, *tidcommon.ServiceError) {
	if f.err != nil {
		return nil, f.err
	}
	return &entitytype.EntityType{
		Category: category, Handle: handle, Schema: f.schemas[string(category)+"\x00"+handle],
	}, nil
}

func TestDeclarativeEntityTypeListerListsTypesOfAllCategories(t *testing.T) {
	lister := declarativeEntityTypeLister{service: fakeEntityTypeService{
		byCategory: map[entitytype.TypeCategory][]entitytype.EntityTypeListItem{
			entitytype.TypeCategoryUser: {
				// The declarative-only store does not set IsReadOnly, so the lister must not rely on it.
				{Handle: "customer", DisplayName: "Customer"},
				{Handle: "person", DisplayName: "Person"},
			},
			entitytype.TypeCategoryAgent: {{Handle: "bot", DisplayName: "Bot"}},
		},
		schemas: map[string]json.RawMessage{
			"agent\x00bot": json.RawMessage(`{"name":{"type":"string"}}`),
		},
	}}
	types, err := lister.ListDeclarativeEntityTypes(context.Background())
	require.NoError(t, err)
	require.Len(t, types, 3)
	assert.Equal(t, "user", types[0].Category)
	assert.Equal(t, "customer", types[0].Handle)
	assert.Equal(t, "Customer", types[0].DisplayName)
	assert.Equal(t, "person", types[1].Handle)
	assert.Equal(t, "agent", types[2].Category)
	assert.Equal(t, "bot", types[2].Handle)
	assert.True(t, types[2].SupportsNameAttribute)
}

func TestDeclarativeEntityTypeListerPagesThroughAllTypes(t *testing.T) {
	var items []entitytype.EntityTypeListItem
	for i := 0; i < 250; i++ {
		items = append(items, entitytype.EntityTypeListItem{
			Handle: "t" + string(rune('a'+i%26)) + string(rune('a'+i/26)), DisplayName: "T",
		})
	}
	lister := declarativeEntityTypeLister{service: fakeEntityTypeService{
		byCategory: map[entitytype.TypeCategory][]entitytype.EntityTypeListItem{entitytype.TypeCategoryUser: items},
	}}
	types, err := lister.ListDeclarativeEntityTypes(context.Background())
	require.NoError(t, err)
	assert.Len(t, types, 250)
}

func TestDeclarativeEntityTypeListerReportsServiceError(t *testing.T) {
	lister := declarativeEntityTypeLister{service: fakeEntityTypeService{
		err: &tidcommon.ServiceError{ErrorDescription: tidcommon.I18nMessage{DefaultValue: "boom"}},
	}}
	_, err := lister.ListDeclarativeEntityTypes(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
