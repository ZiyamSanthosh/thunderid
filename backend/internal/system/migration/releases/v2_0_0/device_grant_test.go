// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func inboundClientColumnRows(extra ...engine.QueryRow) []engine.QueryRow {
	rows := make([]engine.QueryRow, 0, 2+len(extra))
	rows = append(rows,
		engine.QueryRow{"column_name": "deployment_id", "is_nullable": "NO", "data_type": "character varying",
			"maximum_length": "255", "column_default": "none"},
		engine.QueryRow{"column_name": "auth_flow_id", "is_nullable": "NO", "data_type": "character varying",
			"maximum_length": "100", "column_default": "none"})
	return append(rows, extra...)
}

func deviceGrantColumnRow(nullable, dataType, length, defaultValue string) engine.QueryRow {
	return engine.QueryRow{"column_name": "device_grant_flow_id", "is_nullable": nullable, "data_type": dataType,
		"maximum_length": length, "column_default": defaultValue}
}

func inboundClientAccess(kind string, columns []engine.QueryRow) *fakeAccess {
	return newFakeAccess().
		onArgs(queryTableRelation, func([]interface{}) []engine.QueryRow {
			if kind == "" {
				return nil
			}
			return []engine.QueryRow{{"relation_kind": kind}}
		}).
		on(queryTableColumns, columns...)
}

func TestAddInboundClientDeviceGrantFlowIDOperation(t *testing.T) {
	operation, err := newAddInboundClientDeviceGrantFlowIDOperation()
	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, addInboundClientDeviceGrantFlowIDOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Contains(t, implementation.Statements[1].Query,
		`ALTER TABLE "INBOUND_CLIENT"`)
	assert.Contains(t, implementation.Statements[1].Query, `ADD COLUMN DEVICE_GRANT_FLOW_ID VARCHAR(100)`)
	assert.NotContains(t, implementation.Statements[1].Query, "NOT NULL", "the column is nullable")
	assert.NotContains(t, implementation.Statements[1].Query, "DEFAULT", "and has no default")
}

func TestAddInboundClientDeviceGrantFlowIDVerification(t *testing.T) {
	tests := []struct {
		name   string
		access *fakeAccess
		state  engine.VerificationState
	}{
		{name: "column absent", access: inboundClientAccess("r", inboundClientColumnRows()),
			state: engine.VerificationSource},
		{name: "column present", access: inboundClientAccess("r", inboundClientColumnRows(
			deviceGrantColumnRow("YES", "character varying", "100", "none"))), state: engine.VerificationTarget},
		{name: "column not nullable", access: inboundClientAccess("r", inboundClientColumnRows(
			deviceGrantColumnRow("NO", "character varying", "100", "none"))), state: engine.VerificationInvalid},
		{name: "column of another type", access: inboundClientAccess("r", inboundClientColumnRows(
			deviceGrantColumnRow("YES", "integer", "none", "none"))), state: engine.VerificationInvalid},
		{name: "column too short", access: inboundClientAccess("r", inboundClientColumnRows(
			deviceGrantColumnRow("YES", "character varying", "36", "none"))), state: engine.VerificationInvalid},
		{name: "column with a default", access: inboundClientAccess("r", inboundClientColumnRows(
			deviceGrantColumnRow("YES", "character varying", "100", "'x'::character varying"))),
			state: engine.VerificationInvalid},
		{name: "table missing", access: inboundClientAccess("", nil), state: engine.VerificationInvalid},
		{name: "relation is a view", access: inboundClientAccess("v", nil), state: engine.VerificationInvalid},
	}
	operation, err := newAddInboundClientDeviceGrantFlowIDOperation()
	require.NoError(t, err)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.PreVerify(context.Background(), test.access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)

			readiness, err := operation.(*engine.SQLOperation).CheckReadiness(
				context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			if test.state == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}
}

func partitionRow(name, bound string) engine.QueryRow {
	return engine.QueryRow{"partition_name": name, "bound": bound}
}

func runtimeStoreAccess(parentKind string, partitions []engine.QueryRow, deviceRelationExists bool) *fakeAccess {
	return newFakeAccess().
		onArgs(queryTableRelation, func(args []interface{}) []engine.QueryRow {
			switch args[0] {
			case runtimeStoreTable:
				if parentKind == "" {
					return nil
				}
				return []engine.QueryRow{{"relation_kind": parentKind}}
			case deviceReqPartitionTable:
				if deviceRelationExists {
					return []engine.QueryRow{{"relation_kind": "r"}}
				}
			}
			return nil
		}).
		on(queryRuntimeStorePartitions, partitions...)
}

func TestCreateRuntimeStoreDeviceReqPartitionOperation(t *testing.T) {
	operation, err := newCreateRuntimeStoreDeviceReqPartitionOperation()
	require.NoError(t, err)
	definition := operation.Definition()
	assert.Equal(t, createRuntimeStoreDeviceReqPartitionOperationID, definition.ID)
	assert.Equal(t, engine.StoreIDRuntimeTransient, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)

	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Equal(t, "SET LOCAL lock_timeout = '2s'", implementation.Statements[0].Query)
	assert.Contains(t, implementation.Statements[1].Query, `CREATE TABLE "RUNTIME_STORE_DEVICE_REQ"`)
	assert.Contains(t, implementation.Statements[1].Query, `PARTITION OF "RUNTIME_STORE" FOR VALUES IN ('device:req')`)
}

func TestCreateRuntimeStoreDeviceReqPartitionVerification(t *testing.T) {
	others := []engine.QueryRow{
		partitionRow("RUNTIME_STORE_AUTHZ_REQ", "FOR VALUES IN ('authz:req')"),
		partitionRow("RUNTIME_STORE_PAR_REQ", "FOR VALUES IN ('par:req')"),
	}
	withDevice := func(name, bound string) []engine.QueryRow {
		return append(append([]engine.QueryRow(nil), others...), partitionRow(name, bound))
	}
	tests := []struct {
		name    string
		access  *fakeAccess
		state   engine.VerificationState
		summary string
	}{
		{name: "partition absent", access: runtimeStoreAccess("p", others, false), state: engine.VerificationSource},
		{name: "partition present", access: runtimeStoreAccess("p",
			withDevice("RUNTIME_STORE_DEVICE_REQ", "FOR VALUES IN ('device:req')"), true),
			state: engine.VerificationTarget},
		{name: "partition has another bound", access: runtimeStoreAccess("p",
			withDevice("RUNTIME_STORE_DEVICE_REQ", "FOR VALUES IN ('device:other')"), true),
			state: engine.VerificationInvalid, summary: "unexpected bound"},
		{name: "namespace taken by another partition", access: runtimeStoreAccess("p",
			withDevice("RUNTIME_STORE_OTHER", "FOR VALUES IN ('device:req')"), false),
			state: engine.VerificationInvalid, summary: "already belongs to partition RUNTIME_STORE_OTHER"},
		{name: "name taken by a plain table", access: runtimeStoreAccess("p", others, true),
			state: engine.VerificationInvalid, summary: "not a partition"},
		{name: "parent is not partitioned", access: runtimeStoreAccess("r", others, false),
			state: engine.VerificationInvalid, summary: "not a partitioned table"},
		{name: "parent missing", access: runtimeStoreAccess("", nil, false), state: engine.VerificationInvalid},
	}
	operation, err := newCreateRuntimeStoreDeviceReqPartitionOperation()
	require.NoError(t, err)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.PreVerify(context.Background(), test.access, engine.ExecutionTarget{})
			require.NoError(t, err)
			assert.Equal(t, test.state, result.State, result.Summary)
			assert.Contains(t, result.Summary, test.summary)

			readiness, err := operation.(*engine.SQLOperation).CheckReadiness(
				context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			if test.state == engine.VerificationInvalid {
				assert.Equal(t, engine.ReadinessBlocked, readiness.State)
				assert.Len(t, readiness.Findings, 1)
			} else {
				assert.Equal(t, engine.ReadinessReady, readiness.State)
			}
		})
	}
}
