// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package migration defines the release contract used by the migration engine.
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// StoreID identifies a ThunderID logical database.
type StoreID string

const (
	// StoreIDConfigDB identifies the configuration database.
	StoreIDConfigDB StoreID = "configdb"
	// StoreIDEntityDB identifies the entity database.
	StoreIDEntityDB StoreID = "entitydb"
	// StoreIDRuntimeTransient identifies the transient runtime database.
	StoreIDRuntimeTransient StoreID = "runtime_transient"
	// StoreIDRuntimePersistent identifies the persistent runtime database.
	StoreIDRuntimePersistent StoreID = "runtime_persistent"

	currentManifestVersion = 1
)

var (
	supportedStoreIDs = []StoreID{
		StoreIDConfigDB,
		StoreIDEntityDB,
		StoreIDRuntimeTransient,
		StoreIDRuntimePersistent,
	}

	canonicalVersionPattern = regexp.MustCompile(
		`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
			`(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)` +
			`(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?` +
			`(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
	)
)

// StoreRequirement declares the accepted and required migration levels of one logical database.
type StoreRequirement struct {
	ID                  StoreID `json:"id"`
	AcceptedSourceLevel string  `json:"acceptedSourceLevel"`
	RequiredLevel       string  `json:"requiredLevel"`
	Changed             bool    `json:"changed"`
}

// ReleaseManifest declares the database compatibility requirements of a ThunderID release.
type ReleaseManifest struct {
	ManifestVersion          int                `json:"manifestVersion"`
	ReleaseVersion           string             `json:"releaseVersion"`
	MigrationMetadataVersion int                `json:"migrationMetadataVersion"`
	MigrationDocumentation   string             `json:"migrationDocumentation"`
	Stores                   []StoreRequirement `json:"stores"`
}

// LoadReleaseManifest parses and validates a release manifest.
func LoadReleaseManifest(data []byte) (*ReleaseManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var manifest ReleaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode release manifest: %w", err)
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode release manifest: multiple JSON values")
		}
		return nil, fmt.Errorf("decode release manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// Validate verifies that the manifest is internally consistent.
func (m ReleaseManifest) Validate() error {
	if m.ManifestVersion != currentManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", m.ManifestVersion)
	}
	if !isCanonicalVersion(m.ReleaseVersion) {
		return fmt.Errorf("release version %q is not canonical semantic version", m.ReleaseVersion)
	}
	if m.MigrationMetadataVersion < 1 {
		return errors.New("migration metadata version must be positive")
	}
	if m.MigrationDocumentation == "" {
		return errors.New("migration documentation is required")
	}
	if len(m.Stores) != len(supportedStoreIDs) {
		return fmt.Errorf("manifest must declare exactly %d stores", len(supportedStoreIDs))
	}

	knownStores := make(map[StoreID]struct{}, len(supportedStoreIDs))
	for _, storeID := range supportedStoreIDs {
		knownStores[storeID] = struct{}{}
	}

	seenStores := make(map[StoreID]struct{}, len(m.Stores))
	for _, store := range m.Stores {
		if _, ok := knownStores[store.ID]; !ok {
			return fmt.Errorf("unknown store %q", store.ID)
		}
		if _, ok := seenStores[store.ID]; ok {
			return fmt.Errorf("duplicate store %q", store.ID)
		}
		seenStores[store.ID] = struct{}{}

		if !isCanonicalVersion(store.AcceptedSourceLevel) {
			return fmt.Errorf("store %q accepted source level %q is not canonical semantic version",
				store.ID, store.AcceptedSourceLevel)
		}
		if !isCanonicalVersion(store.RequiredLevel) {
			return fmt.Errorf("store %q required level %q is not canonical semantic version",
				store.ID, store.RequiredLevel)
		}

		if store.Changed {
			if store.AcceptedSourceLevel == store.RequiredLevel {
				return fmt.Errorf("changed store %q must advance its required level", store.ID)
			}
			if store.RequiredLevel != m.ReleaseVersion {
				return fmt.Errorf("changed store %q required level must equal release version %q",
					store.ID, m.ReleaseVersion)
			}
		} else if store.AcceptedSourceLevel != store.RequiredLevel {
			return fmt.Errorf("unchanged store %q must retain its accepted source level", store.ID)
		}
	}

	return nil
}

func isCanonicalVersion(version string) bool {
	return canonicalVersionPattern.MatchString(version)
}
