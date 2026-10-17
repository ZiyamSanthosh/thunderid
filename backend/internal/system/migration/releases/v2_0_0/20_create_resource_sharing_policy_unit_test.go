// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceSharingPolicyUnitOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceSharingPolicyUnitOperation,
		engine.OperationID("2.0.0/configdb/20_create_resource_sharing_policy_unit"), resourceSharingPolicyUnitTable, 2)
}

func TestResourceSharingPolicyUnitUsesDeploymentScopedKeys(t *testing.T) {
	assert.Contains(t, resourceSharingPolicyUnitTable.keys,
		configKeySpec{typeCode: "p", columns: "deployment_id,policy_id,ou_id"})
	assert.Contains(t, resourceSharingPolicyUnitTable.keys, configKeySpec{
		typeCode: "f", columns: "deployment_id,policy_id", referencedTable: "resource_sharing_policy",
		referencedColumns: "deployment_id,id", deleteAction: "c",
	})
	assert.Contains(t, postgresCreateResourceSharingPolicyUnitTableSQL,
		"PRIMARY KEY (DEPLOYMENT_ID, POLICY_ID, OU_ID)")
	assert.Contains(t, postgresCreateResourceSharingPolicyUnitTableSQL,
		"FOREIGN KEY (DEPLOYMENT_ID, POLICY_ID)")
	assert.Contains(t, postgresCreateResourceSharingPolicyUnitTableSQL,
		"REFERENCES \"RESOURCE_SHARING_POLICY\" (DEPLOYMENT_ID, ID) ON DELETE CASCADE")
}
