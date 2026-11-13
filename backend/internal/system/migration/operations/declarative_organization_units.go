// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"sync"
)

// DeclarativeOrganizationUnit is an organization unit defined in a declarative resource file. Such a unit has
// no database row, but the rows that name it as their owner still need its identifier.
type DeclarativeOrganizationUnit struct {
	ID     string
	Handle string
}

// DeclarativeOrganizationUnits lists the organization units defined in declarative resource files.
type DeclarativeOrganizationUnits interface {
	ListDeclarativeOrganizationUnits(ctx context.Context) ([]DeclarativeOrganizationUnit, error)
}

// declarativeOrganizationUnitSource resolves the provider on first use, so phases that do not need the
// declared units never read the resource files. A nil source or provider yields no units.
type declarativeOrganizationUnitSource struct {
	provider func() (DeclarativeOrganizationUnits, error)
	// systemID is the organization unit the deployment names in server.system_ou_id. It owns every connection,
	// flow and notification sender that existed before ownership was recorded.
	systemID string
	once     sync.Once
	units    []DeclarativeOrganizationUnit
	err      error
}

// NewDeclarativeOrganizationUnitSource returns the lazy source of the declared organization units.
func NewDeclarativeOrganizationUnitSource(
	provider func() (DeclarativeOrganizationUnits, error),
	systemID string,
) *declarativeOrganizationUnitSource {
	return &declarativeOrganizationUnitSource{provider: provider, systemID: systemID}
}

// systemOrganizationUnitID returns the configured system organization unit ID, which is empty when the
// deployment does not configure one.
func (s *declarativeOrganizationUnitSource) systemOrganizationUnitID() string {
	if s == nil {
		return ""
	}
	return s.systemID
}

func (s *declarativeOrganizationUnitSource) list(ctx context.Context) ([]DeclarativeOrganizationUnit, error) {
	if s == nil || s.provider == nil {
		return nil, nil
	}
	s.once.Do(func() {
		lister, err := s.provider()
		if err != nil {
			s.err = err
			return
		}
		s.units, s.err = lister.ListDeclarativeOrganizationUnits(ctx)
	})
	return s.units, s.err
}

// systemOrganizationUnit returns the declared system organization unit and whether it is declared. A deployment
// that configures no system organization unit has none declared.
func (s *declarativeOrganizationUnitSource) systemOrganizationUnit(
	ctx context.Context,
) (DeclarativeOrganizationUnit, bool, error) {
	if s.systemOrganizationUnitID() == "" {
		return DeclarativeOrganizationUnit{}, false, nil
	}
	units, err := s.list(ctx)
	if err != nil {
		return DeclarativeOrganizationUnit{}, false, err
	}
	for _, unit := range units {
		if unit.ID == s.systemID {
			return unit, true, nil
		}
	}
	return DeclarativeOrganizationUnit{}, false, nil
}
