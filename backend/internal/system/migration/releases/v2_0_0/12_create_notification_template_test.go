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

func TestCreateNotificationTemplateOperation(t *testing.T) {
	assertConfigTableOperation(t, newCreateNotificationTemplateOperation,
		engine.OperationID("2.0.0/configdb/12_create_notification_template"), notificationTemplateTable, 1)

	operation, err := newCreateNotificationTemplateOperation()
	require.NoError(t, err)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	assert.Contains(t, implementation.Statements[0].Query, `PRIMARY KEY (DEPLOYMENT_ID, ID)`)
	assert.Contains(t, implementation.Statements[0].Query, `UNIQUE (DEPLOYMENT_ID, CHANNEL, HANDLE)`)
}

func TestCreateNotificationTemplateRejectsConflictingGeneratedName(t *testing.T) {
	operation, err := newCreateNotificationTemplateOperation()
	require.NoError(t, err)

	readiness, err := operation.(*engine.SQLOperation).CheckReadiness(context.Background(), configTableReadAccess{
		table: notificationTemplateTable, absent: true,
		presentRelations: map[string]bool{notificationTemplatePrimaryKeyName: true},
	}, engine.PlanContext{})
	require.NoError(t, err)
	assert.Equal(t, engine.ReadinessBlocked, readiness.State)
	assert.Contains(t, readiness.Summary, "already in use")
}
