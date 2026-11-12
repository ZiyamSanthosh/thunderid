// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"strings"
	"sync"
)

// DeclarativeConnection is a connection defined in a declarative resource file. Such a connection has no
// database row, but stored users still hold the subjects it issued.
type DeclarativeConnection struct {
	ID   string
	Type string
}

// DeclarativeConnections lists the connections defined in declarative resource files.
type DeclarativeConnections interface {
	ListDeclarativeConnections(ctx context.Context) ([]DeclarativeConnection, error)
}

// declarativeConnectionSource resolves the provider on first use, so phases that do not need the
// declarative connections never read the resource files. A nil source or provider yields no
// connections.
type declarativeConnectionSource struct {
	provider    func() (DeclarativeConnections, error)
	once        sync.Once
	connections []DeclarativeConnection
	err         error
}

func newDeclarativeConnectionSource(
	provider func() (DeclarativeConnections, error),
) *declarativeConnectionSource {
	return &declarativeConnectionSource{provider: provider}
}

func (s *declarativeConnectionSource) list(ctx context.Context) ([]DeclarativeConnection, error) {
	if s == nil || s.provider == nil {
		return nil, nil
	}
	s.once.Do(func() {
		lister, err := s.provider()
		if err != nil {
			s.err = err
			return
		}
		s.connections, s.err = lister.ListDeclarativeConnections(ctx)
	})
	return s.connections, s.err
}

// idsByProvider groups the identifiers of the declarative connections by the provider their type names.
func (s *declarativeConnectionSource) idsByProvider(
	ctx context.Context,
) (map[legacySubjectProvider][]string, error) {
	connections, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[legacySubjectProvider][]string)
	for _, connection := range connections {
		provider := legacySubjectProvider(strings.ToLower(strings.TrimSpace(connection.Type)))
		if connection.ID != "" {
			ids[provider] = append(ids[provider], connection.ID)
		}
	}
	return ids, nil
}
