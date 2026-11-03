// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/thunder-id/thunderid/internal/system/database/provider"
)

// ErrMigrationExecutionLocked means that another migration runner owns the database namespace.
var ErrMigrationExecutionLocked = errors.New("database namespace is owned by another migration runner")

// MigrationExecutionLocker coordinates migration runners without locking application tables.
type MigrationExecutionLocker interface {
	WithLock(context.Context, func(context.Context) error) error
}

type migrationExecutionLockerFactory func(provider.DBClientInterface) MigrationExecutionLocker

var migrationExecutionLockerFactories = map[DatabaseType]migrationExecutionLockerFactory{
	DatabaseTypePostgres: newPostgresMigrationExecutionLocker,
}

// NewMigrationExecutionLocker creates the registered coordination-lock implementation for a database provider.
func NewMigrationExecutionLocker(
	client provider.DBClientInterface,
	databaseType DatabaseType,
) (MigrationExecutionLocker, error) {
	if client == nil {
		return nil, errors.New("create migration execution locker: database client is required")
	}
	factory, supported := migrationExecutionLockerFactories[databaseType]
	if !supported {
		return nil, fmt.Errorf("create migration execution locker: unsupported database type %q", databaseType)
	}
	return factory(client), nil
}
