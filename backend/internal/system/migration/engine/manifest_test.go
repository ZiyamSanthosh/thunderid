// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadReleaseManifestRejectsUnknownField(t *testing.T) {
	data := []byte(`{
		"manifestVersion": 1,
		"releaseVersion": "1.1.0",
		"migrationMetadataVersion": 1,
		"migrationDocumentation": "https://thunderid.dev/docs/migrations",
		"stores": [],
		"unexpected": true
	}`)

	_, err := LoadReleaseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
}

func TestLoadReleaseManifestRejectsMultipleJSONValues(t *testing.T) {
	_, err := LoadReleaseManifest([]byte(`{} {}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple JSON values")
}

func TestReleaseManifestValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ReleaseManifest)
		errorMatch string
	}{
		{
			name: "unsupported manifest version",
			mutate: func(manifest *ReleaseManifest) {
				manifest.ManifestVersion = 2
			},
			errorMatch: "unsupported manifest version",
		},
		{
			name: "noncanonical release version",
			mutate: func(manifest *ReleaseManifest) {
				manifest.ReleaseVersion = "v1.1.0"
			},
			errorMatch: "release version",
		},
		{
			name: "invalid metadata version",
			mutate: func(manifest *ReleaseManifest) {
				manifest.MigrationMetadataVersion = 0
			},
			errorMatch: "metadata version",
		},
		{
			name: "missing documentation",
			mutate: func(manifest *ReleaseManifest) {
				manifest.MigrationDocumentation = ""
			},
			errorMatch: "documentation",
		},
		{
			name: "missing store",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores = manifest.Stores[:3]
			},
			errorMatch: "exactly 4 stores",
		},
		{
			name: "unknown store",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[0].ID = "unknown"
			},
			errorMatch: "unknown store",
		},
		{
			name: "duplicate store",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[1].ID = manifest.Stores[0].ID
			},
			errorMatch: "duplicate store",
		},
		{
			name: "invalid store source level",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[0].AcceptedSourceLevel = "1"
			},
			errorMatch: "accepted source level",
		},
		{
			name: "changed store without advancement",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[0].Changed = true
			},
			errorMatch: "must advance",
		},
		{
			name: "changed store not at release level",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[0].Changed = true
				manifest.Stores[0].RequiredLevel = "1.0.1"
			},
			errorMatch: "must equal release version",
		},
		{
			name: "unchanged store advances",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Stores[0].RequiredLevel = "1.1.0"
			},
			errorMatch: "must retain",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validTestManifest()
			test.mutate(&manifest)

			err := manifest.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorMatch)
		})
	}
}

func validTestManifest() ReleaseManifest {
	return ReleaseManifest{
		ManifestVersion:          1,
		ReleaseVersion:           "1.1.0",
		MigrationMetadataVersion: 1,
		MigrationDocumentation:   "https://thunderid.dev/docs/migrations",
		Stores: []StoreRequirement{
			{ID: StoreIDConfigDB, AcceptedSourceLevel: "1.0.0", RequiredLevel: "1.0.0"},
			{ID: StoreIDEntityDB, AcceptedSourceLevel: "1.0.0", RequiredLevel: "1.0.0"},
			{ID: StoreIDRuntimeTransient, AcceptedSourceLevel: "1.0.0", RequiredLevel: "1.0.0"},
			{ID: StoreIDRuntimePersistent, AcceptedSourceLevel: "1.0.0", RequiredLevel: "1.0.0"},
		},
	}
}
