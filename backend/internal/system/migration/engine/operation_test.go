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

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/tests/mocks/database/providermock"
)

type executionCall struct {
	storeID StoreID
	query   dbmodel.DBQuery
	args    []interface{}
}

type recordingExecutionAccess struct {
	databaseType    DatabaseType
	databaseTypeErr error
	executeErr      error
	calls           []executionCall
}

func (a *recordingExecutionAccess) Query(
	context.Context,
	StoreID,
	dbmodel.DBQuery,
	...interface{},
) ([]QueryRow, error) {
	return nil, nil
}

func (a *recordingExecutionAccess) DatabaseType(StoreID) (DatabaseType, error) {
	return a.databaseType, a.databaseTypeErr
}

func (a *recordingExecutionAccess) Execute(
	_ context.Context,
	storeID StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) (int64, error) {
	a.calls = append(a.calls, executionCall{storeID: storeID, query: query, args: args})
	if a.executeErr != nil {
		return 0, a.executeErr
	}
	return 1, nil
}

func TestSQLOperationProvidesValidatedLifecycle(t *testing.T) {
	config := validSQLOperationConfig()
	operation, err := NewSQLOperation(config)
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, config.Definition, definition)
	definition.ReadStoreIDs[0] = StoreIDRuntimePersistent
	assert.Equal(t, StoreIDConfigDB, operation.Definition().ReadStoreIDs[0])

	readiness, err := operation.CheckReadiness(context.Background(), nil, PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, ReadinessReady, readiness.State)

	scopes, err := operation.ResolveScopes(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, []Scope{{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}}, scopes)

	preVerification, err := operation.PreVerify(context.Background(), nil, scopes[0])
	require.NoError(t, err)
	assert.Equal(t, VerificationSource, preVerification.State)
	postVerification, err := operation.PostVerify(context.Background(), nil, scopes[0])
	require.NoError(t, err)
	assert.Equal(t, VerificationTarget, postVerification.State)

	implementation, err := operation.Implementation(DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 1)
	implementation.Statements[0].Query = "changed"
	secondCopy, err := operation.Implementation(DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE example ADD COLUMN migrated BOOLEAN", secondCopy.Statements[0].Query)

	_, err = operation.Implementation("mysql")
	assert.ErrorContains(t, err, "no SQL implementation")
}

func TestSQLOperationExecutesProviderStatementsInOrder(t *testing.T) {
	config := validSQLOperationConfig()
	config.Implementations[DatabaseTypePostgres] = SQLImplementation{Statements: []SQLStatement{
		{
			ID: "add-column", Query: "ALTER TABLE example ADD COLUMN migrated BOOLEAN",
		},
		{
			ID: "set-value", Query: "UPDATE example SET migrated = $1",
			Args: func(scope Scope) ([]interface{}, error) {
				return []interface{}{scope.ID}, nil
			},
		},
	}}
	operation, err := NewSQLOperation(config)
	require.NoError(t, err)
	access := &recordingExecutionAccess{databaseType: DatabaseTypePostgres}
	scope := Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}

	err = operation.Execute(context.Background(), newOperationExecution(access, scope))

	require.NoError(t, err)
	require.Len(t, access.calls, 2)
	assert.Equal(t, StoreIDEntityDB, access.calls[0].storeID)
	assert.Equal(t, "2.0.0/entitydb/example/add-column", access.calls[0].query.ID)
	assert.Equal(t, "ALTER TABLE example ADD COLUMN migrated BOOLEAN", access.calls[0].query.Query)
	assert.Empty(t, access.calls[0].args)
	assert.Equal(t, "2.0.0/entitydb/example/set-value", access.calls[1].query.ID)
	assert.Equal(t, []interface{}{string(StoreIDEntityDB)}, access.calls[1].args)
}

func TestSQLOperationExecutionFailures(t *testing.T) {
	t.Run("missing execution access", func(t *testing.T) {
		operation, err := NewSQLOperation(validSQLOperationConfig())
		require.NoError(t, err)

		err = operation.Execute(context.Background(), newOperationExecution(nil,
			Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}))

		assert.ErrorContains(t, err, "execution access is required")
	})

	t.Run("invalid scope", func(t *testing.T) {
		operation, err := NewSQLOperation(validSQLOperationConfig())
		require.NoError(t, err)

		err = operation.Execute(context.Background(), newOperationExecution(
			&recordingExecutionAccess{databaseType: DatabaseTypePostgres},
			Scope{Type: ScopeTypeStore, ID: string(StoreIDConfigDB)}))

		assert.ErrorContains(t, err, "must use store ID")
	})

	t.Run("database type unavailable", func(t *testing.T) {
		operation, err := NewSQLOperation(validSQLOperationConfig())
		require.NoError(t, err)
		access := &recordingExecutionAccess{databaseTypeErr: errors.New("provider unavailable")}

		err = operation.Execute(context.Background(), newOperationExecution(access,
			Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}))

		assert.ErrorContains(t, err, "provider unavailable")
		assert.Empty(t, access.calls)
	})

	t.Run("argument construction", func(t *testing.T) {
		config := validSQLOperationConfig()
		config.Implementations[DatabaseTypePostgres] = SQLImplementation{Statements: []SQLStatement{{
			ID: "statement", Query: "UPDATE example SET migrated = TRUE",
			Args: func(Scope) ([]interface{}, error) {
				return nil, errors.New("arguments unavailable")
			},
		}}}
		operation, err := NewSQLOperation(config)
		require.NoError(t, err)
		access := &recordingExecutionAccess{databaseType: DatabaseTypePostgres}

		err = operation.Execute(context.Background(), newOperationExecution(access,
			Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}))

		assert.ErrorContains(t, err, "arguments unavailable")
		assert.Empty(t, access.calls)
	})

	t.Run("statement execution", func(t *testing.T) {
		operation, err := NewSQLOperation(validSQLOperationConfig())
		require.NoError(t, err)
		access := &recordingExecutionAccess{
			databaseType: DatabaseTypePostgres,
			executeErr:   errors.New("statement failed"),
		}

		err = operation.Execute(context.Background(), newOperationExecution(access,
			Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}))

		assert.ErrorContains(t, err, "statement failed")
		require.Len(t, access.calls, 1)
	})
}

func TestNewSQLOperationRejectsIncompleteContracts(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*SQLOperationConfig)
		errorMatch string
	}{
		{name: "invalid definition", mutate: func(config *SQLOperationConfig) {
			config.Definition.ID = ""
		}, errorMatch: "ID is required"},
		{
			name: "non-atomic operation", mutate: func(config *SQLOperationConfig) {
				config.Definition.InterruptionMode = InterruptionModeResumable
			}, errorMatch: "must be atomic",
		},
		{name: "missing readiness", mutate: func(config *SQLOperationConfig) {
			config.ReadinessChecker = nil
		}, errorMatch: "readiness checker"},
		{name: "missing scope resolver", mutate: func(config *SQLOperationConfig) {
			config.ScopeResolver = nil
		}, errorMatch: "scope resolver"},
		{name: "missing pre-verifier", mutate: func(config *SQLOperationConfig) {
			config.PreVerifier = nil
		}, errorMatch: "pre-verifier"},
		{name: "missing post-verifier", mutate: func(config *SQLOperationConfig) {
			config.PostVerifier = nil
		}, errorMatch: "post-verifier"},
		{name: "missing provider implementation", mutate: func(config *SQLOperationConfig) {
			config.Implementations = nil
		}, errorMatch: "implementation for database type"},
		{name: "empty statements", mutate: func(config *SQLOperationConfig) {
			config.Implementations[DatabaseTypePostgres] = SQLImplementation{}
		}, errorMatch: "at least one statement"},
		{name: "missing statement ID", mutate: func(config *SQLOperationConfig) {
			config.Implementations[DatabaseTypePostgres] = SQLImplementation{
				Statements: []SQLStatement{{Query: "SELECT 1"}},
			}
		}, errorMatch: "ID is required"},
		{name: "missing statement query", mutate: func(config *SQLOperationConfig) {
			config.Implementations[DatabaseTypePostgres] = SQLImplementation{
				Statements: []SQLStatement{{ID: "statement"}},
			}
		}, errorMatch: "query is required"},
		{name: "duplicate statement ID", mutate: func(config *SQLOperationConfig) {
			config.Implementations[DatabaseTypePostgres] = SQLImplementation{Statements: []SQLStatement{
				{ID: "statement", Query: "SELECT 1"}, {ID: "statement", Query: "SELECT 2"},
			}}
		}, errorMatch: "repeats statement ID"},
		{name: "unsupported provider", mutate: func(config *SQLOperationConfig) {
			config.Implementations["mysql"] = SQLImplementation{
				Statements: []SQLStatement{{ID: "statement", Query: "SELECT 1"}},
			}
		}, errorMatch: "unsupported database type"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validSQLOperationConfig()
			test.mutate(&config)

			operation, err := NewSQLOperation(config)

			require.Error(t, err)
			assert.Nil(t, operation)
			assert.ErrorContains(t, err, test.errorMatch)
		})
	}
}

func TestSQLOperationRejectsInvalidScopesAndVerification(t *testing.T) {
	t.Run("invalid resolved scope", func(t *testing.T) {
		config := validSQLOperationConfig()
		config.ScopeResolver = func(context.Context, ReadAccess) ([]Scope, error) {
			return []Scope{{Type: ScopeTypeStore, ID: string(StoreIDConfigDB)}}, nil
		}
		operation, err := NewSQLOperation(config)
		require.NoError(t, err)

		scopes, err := operation.ResolveScopes(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, scopes)
		assert.ErrorContains(t, err, "must use store ID")
	})

	t.Run("scope resolution error", func(t *testing.T) {
		config := validSQLOperationConfig()
		config.ScopeResolver = func(context.Context, ReadAccess) ([]Scope, error) {
			return nil, errors.New("query failed")
		}
		operation, err := NewSQLOperation(config)
		require.NoError(t, err)

		_, err = operation.ResolveScopes(context.Background(), nil)
		assert.ErrorContains(t, err, "query failed")
	})

	t.Run("invalid verification result", func(t *testing.T) {
		config := validSQLOperationConfig()
		config.PreVerifier = func(context.Context, ReadAccess, Scope) (VerificationResult, error) {
			return VerificationResult{State: VerificationInvalid, Summary: "Invalid"}, nil
		}
		operation, err := NewSQLOperation(config)
		require.NoError(t, err)

		_, err = operation.PreVerify(context.Background(), nil,
			Scope{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)})
		assert.ErrorContains(t, err, "requires at least one finding")
	})
}

func validSQLOperationConfig() SQLOperationConfig {
	return SQLOperationConfig{
		Definition: OperationDefinition{
			ID: "2.0.0/entitydb/example", Description: "Apply the example migration",
			StoreID: StoreIDEntityDB, ReadStoreIDs: []StoreID{StoreIDConfigDB},
			InterruptionMode: InterruptionModeAtomic, ScopeType: ScopeTypeStore,
		},
		ReadinessChecker: func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error) {
			return ReadinessResult{State: ReadinessReady, Summary: "Operation is ready"}, nil
		},
		ScopeResolver: func(context.Context, ReadAccess) ([]Scope, error) {
			return []Scope{{Type: ScopeTypeStore, ID: string(StoreIDEntityDB)}}, nil
		},
		PreVerifier: func(context.Context, ReadAccess, Scope) (VerificationResult, error) {
			return VerificationResult{State: VerificationSource, Summary: "Source is valid"}, nil
		},
		Implementations: map[DatabaseType]SQLImplementation{
			DatabaseTypePostgres: {
				Statements: []SQLStatement{{
					ID: "add-migrated-column", Query: "ALTER TABLE example ADD COLUMN migrated BOOLEAN",
				}},
			},
		},
		PostVerifier: func(context.Context, ReadAccess, Scope) (VerificationResult, error) {
			return VerificationResult{State: VerificationTarget, Summary: "Target is valid"}, nil
		},
	}
}

func TestOperationExecutionAccessRestrictsWritesToOwningStore(t *testing.T) {
	entityClient := providermock.NewDBClientInterfaceMock(t)
	configClient := providermock.NewDBClientInterfaceMock(t)
	query := dbmodel.DBQuery{ID: "MIG-TEST-03", PostgresQuery: "UPDATE example SET migrated = TRUE"}
	entityClient.On("ExecuteContext", mock.Anything, query, "argument").Return(int64(2), nil).Once()
	access, err := newOperationExecutionAccess(OperationDefinition{
		StoreID: StoreIDEntityDB, ReadStoreIDs: []StoreID{StoreIDConfigDB},
	}, map[StoreID]ReadStore{
		StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
		StoreIDConfigDB: {DatabaseType: DatabaseTypePostgres, Client: configClient},
	})
	require.NoError(t, err)

	rowsAffected, err := access.Execute(context.Background(), StoreIDEntityDB, query, "argument")
	require.NoError(t, err)
	assert.Equal(t, int64(2), rowsAffected)

	_, err = access.Execute(context.Background(), StoreIDConfigDB, query)
	assert.ErrorContains(t, err, "did not declare write access")
	_, err = access.Execute(context.Background(), StoreIDRuntimePersistent, query)
	assert.ErrorContains(t, err, "did not declare write access")
}

func TestOperationExecutionAccessRejectsMissingProviderQuery(t *testing.T) {
	entityClient := providermock.NewDBClientInterfaceMock(t)
	access, err := newOperationExecutionAccess(OperationDefinition{StoreID: StoreIDEntityDB},
		map[StoreID]ReadStore{
			StoreIDEntityDB: {DatabaseType: DatabaseTypePostgres, Client: entityClient},
		})
	require.NoError(t, err)

	_, err = access.Execute(context.Background(), StoreIDEntityDB, dbmodel.DBQuery{ID: "MIG-TEST-04"})
	assert.ErrorContains(t, err, "has no implementation")
}
