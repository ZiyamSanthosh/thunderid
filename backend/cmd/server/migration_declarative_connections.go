// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	declarativeresource "github.com/thunder-id/thunderid/internal/system/declarative_resource"
	"github.com/thunder-id/thunderid/internal/system/migration/current"
)

// newDeclarativeConnectionsProvider returns a lazy provider of the connections defined in declarative
// resource files. The files are only read when an operation first asks for them, so phases and commands that
// do not need the connections never load the resource files.
func newDeclarativeConnectionsProvider() func() (current.DeclarativeConnections, error) {
	return func() (current.DeclarativeConnections, error) {
		return declarativeConnectionLister{}, nil
	}
}

type declarativeConnectionLister struct{}

// ListDeclarativeConnections reads every connection document the declarative loader would read, in the same
// order of precedence, and keeps only the identifier and the vendor type of each.
func (declarativeConnectionLister) ListDeclarativeConnections(
	_ context.Context,
) ([]current.DeclarativeConnection, error) {
	collector := &declarativeConnectionCollector{}
	loader := declarativeresource.NewResourceLoader(declarativeresource.ResourceConfig{
		ResourceType:  "Connection",
		DirectoryName: "connections",
		Parser:        parseDeclarativeConnection,
		IDExtractor: func(dto interface{}) string {
			if connection, ok := dto.(*current.DeclarativeConnection); ok {
				return connection.ID
			}
			return ""
		},
	}, collector)
	if err := loader.LoadResources(); err != nil {
		return nil, fmt.Errorf("read declarative connections: %w", err)
	}
	return collector.connections, nil
}

func parseDeclarativeConnection(data []byte) (interface{}, error) {
	var document struct {
		ID   string `yaml:"id"`
		Type string `yaml:"type"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return &current.DeclarativeConnection{ID: document.ID, Type: document.Type}, nil
}

type declarativeConnectionCollector struct {
	connections []current.DeclarativeConnection
}

func (c *declarativeConnectionCollector) Create(_ string, data interface{}) error {
	connection, ok := data.(*current.DeclarativeConnection)
	if !ok {
		return fmt.Errorf("unexpected declarative connection type %T", data)
	}
	c.connections = append(c.connections, *connection)
	return nil
}
