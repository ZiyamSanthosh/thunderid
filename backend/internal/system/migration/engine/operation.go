// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
)

// Scope identifies one independently verified unit of an operation.
type Scope struct {
	Type ScopeType
	ID   string
}

// VerificationState classifies the observed representation of one operation scope.
type VerificationState string

const (
	// VerificationSource means that the scope is at the exact state accepted for execution.
	VerificationSource VerificationState = "source"
	// VerificationTarget means that the scope already has the exact required result.
	VerificationTarget VerificationState = "target"
	// VerificationInvalid means that the scope is neither a recognized source nor target.
	VerificationInvalid VerificationState = "invalid"
)

// VerificationResult is the standardized result of pre-execution or post-execution verification.
type VerificationResult struct {
	State    VerificationState
	Summary  string
	Findings []Finding
}

// ReadinessFunc performs an operation's plan-wide read-only readiness check.
type ReadinessFunc func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error)

// ScopeResolver discovers the complete stable scopes owned by an operation.
type ScopeResolver func(context.Context, ReadAccess) ([]Scope, error)

// VerificationFunc verifies one operation scope without modifying application data.
type VerificationFunc func(context.Context, ReadAccess, Scope) (VerificationResult, error)

// SQLArgsFunc derives bound statement arguments from the current operation scope.
type SQLArgsFunc func(Scope) ([]interface{}, error)

// SQLStatement is one ordered provider-specific statement of an SQL operation.
type SQLStatement struct {
	ID    string
	Query string
	Args  SQLArgsFunc
}

// SQLImplementation contains the ordered statements for one database provider.
type SQLImplementation struct {
	Statements []SQLStatement
}

// SQLOperationConfig contains the complete contract required to construct an atomic SQL operation.
type SQLOperationConfig struct {
	Definition       OperationDefinition
	ReadinessChecker ReadinessFunc
	ScopeResolver    ScopeResolver
	PreVerifier      VerificationFunc
	Implementations  map[DatabaseType]SQLImplementation
	PostVerifier     VerificationFunc
}

// SQLOperation is an immutable atomic operation executed by the migration engine.
type SQLOperation struct {
	definition       OperationDefinition
	readinessChecker ReadinessFunc
	scopeResolver    ScopeResolver
	preVerifier      VerificationFunc
	implementations  map[DatabaseType]SQLImplementation
	postVerifier     VerificationFunc
}

// NewSQLOperation validates and constructs an atomic provider-explicit SQL operation.
func NewSQLOperation(config SQLOperationConfig) (*SQLOperation, error) {
	knownStores := make(map[StoreID]StoreRequirement, len(supportedStoreIDs))
	for _, storeID := range supportedStoreIDs {
		knownStores[storeID] = StoreRequirement{ID: storeID}
	}
	if err := validateOperationDefinition(config.Definition, knownStores); err != nil {
		return nil, fmt.Errorf("create SQL operation: %w", err)
	}
	if config.Definition.InterruptionMode != InterruptionModeAtomic {
		return nil, errors.New("create SQL operation: interruption mode must be atomic")
	}
	if config.ReadinessChecker == nil {
		return nil, errors.New("create SQL operation: readiness checker is required")
	}
	if config.ScopeResolver == nil {
		return nil, errors.New("create SQL operation: scope resolver is required")
	}
	if config.PreVerifier == nil {
		return nil, errors.New("create SQL operation: pre-verifier is required")
	}
	if config.PostVerifier == nil {
		return nil, errors.New("create SQL operation: post-verifier is required")
	}

	implementations, err := validateAndCopySQLImplementations(config.Implementations)
	if err != nil {
		return nil, err
	}
	definition := config.Definition
	definition.ReadStoreIDs = append([]StoreID(nil), config.Definition.ReadStoreIDs...)
	return &SQLOperation{
		definition: definition, readinessChecker: config.ReadinessChecker,
		scopeResolver: config.ScopeResolver, preVerifier: config.PreVerifier,
		implementations: implementations, postVerifier: config.PostVerifier,
	}, nil
}

// Definition returns an independent copy of the operation definition.
func (o *SQLOperation) Definition() OperationDefinition {
	definition := o.definition
	definition.ReadStoreIDs = append([]StoreID(nil), o.definition.ReadStoreIDs...)
	return definition
}

// CheckReadiness performs the operation's plan-wide read-only readiness check.
func (o *SQLOperation) CheckReadiness(
	ctx context.Context,
	access ReadAccess,
	planContext PlanContext,
) (ReadinessResult, error) {
	return o.readinessChecker(ctx, access, planContext)
}

// ResolveScopes discovers and validates all stable scopes for this operation.
func (o *SQLOperation) ResolveScopes(ctx context.Context, access ReadAccess) ([]Scope, error) {
	scopes, err := o.scopeResolver(ctx, access)
	if err != nil {
		return nil, fmt.Errorf("resolve scopes for operation %q: %w", o.definition.ID, err)
	}
	if err := validateScopes(o.definition, scopes); err != nil {
		return nil, err
	}
	return append([]Scope(nil), scopes...), nil
}

// PreVerify verifies that one scope is at an exact source or target state before execution.
func (o *SQLOperation) PreVerify(
	ctx context.Context,
	access ReadAccess,
	scope Scope,
) (VerificationResult, error) {
	return o.verify(ctx, access, scope, "pre", o.preVerifier)
}

// Execute applies the provider-specific statements for one scope in their declared order.
func (o *SQLOperation) Execute(ctx context.Context, execution OperationExecution) error {
	access := execution.Access()
	scope := execution.Scope()
	if access == nil {
		return fmt.Errorf("execute operation %q: execution access is required", o.definition.ID)
	}
	if err := validateScope(o.definition, scope); err != nil {
		return err
	}
	databaseType, err := access.DatabaseType(o.definition.StoreID)
	if err != nil {
		return fmt.Errorf("execute operation %q: resolve database type: %w", o.definition.ID, err)
	}
	implementation, err := o.Implementation(databaseType)
	if err != nil {
		return err
	}

	for _, statement := range implementation.Statements {
		var args []interface{}
		if statement.Args != nil {
			args, err = statement.Args(scope)
			if err != nil {
				return fmt.Errorf("execute operation %q statement %q: build arguments: %w",
					o.definition.ID, statement.ID, err)
			}
		}
		query := dbmodel.DBQuery{
			ID:    fmt.Sprintf("%s/%s", o.definition.ID, statement.ID),
			Query: statement.Query,
		}
		if _, err := access.Execute(ctx, o.definition.StoreID, query, args...); err != nil {
			return fmt.Errorf("execute operation %q statement %q: %w", o.definition.ID, statement.ID, err)
		}
	}
	return nil
}

// PostVerify verifies one scope after execution.
func (o *SQLOperation) PostVerify(
	ctx context.Context,
	access ReadAccess,
	scope Scope,
) (VerificationResult, error) {
	return o.verify(ctx, access, scope, "post", o.postVerifier)
}

// Implementation returns an independent provider-specific SQL implementation.
func (o *SQLOperation) Implementation(databaseType DatabaseType) (SQLImplementation, error) {
	implementation, exists := o.implementations[databaseType]
	if !exists {
		return SQLImplementation{}, fmt.Errorf("operation %q has no SQL implementation for database type %q",
			o.definition.ID, databaseType)
	}
	return copySQLImplementation(implementation), nil
}

func (o *SQLOperation) verify(
	ctx context.Context,
	access ReadAccess,
	scope Scope,
	phase string,
	verifier VerificationFunc,
) (VerificationResult, error) {
	if err := validateScope(o.definition, scope); err != nil {
		return VerificationResult{}, err
	}
	result, err := verifier(ctx, access, scope)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("%s-verify operation %q scope %q: %w",
			phase, o.definition.ID, scope.ID, err)
	}
	if err := result.validate(); err != nil {
		return VerificationResult{}, fmt.Errorf("%s-verify operation %q scope %q: %w",
			phase, o.definition.ID, scope.ID, err)
	}
	return result, nil
}

func validateAndCopySQLImplementations(
	implementations map[DatabaseType]SQLImplementation,
) (map[DatabaseType]SQLImplementation, error) {
	result := make(map[DatabaseType]SQLImplementation, len(implementations))
	for databaseType := range supportedMigrationDatabaseTypes {
		implementation, exists := implementations[databaseType]
		if !exists {
			return nil, fmt.Errorf("create SQL operation: implementation for database type %q is required", databaseType)
		}
		if len(implementation.Statements) == 0 {
			return nil, fmt.Errorf("create SQL operation: database type %q requires at least one statement", databaseType)
		}
		statementIDs := make(map[string]struct{}, len(implementation.Statements))
		for index, statement := range implementation.Statements {
			if strings.TrimSpace(statement.ID) == "" {
				return nil, fmt.Errorf("create SQL operation: database type %q statement %d ID is required",
					databaseType, index)
			}
			if strings.TrimSpace(statement.Query) == "" {
				return nil, fmt.Errorf("create SQL operation: database type %q statement %q query is required",
					databaseType, statement.ID)
			}
			if _, exists := statementIDs[statement.ID]; exists {
				return nil, fmt.Errorf("create SQL operation: database type %q repeats statement ID %q",
					databaseType, statement.ID)
			}
			statementIDs[statement.ID] = struct{}{}
		}
		result[databaseType] = copySQLImplementation(implementation)
	}
	for databaseType := range implementations {
		if _, supported := supportedMigrationDatabaseTypes[databaseType]; !supported {
			return nil, fmt.Errorf("create SQL operation: unsupported database type %q", databaseType)
		}
	}
	return result, nil
}

func copySQLImplementation(implementation SQLImplementation) SQLImplementation {
	return SQLImplementation{Statements: append([]SQLStatement(nil), implementation.Statements...)}
}

func validateScopes(definition OperationDefinition, scopes []Scope) error {
	if len(scopes) == 0 {
		return fmt.Errorf("operation %q resolved no scopes", definition.ID)
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if err := validateScope(definition, scope); err != nil {
			return err
		}
		if _, exists := seen[scope.ID]; exists {
			return fmt.Errorf("operation %q resolved duplicate scope %q", definition.ID, scope.ID)
		}
		seen[scope.ID] = struct{}{}
	}
	if definition.ScopeType == ScopeTypeStore && len(scopes) != 1 {
		return fmt.Errorf("store-scoped operation %q must resolve exactly one scope", definition.ID)
	}
	return nil
}

func validateScope(definition OperationDefinition, scope Scope) error {
	if scope.Type != definition.ScopeType {
		return fmt.Errorf("operation %q resolved scope %q with type %q instead of %q",
			definition.ID, scope.ID, scope.Type, definition.ScopeType)
	}
	if strings.TrimSpace(scope.ID) == "" {
		return fmt.Errorf("operation %q resolved a scope without an ID", definition.ID)
	}
	if scope.Type == ScopeTypeStore && scope.ID != string(definition.StoreID) {
		return fmt.Errorf("store-scoped operation %q must use store ID %q as its scope ID",
			definition.ID, definition.StoreID)
	}
	return nil
}

func (r VerificationResult) validate() error {
	if r.State != VerificationSource && r.State != VerificationTarget && r.State != VerificationInvalid {
		return fmt.Errorf("unsupported verification state %q", r.State)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return errors.New("verification summary is required")
	}
	for index, finding := range r.Findings {
		if strings.TrimSpace(finding.Code) == "" {
			return fmt.Errorf("verification finding %d code is required", index)
		}
		if strings.TrimSpace(finding.Message) == "" {
			return fmt.Errorf("verification finding %d message is required", index)
		}
	}
	if r.State == VerificationInvalid && len(r.Findings) == 0 {
		return errors.New("invalid verification requires at least one finding")
	}
	return nil
}
