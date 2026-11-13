// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

const addInboundClientDeviceGrantFlowIDOperationID engine.OperationID = "configdb/" +
	"58_add_inbound_client_device_grant_flow_id"

// NewAddInboundClientDeviceGrantFlowIDOperation adds the column that stores each application's Device Grant
// flow. It is nullable with no default, so existing applications stay valid, have no Device Grant flow, and
// need no data migration. The column is appended to the table; the schema scripts list it after AUTH_FLOW_ID,
// which no query depends on.
func NewAddInboundClientDeviceGrantFlowIDOperation() (engine.Operation, error) {
	return newAddColumnOperation(addColumnSpec{
		id:          addInboundClientDeviceGrantFlowIDOperationID,
		description: "Add the Device Grant flow ID to inbound clients",
		storeID:     engine.StoreIDConfigDB,
		table:       "INBOUND_CLIENT",
		columns: []columnSpec{{
			name: "device_grant_flow_id", nullable: "YES", dataType: "character varying",
			maximumLength: "100", defaultValue: "none",
		}},
		statements: []engine.SQLStatement{
			{ID: "set-lock-timeout", Query: "SET LOCAL lock_timeout = '2s'"},
			{ID: "add-device-grant-flow-id", Query: `ALTER TABLE "INBOUND_CLIENT"
    ADD COLUMN DEVICE_GRANT_FLOW_ID VARCHAR(100)`},
		},
	})
}
