// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package operations holds the migration operations a release can put in its plan. A release package chooses
// and orders them, and owns the manifest; the operations themselves do not belong to any one release.
package operations

// Dependencies contains the dependencies available while constructing the operations.
// Add narrowly typed providers only when a concrete operation requires them.
type Dependencies struct {
	// DeclarativeEntityTypes lazily provides the read-only entity types defined in declarative resource
	// files, which have no ENTITY_TYPES row. It is used by entity-type reference operations 42 to 45. When nil, those
	// operations map entity types from the database alone.
	DeclarativeEntityTypes func() (DeclarativeEntityTypes, error)
	// DeclarativeConnections lazily provides the connections defined in declarative resource files. It is
	// used by operation 48 and 49 to find the connection that issued each legacy federated subject. When
	// nil, those operations cannot link a user and report the missing connection.
	DeclarativeConnections func() (DeclarativeConnections, error)
	// DeclarativeOrganizationUnits lazily provides the organization units defined in declarative resource
	// files, which have no ORGANIZATION_UNIT row. The organization unit owner operations use it to find
	// the system organization unit, and are blocked until it is declared.
	DeclarativeOrganizationUnits func() (DeclarativeOrganizationUnits, error)
	// SystemOrganizationUnitID is the organization unit the deployment names in server.system_ou_id. It becomes
	// the owner of the rows that had none, and the owner operations are blocked while it is empty or not
	// declared.
	SystemOrganizationUnitID string
}
