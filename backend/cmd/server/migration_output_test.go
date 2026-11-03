// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func TestFormatMigrationCompatibility(t *testing.T) {
	report := engine.CompatibilityReport{Stores: []engine.StoreCompatibility{
		{
			StoreID: engine.StoreIDConfigDB, CompletedLevel: "2", RequiredLevel: "2",
			Stage: engine.MigrationStageComplete, Status: engine.StatusClean,
		},
		{
			StoreID: engine.StoreIDEntityDB, CompletedLevel: "1", RequiredLevel: "2",
			Stage: engine.MigrationStageComplete, Status: engine.StatusClean,
			Reason: engine.CompatibilityReasonLevelMismatch,
		},
	}, NextMigrationRelease: "2.0.0", MigrationRoute: []string{"2.0.0"}}

	output := formatMigrationCompatibility(report)

	assert.Equal(t, `ThunderID Migration Status

STORE     COMPLETED  REQUIRED  STAGE     STATUS  COMPATIBLE  DETAILS
configdb  2          2         complete  clean   YES         -
entitydb  1          2         complete  clean   NO          level_mismatch

Overall compatibility: NO
Next migration release: 2.0.0
Migration route: 2.0.0
`, output)
}

func TestFormatMigrationPreflightReady(t *testing.T) {
	report := engine.PreflightReport{
		TargetVersion: "2.0.0",
		Phase:         engine.OperationPhaseExpand,
		Stores: []engine.StoreMigrationAssessment{{
			StoreID: engine.StoreIDEntityDB, CompletedLevel: "1", AcceptedSourceLevel: "1",
			RequiredLevel: "2", Stage: engine.MigrationStageComplete,
			State: engine.StoreAssessmentMigrationRequired,
		}},
		Operations: []engine.OperationReadinessReport{{
			OperationID: "03-expand-entity-type", StoreID: engine.StoreIDEntityDB,
			State: engine.ReadinessReady, Summary: "Ready to execute",
		}},
	}

	output := formatMigrationPreflight(report)

	assert.Contains(t, output, "ThunderID Migration Check")
	assert.Contains(t, output, "Target release: 2.0.0")
	assert.Contains(t, output, "Ready: YES")
	assert.Contains(t, output, "entitydb  1          1                2")
	assert.Contains(t, output, "03-expand-entity-type  entitydb  ready  Ready to execute")
	assert.Contains(t, output, "Next command: ./thunderid migration apply expand")
	assert.NotContains(t, output, "Blockers and findings")
}

func TestFormatMigrationPreflightBlocked(t *testing.T) {
	report := engine.PreflightReport{
		TargetVersion: "2.0.0",
		Phase:         engine.OperationPhaseMigrate,
		Stores: []engine.StoreMigrationAssessment{{
			StoreID: engine.StoreIDEntityDB, State: engine.StoreAssessmentBlocked,
			Reason: engine.CompatibilityReasonMetadataMissing,
		}},
		Operations: []engine.OperationReadinessReport{{
			OperationID: "05-backfill", StoreID: engine.StoreIDEntityDB,
			State: engine.ReadinessBlocked, Summary: "Source column is unavailable",
			Err: errors.New("query failed"),
			Findings: []engine.Finding{{
				Code: "source_missing", Message: "The source column does not exist",
				Remediation: "run the expand phase first",
			}},
		}},
	}

	output := formatMigrationPreflight(report)

	assert.Contains(t, output, "Ready: NO")
	assert.Contains(t, output, "Blockers and findings")
	assert.Contains(t, output, "- Store entitydb: metadata_missing")
	assert.Contains(t, output, "- Operation 05-backfill: Source column is unavailable: query failed")
	assert.Contains(t, output,
		"- Operation 05-backfill [source_missing]: The source column does not exist; "+
			"remediation: run the expand phase first")
	assert.NotContains(t, output, "Next command:")
}

func TestFormatMigrationWorkflowPreflight(t *testing.T) {
	report := engine.WorkflowPreflightReport{
		TargetVersion: "2.0.0",
		Stores: []engine.StoreMigrationAssessment{{
			StoreID: engine.StoreIDConfigDB, CompletedLevel: "1.0.0", AcceptedSourceLevel: "1.0.0",
			RequiredLevel: "2.0.0", Stage: engine.MigrationStageComplete,
			State: engine.StoreAssessmentMigrationRequired,
		}},
		Phases: []engine.WorkflowPhasePreflight{
			{
				Phase: engine.OperationPhaseExpand,
				Operations: []engine.OperationReadinessReport{{
					OperationID: "03-add-handle", StoreID: engine.StoreIDConfigDB,
					State: engine.ReadinessReady, Summary: "Ready to execute",
				}},
			},
			{
				Phase: engine.OperationPhaseMigrate,
				Operations: []engine.OperationReadinessReport{{
					OperationID: "05-backfill-handle", StoreID: engine.StoreIDConfigDB,
					State: engine.ReadinessDeferred, Summary: "Checked after expansion",
				}},
			},
		},
	}

	output := formatMigrationWorkflowPreflight(report)

	assert.Contains(t, output, "Workflow: expand -> migrate")
	assert.Contains(t, output, "Ready: YES")
	assert.Contains(t, output, "EXPAND operations")
	assert.Contains(t, output, "03-add-handle")
	assert.Contains(t, output, "MIGRATE operations")
	assert.Contains(t, output, "05-backfill-handle")
	assert.Contains(t, output, "Next command: ./thunderid migration apply")
	assert.NotContains(t, output, "contract")
}
