// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/database/provider"
)

const (
	postgresMigrationExecutionLockKey int64 = 6073191990351123017
	executionLockCleanupTimeout             = 5 * time.Second
)

var postgresExecutionLockQueries = struct {
	tryAcquire model.DBQuery
	release    model.DBQuery
}{
	tryAcquire: model.DBQuery{
		ID:    "MIG-LOCK-01",
		Query: "SELECT pg_try_advisory_lock($1) AS acquired",
	},
	release: model.DBQuery{
		ID:    "MIG-LOCK-02",
		Query: "SELECT pg_advisory_unlock($1) AS released",
	},
}

type postgresMigrationExecutionLocker struct {
	client provider.DBClientInterface
}

func newPostgresMigrationExecutionLocker(client provider.DBClientInterface) MigrationExecutionLocker {
	return &postgresMigrationExecutionLocker{client: client}
}

func (l *postgresMigrationExecutionLocker) WithLock(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	if fn == nil {
		return errors.New("execute with migration lock: function is required")
	}
	return l.client.WithSession(ctx, func(session provider.DBSessionInterface) (runErr error) {
		acquired, err := queryPostgresSessionBool(ctx, session, postgresExecutionLockQueries.tryAcquire,
			postgresMigrationExecutionLockKey, "acquired")
		if err != nil {
			return fmt.Errorf("acquire migration execution lock: %w", err)
		}
		if !acquired {
			return ErrMigrationExecutionLocked
		}

		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), executionLockCleanupTimeout)
			defer cancel()
			released, releaseErr := queryPostgresSessionBool(cleanupCtx, session,
				postgresExecutionLockQueries.release, postgresMigrationExecutionLockKey, "released")
			if releaseErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("release migration execution lock: %w", releaseErr))
			} else if !released {
				runErr = errors.Join(runErr, errors.New("release migration execution lock: lock was not held"))
			}
		}()

		return fn(ctx)
	})
}

func queryPostgresSessionBool(
	ctx context.Context,
	session provider.DBSessionInterface,
	query model.DBQuery,
	argument int64,
	column string,
) (bool, error) {
	rows, err := session.QueryContext(ctx, query, argument)
	if err != nil {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("query %q returned %d rows instead of one", query.ID, len(rows))
	}
	return QueryRow(rows[0]).Bool(column)
}
