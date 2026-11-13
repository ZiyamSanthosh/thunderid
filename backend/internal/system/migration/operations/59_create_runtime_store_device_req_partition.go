// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"fmt"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	createRuntimeStoreDeviceReqPartitionOperationID engine.OperationID = "runtime_transient/" +
		"59_create_runtime_store_device_req_partition"

	runtimeStoreTable          = "RUNTIME_STORE"
	deviceReqPartitionTable    = "RUNTIME_STORE_DEVICE_REQ"
	deviceReqPartitionBound    = "FOR VALUES IN ('device:req')"
	partitionedRelationKind    = "p"
	deviceReqPartitionConflict = "unexpected_runtime_store_partition"
)

// queryRuntimeStorePartitions lists the partitions of RUNTIME_STORE with their bounds.
var queryRuntimeStorePartitions = dbmodel.DBQuery{
	ID: "MIG-USM-02-01",
	PostgresQuery: `SELECT c.relname AS partition_name, pg_get_expr(c.relpartbound, c.oid) AS bound
		FROM pg_inherits i
		INNER JOIN pg_class c ON c.oid = i.inhrelid
		INNER JOIN pg_class p ON p.oid = i.inhparent
		INNER JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = CURRENT_SCHEMA() AND p.relname = 'RUNTIME_STORE'
		ORDER BY c.relname`,
}

// NewCreateRuntimeStoreDeviceReqPartitionOperation adds the runtime store partition for pending device
// authorization requests. The new binary stores them under the device:req namespace; the previous binary
// never writes that namespace, so the partition is invisible to it. Creating a partition locks the parent
// table, so the statement runs with a short lock timeout and fails, to be retried, instead of waiting behind
// sign-ins.
func NewCreateRuntimeStoreDeviceReqPartitionOperation() (engine.Operation, error) {
	verify := func(ctx context.Context, access engine.ReadAccess, _ engine.ExecutionTarget) (
		engine.VerificationResult, error) {
		return verifyDeviceReqPartition(ctx, access)
	}
	return engine.NewSQLOperation(engine.SQLOperationConfig{
		Definition: engine.OperationDefinition{
			ID:               createRuntimeStoreDeviceReqPartitionOperationID,
			Description:      "Add the runtime store partition for pending device authorization requests",
			StoreID:          engine.StoreIDRuntimeTransient,
			Kind:             engine.OperationKindSchemaChange,
			InterruptionMode: engine.InterruptionModeAtomic,
		},
		ReadinessChecker: func(ctx context.Context, access engine.ReadAccess, _ engine.PlanContext) (
			engine.ReadinessResult, error) {
			return readinessFromVerification(ctx, access,
				func(ctx context.Context, access engine.ReadAccess, _ string) (engine.VerificationResult, error) {
					return verifyDeviceReqPartition(ctx, access)
				})
		},
		PreVerifier: verify,
		Implementations: map[engine.DatabaseType]engine.SQLImplementation{
			engine.DatabaseTypePostgres: {Statements: []engine.SQLStatement{
				{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
				{ID: "create-device-req-partition", Query: `CREATE TABLE "RUNTIME_STORE_DEVICE_REQ"
    PARTITION OF "RUNTIME_STORE" FOR VALUES IN ('device:req')`},
			}},
		},
		PostVerifier: verify,
	})
}

// verifyDeviceReqPartition is at source while the partition is absent and its namespace is free, and at target
// when RUNTIME_STORE_DEVICE_REQ is a partition of RUNTIME_STORE for exactly the device:req namespace.
func verifyDeviceReqPartition(ctx context.Context, access engine.ReadAccess) (engine.VerificationResult, error) {
	parent, err := access.Query(ctx, engine.StoreIDRuntimeTransient, queryTableRelation, runtimeStoreTable)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s relation: %w", runtimeStoreTable, err)
	}
	if len(parent) != 1 {
		return invalidVerification(deviceReqPartitionConflict, fmt.Sprintf(
			"Expected one %s table, found %d", runtimeStoreTable, len(parent))), nil
	}
	kind, err := parent[0].String("relation_kind")
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if kind != partitionedRelationKind {
		return invalidVerification(deviceReqPartitionConflict, fmt.Sprintf(
			"%s is not a partitioned table", runtimeStoreTable)), nil
	}

	partitions, err := access.Query(ctx, engine.StoreIDRuntimeTransient, queryRuntimeStorePartitions)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s partitions: %w", runtimeStoreTable, err)
	}
	for _, row := range partitions {
		name, nameErr := row.String("partition_name")
		bound, boundErr := row.String("bound")
		if err := errors.Join(nameErr, boundErr); err != nil {
			return engine.VerificationResult{}, fmt.Errorf("read %s partition: %w", runtimeStoreTable, err)
		}
		isBound := normalizeExpression(bound) == normalizeExpression(deviceReqPartitionBound)
		switch {
		case name == deviceReqPartitionTable && isBound:
			return engine.VerificationResult{State: engine.VerificationTarget,
				Summary: deviceReqPartitionTable + " is a partition of " + runtimeStoreTable + " for device:req"}, nil
		case name == deviceReqPartitionTable:
			return invalidVerification(deviceReqPartitionConflict, fmt.Sprintf(
				"%s is a partition with unexpected bound %s", deviceReqPartitionTable, bound)), nil
		case isBound:
			return invalidVerification(deviceReqPartitionConflict, fmt.Sprintf(
				"The device:req namespace already belongs to partition %s", name)), nil
		}
	}
	relation, err := access.Query(ctx, engine.StoreIDRuntimeTransient, queryTableRelation, deviceReqPartitionTable)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("inspect %s relation: %w", deviceReqPartitionTable, err)
	}
	if len(relation) != 0 {
		return invalidVerification(deviceReqPartitionConflict, fmt.Sprintf(
			"Relation name %s is already in use and is not a partition of %s", deviceReqPartitionTable,
			runtimeStoreTable)), nil
	}
	return engine.VerificationResult{State: engine.VerificationSource,
		Summary: deviceReqPartitionTable + " is ready to be created"}, nil
}
