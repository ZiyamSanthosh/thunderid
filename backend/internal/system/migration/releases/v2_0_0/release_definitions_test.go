// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const planDefinitionsGolden = "testdata/plan_definitions.golden"

// TestPlanDefinitionsAreFrozen pins what every operation of the plan declares: its phase, position, id,
// description, store, read stores, kind and interruption mode. The operations are shared by every release, so
// a change to one that this release lists changes this release. A change that shows here is either a new
// operation, which belongs in the plan on purpose, or an edit of a shipped one, which belongs in a new
// operation. Set UPDATE_PLAN_GOLDEN=1 to rewrite the file when the plan is still in development.
func TestPlanDefinitionsAreFrozen(t *testing.T) {
	definition, err := Definition()
	require.NoError(t, err)

	var lines []string
	record := func(phase string, operations []engine.Operation) {
		for position, operation := range operations {
			lines = append(lines, describeOperation(phase, position+1, operation.Definition()))
		}
	}
	record("expand", definition.Plan.Expand)
	record("migrate", definition.Plan.Migrate)
	record("contract", definition.Plan.Contract)
	for position, operation := range definition.Plan.Rollback {
		lines = append(lines, describeOperation("rollback", position+1, operation.Definition()))
	}
	actual := strings.Join(lines, "\n") + "\n"

	if os.Getenv("UPDATE_PLAN_GOLDEN") == "1" {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(planDefinitionsGolden, []byte(actual), 0o600))
	}
	expected, err := os.ReadFile(planDefinitionsGolden)
	require.NoError(t, err)
	require.Equal(t, string(expected), actual,
		"the plan definitions changed; shipped operations are frozen, so add a new operation instead of editing one")
}

func describeOperation(phase string, position int, definition engine.OperationDefinition) string {
	readStores := make([]string, 0, len(definition.ReadStoreIDs))
	for _, store := range definition.ReadStoreIDs {
		readStores = append(readStores, string(store))
	}
	return fmt.Sprintf("%s|%03d|%s|%s|%s|%s|%s|%s", phase, position, definition.ID, definition.StoreID,
		strings.Join(readStores, ","), definition.Kind, definition.InterruptionMode, definition.Description)
}
