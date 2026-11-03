// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

type certificateReferenceIDReadAccess struct {
	rows []engine.QueryRow
	err  error
}

func (a certificateReferenceIDReadAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	_ ...interface{},
) ([]engine.QueryRow, error) {
	if a.err != nil {
		return nil, a.err
	}
	if storeID != engine.StoreIDConfigDB || query.GetID() != queryCertificateReferenceIDLength.GetID() {
		return nil, errors.New("unexpected query")
	}
	return a.rows, nil
}

func (certificateReferenceIDReadAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func TestExpandCertificateReferenceIDOperation(t *testing.T) {
	operation, err := newExpandCertificateReferenceIDOperation()
	require.NoError(t, err)

	definition := operation.Definition()
	assert.Equal(t, engine.OperationID("2.0.0/configdb/40_expand_certificate_reference_id"), definition.ID)
	assert.Equal(t, engine.StoreIDConfigDB, definition.StoreID)
	assert.Equal(t, engine.OperationKindSchemaChange, definition.Kind)
	assert.Equal(t, engine.InterruptionModeAtomic, definition.InterruptionMode)
	implementation, err := operation.(*engine.SQLOperation).Implementation(engine.DatabaseTypePostgres)
	require.NoError(t, err)
	require.Len(t, implementation.Statements, 2)
	assert.Contains(t, implementation.Statements[1].Query, `ALTER COLUMN REF_ID TYPE VARCHAR(2048)`)

	source, err := operation.PreVerify(
		context.Background(), certificateReferenceIDLengthAccess(36), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationSource, source.State)

	target, err := operation.PostVerify(
		context.Background(), certificateReferenceIDLengthAccess(2048), engine.ExecutionTarget{})
	require.NoError(t, err)
	assert.Equal(t, engine.VerificationTarget, target.State)
}

func TestExpandCertificateReferenceIDReadiness(t *testing.T) {
	operation, err := newExpandCertificateReferenceIDOperation()
	require.NoError(t, err)

	tests := []struct {
		name          string
		access        certificateReferenceIDReadAccess
		expectedState engine.ReadinessState
		expectedText  string
		findingCount  int
	}{
		{
			name: "source column is ready", access: certificateReferenceIDLengthAccess(36),
			expectedState: engine.ReadinessReady, expectedText: "ready to be expanded",
		},
		{
			name: "target column is ready", access: certificateReferenceIDLengthAccess(2048),
			expectedState: engine.ReadinessReady, expectedText: "already accepts",
		},
		{
			name: "missing column blocks migration", access: certificateReferenceIDReadAccess{},
			expectedState: engine.ReadinessBlocked, expectedText: "Expected one CERTIFICATE.REF_ID column",
			findingCount: 1,
		},
		{
			name: "unexpected length blocks migration", access: certificateReferenceIDLengthAccess(255),
			expectedState: engine.ReadinessBlocked, expectedText: "Unexpected CERTIFICATE.REF_ID maximum length",
			findingCount: 1,
		},
		{
			name: "nullable column blocks migration",
			access: certificateReferenceIDReadAccess{rows: []engine.QueryRow{{
				"is_nullable": "YES", "data_type": "character varying", "maximum_length": int64(36),
			}}},
			expectedState: engine.ReadinessBlocked, expectedText: "Unexpected CERTIFICATE.REF_ID definition",
			findingCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := operation.(*engine.SQLOperation).CheckReadiness(
				context.Background(), test.access, engine.PlanContext{})
			require.NoError(t, err)
			assert.Equal(t, test.expectedState, result.State)
			assert.Contains(t, result.Summary, test.expectedText)
			assert.Len(t, result.Findings, test.findingCount)
			if test.findingCount > 0 {
				assert.Equal(t, "unexpected_certificate_reference_id_length", result.Findings[0].Code)
				assert.NotEmpty(t, result.Findings[0].Remediation)
			}
		})
	}
}

func TestExpandCertificateReferenceIDReadinessPropagatesInspectionFailure(t *testing.T) {
	operation, err := newExpandCertificateReferenceIDOperation()
	require.NoError(t, err)

	result, err := operation.(*engine.SQLOperation).CheckReadiness(
		context.Background(), certificateReferenceIDReadAccess{err: errors.New("database unavailable")},
		engine.PlanContext{})
	require.Error(t, err)
	assert.Empty(t, result.State)
	assert.ErrorContains(t, err, "database unavailable")
}

func certificateReferenceIDLengthAccess(length int64) certificateReferenceIDReadAccess {
	return certificateReferenceIDReadAccess{rows: []engine.QueryRow{{
		"is_nullable": "NO", "data_type": "character varying", "maximum_length": length,
	}}}
}
