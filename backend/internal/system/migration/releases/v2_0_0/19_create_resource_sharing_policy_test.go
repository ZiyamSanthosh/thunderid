// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestCreateResourceSharingPolicyOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateResourceSharingPolicyOperation,
		engine.OperationID("2.0.0/configdb/19_create_resource_sharing_policy"), resourceSharingPolicyTable, 2)
}

func TestResourceSharingPolicyUsesDeploymentScopedKeys(t *testing.T) {
	assert.Contains(t, resourceSharingPolicyTable.keys,
		configKeySpec{typeCode: "p", columns: "deployment_id,id"})
	assert.Contains(t, resourceSharingPolicyTable.indexes,
		configIndexSpec{name: "idx_rsp_parent", unique: "false", columns: "deployment_id,parent_policy_id"})
	assert.Contains(t, postgresCreateResourceSharingPolicyTableSQL,
		"PRIMARY KEY (DEPLOYMENT_ID, ID)")
	assert.Contains(t, postgresCreateResourceSharingPolicyParentIndexSQL,
		"(DEPLOYMENT_ID, PARENT_POLICY_ID)")
}
