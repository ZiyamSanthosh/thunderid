// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// executedStatement is one statement a fake recorded.
type executedStatement struct {
	storeID engine.StoreID
	queryID string
	args    []interface{}
}

// fakeAccess answers queries from per-query handlers and records every executed statement. A handler that is
// not registered fails the query, so a test notices an unexpected read.
type fakeAccess struct {
	queries  map[string]func(args []interface{}) []engine.QueryRow
	affected map[string]func(args []interface{}) int64
	executed []executedStatement
	err      error
}

func newFakeAccess() *fakeAccess {
	return &fakeAccess{
		queries:  map[string]func([]interface{}) []engine.QueryRow{},
		affected: map[string]func([]interface{}) int64{},
	}
}

// on registers fixed rows for a query.
func (a *fakeAccess) on(query dbmodel.DBQuery, rows ...engine.QueryRow) *fakeAccess {
	a.queries[query.GetID()] = func([]interface{}) []engine.QueryRow { return rows }
	return a
}

// onArgs registers a handler that chooses rows from the query arguments.
func (a *fakeAccess) onArgs(query dbmodel.DBQuery, handler func(args []interface{}) []engine.QueryRow) *fakeAccess {
	a.queries[query.GetID()] = handler
	return a
}

// relations registers which relations exist for queryTableRelation.
func (a *fakeAccess) relations(names ...string) *fakeAccess {
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
	}
	return a.onArgs(queryTableRelation, func(args []interface{}) []engine.QueryRow {
		if name, _ := args[0].(string); present[name] {
			return []engine.QueryRow{{"relation_kind": "r"}}
		}
		return []engine.QueryRow{}
	})
}

func (a *fakeAccess) Query(
	_ context.Context,
	_ engine.StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	handler, exists := a.queries[query.GetID()]
	if !exists {
		return nil, fmt.Errorf("unexpected query %s", query.GetID())
	}
	return handler(args), nil
}

func (*fakeAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func (a *fakeAccess) Execute(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) (int64, error) {
	if a.err != nil {
		return 0, a.err
	}
	a.executed = append(a.executed, executedStatement{storeID: storeID, queryID: query.GetID(), args: args})
	if handler, exists := a.affected[query.GetID()]; exists {
		return handler(args), nil
	}
	return 1, nil
}

// executedIDs returns the IDs of the executed statements, in order.
func (a *fakeAccess) executedIDs() []string {
	ids := make([]string, 0, len(a.executed))
	for _, statement := range a.executed {
		ids = append(ids, statement.queryID)
	}
	return ids
}

// migrationUserRow builds a working-table row.
func migrationUserRow(userID, originalUserID, ouID string, original bool) engine.QueryRow {
	return engine.QueryRow{
		"deployment_id": "dep", "user_id": userID, "is_original": original, "original_user_id": originalUserID,
		"email": "person@example.com", "ou_id": ouID, "linked_accounts": "{}", "sharing_policy_id": nil,
		"owner_ou_id": nil, "added_group_ids": "[]",
	}
}
