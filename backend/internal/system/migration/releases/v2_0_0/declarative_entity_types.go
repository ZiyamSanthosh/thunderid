// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"sync"
)

// DeclarativeEntityType is a read-only entity type defined in a declarative resource file. Such a
// type has no row in ENTITY_TYPES, but stored entities and configuration still refer to it by name.
type DeclarativeEntityType struct {
	Category              string
	Handle                string
	DisplayName           string
	SupportsNameAttribute bool
	// DeclaresSubAttribute reports whether the schema still declares the legacy sub attribute.
	DeclaresSubAttribute bool
}

// DeclarativeEntityTypes lists the entity types defined in declarative resource files.
type DeclarativeEntityTypes interface {
	ListDeclarativeEntityTypes(ctx context.Context) ([]DeclarativeEntityType, error)
}

// declarativeEntityTypeSource resolves the provider on first use, so phases that do not need the
// declarative entity types never build the services behind it. A nil source or provider yields no
// declarative entity types, which leaves the mapping built from the database alone.
type declarativeEntityTypeSource struct {
	provider func() (DeclarativeEntityTypes, error)
	once     sync.Once
	types    []DeclarativeEntityType
	err      error
}

func newDeclarativeEntityTypeSource(
	provider func() (DeclarativeEntityTypes, error),
) *declarativeEntityTypeSource {
	return &declarativeEntityTypeSource{provider: provider}
}

func (s *declarativeEntityTypeSource) list(ctx context.Context) ([]DeclarativeEntityType, error) {
	if s == nil || s.provider == nil {
		return nil, nil
	}
	s.once.Do(func() {
		lister, err := s.provider()
		if err != nil {
			s.err = err
			return
		}
		s.types, s.err = lister.ListDeclarativeEntityTypes(ctx)
	})
	return s.types, s.err
}

// withDeclaredEntityTypes attaches the declarative entity type source to an operation config.
func withDeclaredEntityTypes(
	declared *declarativeEntityTypeSource,
	config entityTypeReferenceOperationConfig,
) entityTypeReferenceOperationConfig {
	config.declared = declared
	return config
}
