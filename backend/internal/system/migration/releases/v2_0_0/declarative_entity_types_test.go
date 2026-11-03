// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type fakeDeclarativeEntityTypes struct {
	types []DeclarativeEntityType
	err   error
	calls *int
}

func (f fakeDeclarativeEntityTypes) ListDeclarativeEntityTypes(context.Context) ([]DeclarativeEntityType, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.types, f.err
}

func mappingWith(declared ...DeclarativeEntityType) entityTypeReferenceMapping {
	mapping := entityTypeReferenceMapping{
		byName:   map[string]string{"dep\x00user\x00PAUMStaff": "paumstaff"},
		byHandle: map[string]string{"dep\x00user\x00paumstaff": "PAUMStaff"},
	}
	mapping.addDeclared(declared)
	return mapping
}

func TestTranslateUsesDeclarativeEntityTypes(t *testing.T) {
	customer := DeclarativeEntityType{Category: "user", Handle: "customer", DisplayName: "Customer"}
	mapping := mappingWith(customer)

	forward, changed, err := mapping.translate("dep", "user", "Customer", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "customer", forward)
	assert.True(t, changed)

	// A value that is already a handle is accepted unchanged, so the migration can be repeated.
	again, changed, err := mapping.translate("dep", "user", "customer", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "customer", again)
	assert.False(t, changed)

	back, changed, err := mapping.translate("dep", "user", "customer", entityTypeReferenceRollback)
	require.NoError(t, err)
	assert.Equal(t, "Customer", back)
	assert.True(t, changed)

	// The declarative types apply to every deployment, not only one.
	other, _, err := mapping.translate("another-deployment", "user", "Customer", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "customer", other)

	// Database types keep working next to declarative ones.
	stored, _, err := mapping.translate("dep", "user", "PAUMStaff", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "paumstaff", stored)
}

func TestTranslateWithoutDeclarativeEntityTypesStillFails(t *testing.T) {
	_, _, err := mappingWith().translate("dep", "user", "Customer", entityTypeReferenceForward)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `entity-type reference "Customer" has no forward mapping`)
}

func TestTranslateRejectsConflictingDatabaseAndDeclarativeTypes(t *testing.T) {
	mapping := mappingWith(DeclarativeEntityType{Category: "user", Handle: "staff", DisplayName: "PAUMStaff"})
	_, _, err := mapping.translate("dep", "user", "PAUMStaff", entityTypeReferenceForward)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is defined differently")

	// Agreeing on the handle is not a conflict.
	agreeing := mappingWith(DeclarativeEntityType{Category: "user", Handle: "paumstaff", DisplayName: "PAUMStaff"})
	handle, _, err := agreeing.translate("dep", "user", "PAUMStaff", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "paumstaff", handle)
}

func TestTranslateDeclarativeEntityTypesAreCategoryScoped(t *testing.T) {
	mapping := mappingWith(DeclarativeEntityType{Category: "agent", Handle: "bot", DisplayName: "Bot"})
	_, _, err := mapping.translate("dep", "user", "Bot", entityTypeReferenceForward)
	require.Error(t, err)
	handle, _, err := mapping.translate("dep", "agent", "Bot", entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "bot", handle)
}

func TestDeclarativeEntityTypeSourceIsLazyAndCached(t *testing.T) {
	assert.Nil(t, mustList(t, (*declarativeEntityTypeSource)(nil)))
	assert.Nil(t, mustList(t, newDeclarativeEntityTypeSource(nil)))

	providerCalls, listCalls := 0, 0
	source := newDeclarativeEntityTypeSource(func() (DeclarativeEntityTypes, error) {
		providerCalls++
		return fakeDeclarativeEntityTypes{
			types: []DeclarativeEntityType{{Category: "user", Handle: "person", DisplayName: "Person"}},
			calls: &listCalls,
		}, nil
	})
	assert.Zero(t, providerCalls, "the provider must not run until the first use")
	require.Len(t, mustList(t, source), 1)
	require.Len(t, mustList(t, source), 1)
	assert.Equal(t, 1, providerCalls)
	assert.Equal(t, 1, listCalls)
}

func TestDeclarativeEntityTypeSourcePropagatesProviderError(t *testing.T) {
	source := newDeclarativeEntityTypeSource(func() (DeclarativeEntityTypes, error) {
		return nil, errors.New("resources are invalid")
	})
	_, err := source.list(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resources are invalid")
}

func mustList(t *testing.T, source *declarativeEntityTypeSource) []DeclarativeEntityType {
	t.Helper()
	types, err := source.list(context.Background())
	require.NoError(t, err)
	return types
}

func TestEntityRecordTransformSkipsCategoriesWithoutEntityTypes(t *testing.T) {
	transform := entityTypeReferencesOperationConfig(
		migrateEntityTypeReferencesOperationID, "", entityTypeReferenceForward).transform
	mapping := mappingWith(DeclarativeEntityType{Category: "user", Handle: "customer", DisplayName: "Customer"})

	// App entities always carry the fixed type "application"; it is not an entity-type reference.
	app := engineRow("app")
	unchanged, changed, err := transform("application", "dep", app, mapping, entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "application", unchanged)
	assert.False(t, changed)

	user := engineRow("user")
	handle, changed, err := transform("Customer", "dep", user, mapping, entityTypeReferenceForward)
	require.NoError(t, err)
	assert.Equal(t, "customer", handle)
	assert.True(t, changed)

	_, _, err = transform("Unknown", "dep", user, mapping, entityTypeReferenceForward)
	require.Error(t, err)
}

func engineRow(category string) engine.QueryRow {
	return engine.QueryRow{"category": category}
}

func TestHandleAllocationAvoidsDeclarativeHandles(t *testing.T) {
	reserved := map[string]struct{}{
		reservedEntityTypeHandleKey("user", "customer"):   {},
		reservedEntityTypeHandleKey("user", "customer_1"): {},
	}
	handle, err := nextAvailableEntityTypeHandle("customer", "dep", "user", map[string]struct{}{}, reserved)
	require.NoError(t, err)
	assert.Equal(t, "customer_2", handle)

	// The reserved handle is category scoped and applies to every deployment.
	other, err := nextAvailableEntityTypeHandle("customer", "another-dep", "agent", map[string]struct{}{}, reserved)
	require.NoError(t, err)
	assert.Equal(t, "customer", other)
	again, err := nextAvailableEntityTypeHandle("customer", "another-dep", "user", map[string]struct{}{}, reserved)
	require.NoError(t, err)
	assert.Equal(t, "customer_2", again)
}

func TestBackfillVerificationRejectsHandleOfDeclarativeEntityType(t *testing.T) {
	source := newDeclarativeEntityTypeSource(func() (DeclarativeEntityTypes, error) {
		return fakeDeclarativeEntityTypes{types: []DeclarativeEntityType{
			{Category: "USER", Handle: "customer", DisplayName: "Customer"},
		}}, nil
	})
	operation := newBackfillEntityTypeHandleOperation(source)

	// A database row that already carries the declarative handle would stop the server from starting.
	result, err := operation.PostVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "customer", "customer"),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationInvalid, result.State)
	assert.Contains(t, result.Summary, "declarative entity type")

	// A different handle is fine.
	result, err = operation.PostVerify(context.Background(), entityTypeHandleAccess(
		entityTypeHandleRow("id-1", "customer", "customer_1"),
	), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, result.State)
}

func TestBackfillPropagatesDeclarativeEntityTypeError(t *testing.T) {
	source := newDeclarativeEntityTypeSource(func() (DeclarativeEntityTypes, error) {
		return nil, errors.New("resources are invalid")
	})
	_, err := newBackfillEntityTypeHandleOperation(source).PreVerify(
		context.Background(), entityTypeHandleAccess(), engine.ExecutionTarget{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "resources are invalid")
}
