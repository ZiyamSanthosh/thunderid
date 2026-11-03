// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package current explicitly selects the migration definition compiled into this binary.
package current

import (
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	release "github.com/thunder-id/thunderid/internal/system/migration/releases/v2_0_0"
)

// Dependencies contains the dependencies available to the release compiled into this binary.
type Dependencies = release.Dependencies

// DeclarativeEntityTypes lists the entity types defined in declarative resource files.
type DeclarativeEntityTypes = release.DeclarativeEntityTypes

// DeclarativeEntityType is an entity type defined in a declarative resource file.
type DeclarativeEntityType = release.DeclarativeEntityType

// DeclarativeConnections lists the connections defined in declarative resource files.
type DeclarativeConnections = release.DeclarativeConnections

// DeclarativeConnection is a connection defined in a declarative resource file.
type DeclarativeConnection = release.DeclarativeConnection

// Definition returns the release definition selected for the current binary.
func Definition() (*engine.ReleaseDefinition, error) {
	return DefinitionWithDependencies(Dependencies{})
}

// DefinitionWithDependencies returns the current release definition with the supplied dependencies.
func DefinitionWithDependencies(dependencies Dependencies) (*engine.ReleaseDefinition, error) {
	return release.DefinitionWithDependencies(dependencies)
}

// Catalog returns the ordered migration manifests known to the current binary.
func Catalog() ([]engine.ReleaseManifest, error) {
	definition, err := release.Definition()
	if err != nil {
		return nil, err
	}
	return []engine.ReleaseManifest{definition.Manifest}, nil
}
