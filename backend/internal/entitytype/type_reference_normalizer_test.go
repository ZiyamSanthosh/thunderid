// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package entitytype

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/declarative_resource/entity"
)

// newCompositeTypeReferenceNormalizer builds a normalizer over a real composite store whose database side
// is a mock, so declarative types are resolved through the same list the product uses.
func newCompositeTypeReferenceNormalizer(
	t *testing.T, declarative []EntityType, dbItems []EntityTypeListItem,
) EntityTypeReferenceNormalizer {
	ctx := context.Background()

	require.NoError(t, entity.GetInstance().Clear())
	t.Cleanup(func() { _ = entity.GetInstance().Clear() })

	fileStore, _ := newEntityTypeFileBasedStore()
	for _, declared := range declarative {
		require.NoError(t, fileStore.CreateEntityType(ctx, declared))
	}

	dbStore := newEntityTypeStoreInterfaceMock(t)
	dbStore.On("GetEntityTypeByHandle", ctx, TypeCategoryUser, "Customer").
		Return(EntityType{}, ErrEntityTypeNotFound).Maybe()
	dbStore.On("GetEntityTypeByHandle", ctx, TypeCategoryUser, "customer").
		Return(EntityType{}, ErrEntityTypeNotFound).Maybe()
	dbStore.On("GetEntityTypeListCount", ctx, TypeCategoryUser).Return(len(dbItems), nil).Maybe()
	dbStore.On("GetEntityTypeList", ctx, TypeCategoryUser, len(dbItems), 0).Return(dbItems, nil).Maybe()

	return newEntityTypeReferenceNormalizer(newCompositeEntityTypeStore(fileStore, dbStore))
}

func TestNormalizeEntityTypeReferencesResolvesLegacyNameOfDeclarativeType(t *testing.T) {
	normalizer := newCompositeTypeReferenceNormalizer(t, []EntityType{{
		ID:          "declared-customer",
		Category:    TypeCategoryUser,
		Handle:      "customer",
		DisplayName: "Customer",
	}}, nil)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Customer", "customer"})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"Customer": "customer", "customer": "customer"}, resolved)
}

func TestNormalizeEntityTypeReferencesLeavesLegacyNameSharedByDeclarativeAndDatabaseTypesOut(t *testing.T) {
	normalizer := newCompositeTypeReferenceNormalizer(t, []EntityType{{
		ID:          "declared-customer",
		Category:    TypeCategoryUser,
		Handle:      "customer",
		DisplayName: "Customer",
	}}, []EntityTypeListItem{{
		ID:          "db-customer",
		Handle:      "retail",
		DisplayName: "Customer",
	}})

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Customer"})

	require.NoError(t, err)
	require.Empty(t, resolved)
}

func TestNormalizeEntityTypeReferencesKeepsHandles(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, "customer").
		Return(EntityType{Handle: "customer"}, nil).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"customer", "customer"})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"customer": "customer"}, resolved)
}

func TestNormalizeEntityTypeReferencesMapsLegacyNamesToHandles(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, "Customer").
		Return(EntityType{}, ErrEntityTypeNotFound).
		Once()
	storeMock.
		On("GetEntityTypeList", context.Background(), TypeCategoryUser, serverconst.MaxPageSize, 0).
		Return([]EntityTypeListItem{
			{Handle: "customer", DisplayName: "Customer"},
			{Handle: "employee", DisplayName: "Employee"},
		}, nil).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Customer"})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"Customer": "customer"}, resolved)
}

func TestNormalizeEntityTypeReferencesLeavesUnknownAndAmbiguousValuesOut(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, mock.Anything).
		Return(EntityType{}, ErrEntityTypeNotFound).
		Twice()
	storeMock.
		On("GetEntityTypeList", context.Background(), TypeCategoryUser, serverconst.MaxPageSize, 0).
		Return([]EntityTypeListItem{
			{Handle: "retail", DisplayName: "Customer"},
			{Handle: "business", DisplayName: "Customer"},
		}, nil).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Customer", "Missing"})

	require.NoError(t, err)
	require.Empty(t, resolved)
}

func TestNormalizeEntityTypeReferencesIgnoresTypesWithoutHandle(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, "Partner").
		Return(EntityType{}, ErrEntityTypeNotFound).
		Once()
	storeMock.
		On("GetEntityTypeList", context.Background(), TypeCategoryUser, serverconst.MaxPageSize, 0).
		Return([]EntityTypeListItem{{Handle: "", DisplayName: "Partner"}}, nil).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Partner"})

	require.NoError(t, err)
	require.Empty(t, resolved)
}

func TestNormalizeEntityTypeReferencesReturnsErrorOnLookupFailure(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, "customer").
		Return(EntityType{}, errors.New("db failure")).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"customer"})

	require.Error(t, err)
	require.Nil(t, resolved)
}

func TestNormalizeEntityTypeReferencesReturnsErrorOnListFailure(t *testing.T) {
	storeMock := newEntityTypeStoreInterfaceMock(t)
	storeMock.
		On("GetEntityTypeByHandle", context.Background(), TypeCategoryUser, "Customer").
		Return(EntityType{}, ErrEntityTypeNotFound).
		Once()
	storeMock.
		On("GetEntityTypeList", context.Background(), TypeCategoryUser, serverconst.MaxPageSize, 0).
		Return(nil, errors.New("db failure")).
		Once()

	normalizer := newEntityTypeReferenceNormalizer(storeMock)

	resolved, err := normalizer.NormalizeEntityTypeReferences(
		context.Background(), TypeCategoryUser, []string{"Customer"})

	require.Error(t, err)
	require.Nil(t, resolved)
}

func TestNormalizeEntityTypeReferencesRejectsInvalidCategory(t *testing.T) {
	normalizer := newEntityTypeReferenceNormalizer(newEntityTypeStoreInterfaceMock(t))

	resolved, err := normalizer.NormalizeEntityTypeReferences(context.Background(), TypeCategory("app"), []string{"x"})

	require.Error(t, err)
	require.Nil(t, resolved)
}
