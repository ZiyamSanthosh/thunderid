// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMigrationRouteNotFound means that the release catalog has no path from the observed store levels.
var ErrMigrationRouteNotFound = errors.New("no migration route is available for the observed store levels")

// MigrationRoute is the ordered list of ThunderID releases that own the required database transitions.
type MigrationRoute struct {
	Releases []string
}

// NextMigrationRelease returns the first release that must migrate the observed databases.
func (r MigrationRoute) NextMigrationRelease() string {
	if len(r.Releases) == 0 {
		return ""
	}
	return r.Releases[0]
}

// ReleaseRouteResolver finds database migration paths from an ordered release-manifest catalog.
type ReleaseRouteResolver struct {
	target  ReleaseManifest
	catalog []ReleaseManifest
}

// NewReleaseRouteResolver validates a release catalog used to guide incompatible installations.
func NewReleaseRouteResolver(
	target ReleaseManifest,
	catalog []ReleaseManifest,
) (*ReleaseRouteResolver, error) {
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("create release route resolver: invalid target manifest: %w", err)
	}
	if len(catalog) == 0 {
		return nil, errors.New("create release route resolver: release catalog is required")
	}

	manifests := make([]ReleaseManifest, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	targetFound := false
	for index, manifest := range catalog {
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("create release route resolver: invalid catalog manifest %d: %w", index, err)
		}
		if _, duplicate := seen[manifest.ReleaseVersion]; duplicate {
			return nil, fmt.Errorf("create release route resolver: duplicate release %q", manifest.ReleaseVersion)
		}
		seen[manifest.ReleaseVersion] = struct{}{}
		if manifest.ReleaseVersion == target.ReleaseVersion {
			targetFound = true
		}
		manifests[index] = manifest
	}
	if !targetFound {
		return nil, fmt.Errorf("create release route resolver: target release %q is absent from the catalog",
			target.ReleaseVersion)
	}
	return &ReleaseRouteResolver{target: target, catalog: manifests}, nil
}

// Resolve finds the shortest catalog path from observed completed levels to the target levels.
func (r *ReleaseRouteResolver) Resolve(completedLevels map[StoreID]string) (MigrationRoute, error) {
	start, err := storeLevelVector(completedLevels)
	if err != nil {
		return MigrationRoute{}, fmt.Errorf("resolve migration route: %w", err)
	}
	target := requiredLevelVector(r.target)
	if start == target {
		return MigrationRoute{}, nil
	}

	type routeNode struct {
		levels   string
		releases []string
	}
	queue := []routeNode{{levels: start}}
	visited := map[string]struct{}{start: {}}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, manifest := range r.catalog {
			if acceptedLevelVector(manifest) != node.levels {
				continue
			}
			next := requiredLevelVector(manifest)
			if next == node.levels {
				continue
			}
			releases := append(append([]string(nil), node.releases...), manifest.ReleaseVersion)
			if next == target {
				return MigrationRoute{Releases: releases}, nil
			}
			if _, exists := visited[next]; exists {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, routeNode{levels: next, releases: releases})
		}
	}
	return MigrationRoute{}, ErrMigrationRouteNotFound
}

func storeLevelVector(levels map[StoreID]string) (string, error) {
	if len(levels) != len(supportedStoreIDs) {
		return "", fmt.Errorf("completed levels must contain exactly %d stores", len(supportedStoreIDs))
	}
	values := make([]string, 0, len(supportedStoreIDs))
	for _, storeID := range supportedStoreIDs {
		level, exists := levels[storeID]
		if !exists {
			return "", fmt.Errorf("completed level for store %q is missing", storeID)
		}
		if !isCanonicalVersion(level) {
			return "", fmt.Errorf("completed level %q for store %q is not canonical", level, storeID)
		}
		values = append(values, level)
	}
	return strings.Join(values, "\x00"), nil
}

func acceptedLevelVector(manifest ReleaseManifest) string {
	return manifestLevelVector(manifest, func(requirement StoreRequirement) string {
		return requirement.AcceptedSourceLevel
	})
}

func requiredLevelVector(manifest ReleaseManifest) string {
	return manifestLevelVector(manifest, func(requirement StoreRequirement) string {
		return requirement.RequiredLevel
	})
}

func manifestLevelVector(
	manifest ReleaseManifest,
	level func(StoreRequirement) string,
) string {
	requirements := make(map[StoreID]StoreRequirement, len(manifest.Stores))
	for _, requirement := range manifest.Stores {
		requirements[requirement.ID] = requirement
	}
	values := make([]string, 0, len(supportedStoreIDs))
	for _, storeID := range supportedStoreIDs {
		values = append(values, level(requirements[storeID]))
	}
	return strings.Join(values, "\x00")
}
