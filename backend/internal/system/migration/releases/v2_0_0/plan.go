// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import "github.com/thunder-id/thunderid/internal/system/migration/engine"

func migrationPlan() (engine.MigrationPlan, error) {
	removeReasonCheck, err := newRemoveRevocationReasonCheckOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	createAuthorizationPDPConnection, err := newCreateAuthorizationPDPConnectionOperation()
	if err != nil {
		return engine.MigrationPlan{}, err
	}
	return engine.MigrationPlan{
		TargetVersion: "2.0.0",
		Operations: []engine.Operation{
			removeReasonCheck,
			createAuthorizationPDPConnection,
		},
	}, nil
}
