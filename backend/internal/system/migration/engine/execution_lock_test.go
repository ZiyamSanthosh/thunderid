// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbprovider "github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

func TestMigrationExecutionLockerHoldsAndReleasesLock(t *testing.T) {
	client := providermock.NewDBClientInterfaceMock(t)
	session := providermock.NewDBSessionInterfaceMock(t)
	expectExecutionLockSession(client, session)
	session.On("QueryContext", mock.Anything, postgresExecutionLockQueries.tryAcquire,
		postgresMigrationExecutionLockKey).
		Return([]map[string]interface{}{{"acquired": true}}, nil).Once()
	session.On("QueryContext", mock.Anything, postgresExecutionLockQueries.release,
		postgresMigrationExecutionLockKey).
		Return([]map[string]interface{}{{"released": true}}, nil).Once()
	locker, err := NewMigrationExecutionLocker(client, DatabaseTypePostgres)
	require.NoError(t, err)
	executed := false

	err = locker.WithLock(context.Background(), func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestMigrationExecutionLockerRejectsConcurrentRunner(t *testing.T) {
	client := providermock.NewDBClientInterfaceMock(t)
	session := providermock.NewDBSessionInterfaceMock(t)
	expectExecutionLockSession(client, session)
	session.On("QueryContext", mock.Anything, postgresExecutionLockQueries.tryAcquire,
		postgresMigrationExecutionLockKey).
		Return([]map[string]interface{}{{"acquired": false}}, nil).Once()
	locker, err := NewMigrationExecutionLocker(client, DatabaseTypePostgres)
	require.NoError(t, err)
	executed := false

	err = locker.WithLock(context.Background(), func(context.Context) error {
		executed = true
		return nil
	})

	require.ErrorIs(t, err, ErrMigrationExecutionLocked)
	assert.False(t, executed)
}

func TestMigrationExecutionLockerPreservesExecutionAndReleaseErrors(t *testing.T) {
	client := providermock.NewDBClientInterfaceMock(t)
	session := providermock.NewDBSessionInterfaceMock(t)
	expectExecutionLockSession(client, session)
	session.On("QueryContext", mock.Anything, postgresExecutionLockQueries.tryAcquire,
		postgresMigrationExecutionLockKey).
		Return([]map[string]interface{}{{"acquired": true}}, nil).Once()
	releaseErr := errors.New("release failed")
	session.On("QueryContext", mock.Anything, postgresExecutionLockQueries.release,
		postgresMigrationExecutionLockKey).
		Return(nil, releaseErr).Once()
	locker, err := NewMigrationExecutionLocker(client, DatabaseTypePostgres)
	require.NoError(t, err)
	executionErr := errors.New("execution failed")

	err = locker.WithLock(context.Background(), func(context.Context) error {
		return executionErr
	})

	require.ErrorIs(t, err, executionErr)
	assert.ErrorIs(t, err, releaseErr)
}

func TestNewMigrationExecutionLockerValidatesProvider(t *testing.T) {
	client := providermock.NewDBClientInterfaceMock(t)

	missingClient, missingClientErr := NewMigrationExecutionLocker(nil, DatabaseTypePostgres)
	unsupported, unsupportedErr := NewMigrationExecutionLocker(client, DatabaseType("unsupported"))

	assert.Nil(t, missingClient)
	require.Error(t, missingClientErr)
	assert.Nil(t, unsupported)
	require.Error(t, unsupportedErr)
}

func expectExecutionLockSession(
	client *providermock.DBClientInterfaceMock,
	session *providermock.DBSessionInterfaceMock,
) {
	client.On("WithSession", mock.Anything, mock.Anything).
		Return(func(ctx context.Context, fn func(dbprovider.DBSessionInterface) error) error {
			return fn(session)
		}).Once()
}
