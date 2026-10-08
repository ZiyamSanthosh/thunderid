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

// ExecutionTarget identifies one independently verified execution of an operation.
// An empty deployment ID means that the operation targets the complete store.
type ExecutionTarget struct {
	DeploymentID string
}

// VerificationState classifies the observed representation of one operation target.
type VerificationState string

const (
	// VerificationSource means that the target is at the exact state accepted for execution.
	VerificationSource VerificationState = "source"
	// VerificationTarget means that the execution target already has the exact required result.
	VerificationTarget VerificationState = "target"
	// VerificationInvalid means that the execution target is neither a recognized source nor result.
	VerificationInvalid VerificationState = "invalid"
)

// VerificationResult is the standardized result of pre-execution or post-execution verification.
type VerificationResult struct {
	State    VerificationState
	Summary  string
	Findings []Finding
}

// ExecutionAccess provides read access to declared stores and write access only to the owning store.
type ExecutionAccess interface {
	ReadAccess
	Execute(context.Context, StoreID, dbmodel.DBQuery, ...interface{}) (int64, error)
}

// OperationExecution contains the engine-controlled resources for one operation target.
type OperationExecution struct {
	access ExecutionAccess
	target ExecutionTarget
}

func newOperationExecution(access ExecutionAccess, target ExecutionTarget) OperationExecution {
	return OperationExecution{access: access, target: target}
}

// Access returns the restricted database access for the operation.
func (e OperationExecution) Access() ExecutionAccess {
	return e.access
}

// Target returns the target currently being executed.
func (e OperationExecution) Target() ExecutionTarget {
	return e.target
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

// ReadinessFunc performs an operation's plan-wide read-only readiness check.
type ReadinessFunc func(context.Context, ReadAccess, PlanContext) (ReadinessResult, error)

// TargetResolver converts the migration-run selection into the targets executed by an operation.
type TargetResolver func(context.Context, ReadAccess, MigrationSelection) ([]ExecutionTarget, error)

// VerificationFunc verifies one operation target without modifying application data.
type VerificationFunc func(context.Context, ReadAccess, ExecutionTarget) (VerificationResult, error)

// SQLArgsFunc derives bound statement arguments from the current operation target.
type SQLArgsFunc func(ExecutionTarget) ([]interface{}, error)

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
	TargetResolver   TargetResolver
	PreVerifier      VerificationFunc
	Implementations  map[DatabaseType]SQLImplementation
	PostVerifier     VerificationFunc
}

// SQLOperation is an immutable atomic operation executed by the migration engine.
type SQLOperation struct {
	definition       OperationDefinition
	readinessChecker ReadinessFunc
	targetResolver   TargetResolver
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
	if config.Definition.Kind == OperationKindSchemaChange && config.TargetResolver != nil {
		return nil, errors.New("create SQL operation: schema-change operation cannot define a target resolver")
	}
	if config.Definition.Kind == OperationKindDataChange && config.TargetResolver == nil {
		return nil, errors.New("create SQL operation: data-change operation requires a target resolver")
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
		targetResolver: config.TargetResolver, preVerifier: config.PreVerifier,
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
	if o.readinessChecker == nil {
		return ReadinessResult{
			State: ReadinessReady, Summary: "No additional operation readiness check is required",
		}, nil
	}
	return o.readinessChecker(ctx, access, planContext)
}

// ResolveTargets converts one migration selection into validated operation targets.
func (o *SQLOperation) ResolveTargets(
	ctx context.Context,
	access ReadAccess,
	selection MigrationSelection,
) ([]ExecutionTarget, error) {
	if err := validateMigrationSelection(selection); err != nil {
		return nil, fmt.Errorf("resolve targets for operation %q: %w", o.definition.ID, err)
	}
	if o.definition.Kind == OperationKindSchemaChange {
		return []ExecutionTarget{{}}, nil
	}
	selection.DeploymentIDs = append([]string(nil), selection.DeploymentIDs...)
	targets, err := o.targetResolver(ctx, access, selection)
	if err != nil {
		return nil, fmt.Errorf("resolve targets for operation %q: %w", o.definition.ID, err)
	}
	if err := validateExecutionTargets(o.definition, targets); err != nil {
		return nil, err
	}
	return append([]ExecutionTarget(nil), targets...), nil
}

// PreVerify verifies that one target is at an exact source or target state before execution.
func (o *SQLOperation) PreVerify(
	ctx context.Context,
	access ReadAccess,
	target ExecutionTarget,
) (VerificationResult, error) {
	return o.verify(ctx, access, target, "pre", o.preVerifier)
}

// Execute applies the provider-specific statements for one target in their declared order.
func (o *SQLOperation) Execute(ctx context.Context, execution OperationExecution) error {
	access := execution.Access()
	target := execution.Target()
	if access == nil {
		return fmt.Errorf("execute operation %q: execution access is required", o.definition.ID)
	}
	if err := validateExecutionTarget(o.definition, target); err != nil {
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
			args, err = statement.Args(target)
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

// PostVerify verifies one target after execution.
func (o *SQLOperation) PostVerify(
	ctx context.Context,
	access ReadAccess,
	target ExecutionTarget,
) (VerificationResult, error) {
	return o.verify(ctx, access, target, "post", o.postVerifier)
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
	target ExecutionTarget,
	phase string,
	verifier VerificationFunc,
) (VerificationResult, error) {
	if err := validateExecutionTarget(o.definition, target); err != nil {
		return VerificationResult{}, err
	}
	result, err := verifier(ctx, access, target)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("%s-verify operation %q target %q: %w",
			phase, o.definition.ID, executionTargetName(target), err)
	}
	if err := result.validate(); err != nil {
		return VerificationResult{}, fmt.Errorf("%s-verify operation %q target %q: %w",
			phase, o.definition.ID, executionTargetName(target), err)
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

func validateMigrationSelection(selection MigrationSelection) error {
	seen := make(map[string]struct{}, len(selection.DeploymentIDs))
	for _, deploymentID := range selection.DeploymentIDs {
		if strings.TrimSpace(deploymentID) == "" {
			return errors.New("migration selection contains an empty deployment ID")
		}
		if deploymentID != strings.TrimSpace(deploymentID) {
			return fmt.Errorf("migration selection deployment ID %q contains surrounding whitespace", deploymentID)
		}
		if len([]rune(deploymentID)) > maxDeploymentIDLength {
			return fmt.Errorf("migration selection deployment ID exceeds %d characters", maxDeploymentIDLength)
		}
		if _, exists := seen[deploymentID]; exists {
			return fmt.Errorf("migration selection repeats deployment ID %q", deploymentID)
		}
		seen[deploymentID] = struct{}{}
	}
	return nil
}

func validateExecutionTargets(definition OperationDefinition, targets []ExecutionTarget) error {
	if len(targets) == 0 {
		return fmt.Errorf("operation %q resolved no execution targets", definition.ID)
	}
	seen := make(map[string]struct{}, len(targets))
	completeStoreTarget := false
	for _, target := range targets {
		if err := validateExecutionTarget(definition, target); err != nil {
			return err
		}
		if target.DeploymentID == "" {
			completeStoreTarget = true
		}
		if _, exists := seen[target.DeploymentID]; exists {
			return fmt.Errorf("operation %q resolved duplicate execution target %q",
				definition.ID, executionTargetName(target))
		}
		seen[target.DeploymentID] = struct{}{}
	}
	if completeStoreTarget && len(targets) > 1 {
		return fmt.Errorf("operation %q cannot combine a complete-store target with deployment targets", definition.ID)
	}
	return nil
}

func validateExecutionTarget(definition OperationDefinition, target ExecutionTarget) error {
	if target.DeploymentID != strings.TrimSpace(target.DeploymentID) {
		return fmt.Errorf("operation %q resolved deployment target %q with surrounding whitespace",
			definition.ID, target.DeploymentID)
	}
	return nil
}

func executionTargetName(target ExecutionTarget) string {
	if target.DeploymentID == "" {
		return "complete_store"
	}
	return target.DeploymentID
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
