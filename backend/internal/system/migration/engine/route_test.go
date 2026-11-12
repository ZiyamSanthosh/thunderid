// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseRouteResolverFindsMultiReleaseRoute(t *testing.T) {
	releaseOne := routeManifest("1.1.0", "1.0.0", StoreIDConfigDB)
	releaseTwo := routeManifest("2.0.0", "1.1.0", StoreIDConfigDB)
	resolver, err := NewReleaseRouteResolver(releaseTwo, []ReleaseManifest{releaseOne, releaseTwo})
	require.NoError(t, err)

	route, err := resolver.Resolve(routeLevels("1.0.0"))

	require.NoError(t, err)
	assert.Equal(t, []string{"1.1.0", "2.0.0"}, route.Releases)
	assert.Equal(t, "1.1.0", route.NextMigrationRelease())
}

func TestReleaseRouteResolverHandlesTargetAndUnknownLevels(t *testing.T) {
	target := routeManifest("2.0.0", "1.0.0", StoreIDConfigDB)
	resolver, err := NewReleaseRouteResolver(target, []ReleaseManifest{target})
	require.NoError(t, err)

	atTarget, targetErr := resolver.Resolve(routeLevels("2.0.0"))
	_, unknownErr := resolver.Resolve(routeLevels("0.9.0"))

	require.NoError(t, targetErr)
	assert.Empty(t, atTarget.Releases)
	require.Error(t, unknownErr)
	assert.True(t, errors.Is(unknownErr, ErrMigrationRouteNotFound))
}

func TestReleaseRouteResolverValidatesCatalogAndLevels(t *testing.T) {
	target := routeManifest("2.0.0", "1.0.0", StoreIDConfigDB)
	_, missingTargetErr := NewReleaseRouteResolver(target, []ReleaseManifest{
		routeManifest("1.1.0", "1.0.0", StoreIDConfigDB),
	})
	resolver, err := NewReleaseRouteResolver(target, []ReleaseManifest{target})
	require.NoError(t, err)
	_, levelsErr := resolver.Resolve(map[StoreID]string{StoreIDConfigDB: "1.0.0"})

	require.Error(t, missingTargetErr)
	assert.ErrorContains(t, missingTargetErr, "absent from the catalog")
	require.Error(t, levelsErr)
	assert.ErrorContains(t, levelsErr, "exactly 4 stores")
}

func routeManifest(releaseVersion string, sourceLevel string, changedStore StoreID) ReleaseManifest {
	manifest := validTestManifest()
	manifest.ReleaseVersion = releaseVersion
	for index := range manifest.Stores {
		manifest.Stores[index].AcceptedSourceLevel = "1.0.0"
		manifest.Stores[index].RequiredLevel = "1.0.0"
		manifest.Stores[index].Changed = false
		if manifest.Stores[index].ID == changedStore {
			manifest.Stores[index].AcceptedSourceLevel = sourceLevel
			manifest.Stores[index].RequiredLevel = releaseVersion
			manifest.Stores[index].Changed = true
		}
	}
	return manifest
}

func routeLevels(configLevel string) map[StoreID]string {
	return map[StoreID]string{
		StoreIDConfigDB:          configLevel,
		StoreIDEntityDB:          "1.0.0",
		StoreIDRuntimeTransient:  "1.0.0",
		StoreIDRuntimePersistent: "1.0.0",
	}
}
