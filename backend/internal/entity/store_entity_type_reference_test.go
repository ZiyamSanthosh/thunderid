// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package entity

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/entitytypemock"
)

func TestNormalizeEntityTypeReferencesConvertsLegacyNamesToHandles(t *testing.T) {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(t)
	normalizer.
		On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser,
			[]string{"Customer", "customer"}).
		Return(map[string]string{"Customer": "customer", "customer": "customer"}, nil).
		Once()
	store := &entityDBStore{entityTypeNormalizer: normalizer}

	entities := []providers.Entity{
		{ID: "1", Category: providers.EntityCategoryUser, Type: "Customer"},
		{ID: "2", Category: providers.EntityCategoryUser, Type: "customer"},
	}

	require.NoError(t, store.normalizeEntityTypeReferences(context.Background(), entities))
	require.Equal(t, "customer", entities[0].Type)
	require.Equal(t, "customer", entities[1].Type)
}

func TestNormalizeEntityTypeReferencesLeavesUnresolvedTypesUnchanged(t *testing.T) {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(t)
	normalizer.
		On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, []string{"Gone"}).
		Return(map[string]string{}, nil).
		Once()
	store := &entityDBStore{entityTypeNormalizer: normalizer}

	entities := []providers.Entity{{ID: "1", Category: providers.EntityCategoryUser, Type: "Gone"}}

	require.NoError(t, store.normalizeEntityTypeReferences(context.Background(), entities))
	require.Equal(t, "Gone", entities[0].Type)
}

func TestNormalizeEntityTypeReferencesResolvesEachCategorySeparately(t *testing.T) {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(t)
	normalizer.
		On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, []string{"Default"}).
		Return(map[string]string{"Default": "user-default"}, nil).
		Once()
	normalizer.
		On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryAgent, []string{"Default"}).
		Return(map[string]string{"Default": "default"}, nil).
		Once()
	store := &entityDBStore{entityTypeNormalizer: normalizer}

	entities := []providers.Entity{
		{ID: "1", Category: providers.EntityCategoryUser, Type: "Default"},
		{ID: "2", Category: providers.EntityCategoryAgent, Type: "Default"},
	}

	require.NoError(t, store.normalizeEntityTypeReferences(context.Background(), entities))
	require.Equal(t, "user-default", entities[0].Type)
	require.Equal(t, "default", entities[1].Type)
}

func TestNormalizeEntityTypeReferencesSkipsCategoriesWithoutEntityTypes(t *testing.T) {
	store := &entityDBStore{entityTypeNormalizer: entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(t)}

	entities := []providers.Entity{{ID: "1", Category: providers.EntityCategoryApp, Type: "Web"}}

	require.NoError(t, store.normalizeEntityTypeReferences(context.Background(), entities))
	require.Equal(t, "Web", entities[0].Type)
}

func TestNormalizeEntityTypeReferencesWithoutNormalizerIsNoop(t *testing.T) {
	store := &entityDBStore{}

	entities := []providers.Entity{{ID: "1", Category: providers.EntityCategoryUser, Type: "Customer"}}

	require.NoError(t, store.normalizeEntityTypeReferences(context.Background(), entities))
	require.Equal(t, "Customer", entities[0].Type)
}

func TestNormalizeEntityTypeReferencesReturnsNormalizerError(t *testing.T) {
	normalizer := entitytypemock.NewEntityTypeReferenceNormalizerInterfaceMock(t)
	normalizer.
		On("NormalizeEntityTypeReferences", mock.Anything, entitytype.TypeCategoryUser, []string{"Customer"}).
		Return(nil, errors.New("lookup failed")).
		Once()
	store := &entityDBStore{entityTypeNormalizer: normalizer}

	entities := []providers.Entity{{ID: "1", Category: providers.EntityCategoryUser, Type: "Customer"}}

	require.Error(t, store.normalizeEntityTypeReferences(context.Background(), entities))
}
