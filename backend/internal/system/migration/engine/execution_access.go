// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
)

// ExecutionAccess provides read access to declared stores and write access only to the owning store.
type ExecutionAccess interface {
	ReadAccess
	Execute(context.Context, StoreID, dbmodel.DBQuery, ...interface{}) (int64, error)
}

// OperationExecution contains the engine-controlled resources for one operation scope.
type OperationExecution struct {
	access ExecutionAccess
	scope  Scope
}

func newOperationExecution(access ExecutionAccess, scope Scope) OperationExecution {
	return OperationExecution{access: access, scope: scope}
}

// Access returns the restricted database access for the operation.
func (e OperationExecution) Access() ExecutionAccess {
	return e.access
}

// Scope returns the scope currently being executed.
func (e OperationExecution) Scope() Scope {
	return e.scope
}

type operationExecutionAccess struct {
	*operationReadAccess
	writableStoreID StoreID
}

func newOperationExecutionAccess(
	definition OperationDefinition,
	configuredStores map[StoreID]ReadStore,
) (*operationExecutionAccess, error) {
	readAccess, err := newOperationReadAccess(definition, configuredStores)
	if err != nil {
		return nil, err
	}
	return &operationExecutionAccess{
		operationReadAccess: readAccess,
		writableStoreID:     definition.StoreID,
	}, nil
}

func (a *operationExecutionAccess) Execute(
	ctx context.Context,
	storeID StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) (int64, error) {
	if storeID != a.writableStoreID {
		return 0, fmt.Errorf("operation did not declare write access to store %q", storeID)
	}
	store := a.stores[storeID]
	if strings.TrimSpace(query.GetQuery(string(store.DatabaseType))) == "" {
		return 0, fmt.Errorf("query %q has no implementation for database type %q",
			query.GetID(), store.DatabaseType)
	}
	return store.Client.ExecuteContext(ctx, query, args...)
}
