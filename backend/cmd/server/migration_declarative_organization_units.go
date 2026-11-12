// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl // Mirrors the declarative connection lister; only the resource type, directory and fields differ.
package main

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	declarativeresource "github.com/thunder-id/thunderid/internal/system/declarative_resource"
	"github.com/thunder-id/thunderid/internal/system/migration/current"
)

// newDeclarativeOrganizationUnitsProvider returns a lazy provider of the organization units defined in
// declarative resource files. The files are only read when an operation first asks for them, so phases and
// commands that do not need the organization units never load the resource files.
func newDeclarativeOrganizationUnitsProvider() func() (current.DeclarativeOrganizationUnits, error) {
	return func() (current.DeclarativeOrganizationUnits, error) {
		return declarativeOrganizationUnitLister{}, nil
	}
}

type declarativeOrganizationUnitLister struct{}

// ListDeclarativeOrganizationUnits reads every organization unit document the declarative loader would read, in
// the same order of precedence, and keeps only the identifier and the handle of each.
func (declarativeOrganizationUnitLister) ListDeclarativeOrganizationUnits(
	_ context.Context,
) ([]current.DeclarativeOrganizationUnit, error) {
	collector := &declarativeOrganizationUnitCollector{}
	loader := declarativeresource.NewResourceLoader(declarativeresource.ResourceConfig{
		ResourceType:  "OrganizationUnit",
		DirectoryName: "organization_units",
		Parser:        parseDeclarativeOrganizationUnit,
		IDExtractor: func(dto interface{}) string {
			if unit, ok := dto.(*current.DeclarativeOrganizationUnit); ok {
				return unit.ID
			}
			return ""
		},
	}, collector)
	if err := loader.LoadResources(); err != nil {
		return nil, fmt.Errorf("read declarative organization units: %w", err)
	}
	return collector.units, nil
}

func parseDeclarativeOrganizationUnit(data []byte) (interface{}, error) {
	var document struct {
		ID     string `yaml:"id"`
		Handle string `yaml:"handle"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return &current.DeclarativeOrganizationUnit{ID: document.ID, Handle: document.Handle}, nil
}

type declarativeOrganizationUnitCollector struct {
	units []current.DeclarativeOrganizationUnit
}

func (c *declarativeOrganizationUnitCollector) Create(_ string, data interface{}) error {
	unit, ok := data.(*current.DeclarativeOrganizationUnit)
	if !ok {
		return fmt.Errorf("unexpected declarative organization unit type %T", data)
	}
	c.units = append(c.units, *unit)
	return nil
}
