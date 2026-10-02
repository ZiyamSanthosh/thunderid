// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package current

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionSelectsVersion2(t *testing.T) {
	definition, err := Definition()

	require.NoError(t, err)
	assert.Equal(t, "2.0.0", definition.Manifest.ReleaseVersion)
	assert.Equal(t, "2.0.0", definition.Plan.TargetVersion)
}

func TestCatalogContainsCurrentRelease(t *testing.T) {
	catalog, err := Catalog()

	require.NoError(t, err)
	require.Len(t, catalog, 1)
	assert.Equal(t, "2.0.0", catalog[0].ReleaseVersion)
}
