// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

func formatMigrationCompatibility(report engine.CompatibilityReport) string {
	var output bytes.Buffer
	fmt.Fprintln(&output, "ThunderID Migration Status")
	fmt.Fprintln(&output)

	table := tabwriter.NewWriter(&output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "STORE\tCOMPLETED\tREQUIRED\tSTAGE\tSTATUS\tCOMPATIBLE\tDETAILS")
	for _, store := range report.Stores {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			store.StoreID,
			displayMigrationValue(store.CompletedLevel),
			displayMigrationValue(store.RequiredLevel),
			displayMigrationValue(string(store.Stage)),
			displayMigrationValue(string(store.Status)),
			yesNo(store.Compatible()),
			migrationDetails(string(store.Reason), store.Err),
		)
	}
	_ = table.Flush()

	fmt.Fprintf(&output, "\nOverall compatibility: %s\n", yesNo(report.Compatible()))
	if report.NextMigrationRelease != "" {
		fmt.Fprintf(&output, "Next migration release: %s\n", report.NextMigrationRelease)
		fmt.Fprintf(&output, "Migration route: %s\n", strings.Join(report.MigrationRoute, " -> "))
	} else if report.RouteErr != nil {
		fmt.Fprintf(&output, "Migration route: unavailable (%s)\n", report.RouteErr)
	}
	return output.String()
}

func formatMigrationPreflight(report engine.PreflightReport) string {
	var output bytes.Buffer
	fmt.Fprintln(&output, "ThunderID Migration Check")
	fmt.Fprintf(&output, "\nTarget release: %s\n", displayMigrationValue(report.TargetVersion))
	fmt.Fprintf(&output, "Phase: %s\n", displayMigrationValue(string(report.Phase)))
	fmt.Fprintf(&output, "Ready: %s\n", yesNo(report.Ready()))

	writeMigrationStoreTable(&output, report.Stores)
	fmt.Fprintln(&output, "\nOperations")
	writeMigrationOperationTable(&output, report.Operations)

	writeMigrationBlockers(&output, report)
	if report.Ready() {
		fmt.Fprintf(&output, "\nNext command: ./thunderid migration apply %s\n", report.Phase)
	}
	return output.String()
}

func formatMigrationWorkflowPreflight(report engine.WorkflowPreflightReport) string {
	var output bytes.Buffer
	fmt.Fprintln(&output, "ThunderID Migration Check")
	fmt.Fprintf(&output, "\nTarget release: %s\n", displayMigrationValue(report.TargetVersion))
	fmt.Fprintln(&output, "Workflow: expand -> migrate")
	fmt.Fprintf(&output, "Ready: %s\n", yesNo(report.Ready()))

	writeMigrationStoreTable(&output, report.Stores)
	for _, phase := range report.Phases {
		fmt.Fprintf(&output, "\n%s operations\n", strings.ToUpper(string(phase.Phase)))
		writeMigrationOperationTable(&output, phase.Operations)
	}

	writeWorkflowMigrationBlockers(&output, report)
	if report.Ready() {
		fmt.Fprintln(&output, "\nNext command: ./thunderid migration apply")
	}
	return output.String()
}

func writeMigrationStoreTable(output *bytes.Buffer, stores []engine.StoreMigrationAssessment) {
	fmt.Fprintln(output, "\nStores")
	storeTable := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(storeTable, "STORE\tCOMPLETED\tACCEPTED SOURCE\tREQUIRED\tSTAGE\tASSESSMENT\tDETAILS")
	for _, store := range stores {
		fmt.Fprintf(storeTable, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			store.StoreID,
			displayMigrationValue(store.CompletedLevel),
			displayMigrationValue(store.AcceptedSourceLevel),
			displayMigrationValue(store.RequiredLevel),
			displayMigrationValue(string(store.Stage)),
			displayMigrationValue(string(store.State)),
			migrationDetails(string(store.Reason), store.Err),
		)
	}
	_ = storeTable.Flush()
}

func writeMigrationOperationTable(output *bytes.Buffer, operations []engine.OperationReadinessReport) {
	if len(operations) == 0 {
		fmt.Fprintln(output, "No operations")
		return
	}
	operationTable := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(operationTable, "OPERATION\tSTORE\tSTATE\tSUMMARY")
	for _, operation := range operations {
		fmt.Fprintf(operationTable, "%s\t%s\t%s\t%s\n",
			operation.OperationID,
			operation.StoreID,
			displayMigrationValue(string(operation.State)),
			migrationDetails(operation.Summary, operation.Err),
		)
	}
	_ = operationTable.Flush()
}

func writeWorkflowMigrationBlockers(output *bytes.Buffer, report engine.WorkflowPreflightReport) {
	blockers := migrationStoreBlockers(report.Stores)
	for _, phase := range report.Phases {
		blockers = append(blockers, migrationOperationBlockers(phase.Operations)...)
	}
	writeMigrationBlockerList(output, blockers)
}

func writeMigrationBlockers(output *bytes.Buffer, report engine.PreflightReport) {
	blockers := migrationStoreBlockers(report.Stores)
	blockers = append(blockers, migrationOperationBlockers(report.Operations)...)
	writeMigrationBlockerList(output, blockers)
}

func migrationStoreBlockers(stores []engine.StoreMigrationAssessment) []string {
	var blockers []string
	for _, store := range stores {
		if !store.Ready() {
			blockers = append(blockers, fmt.Sprintf("Store %s: %s",
				store.StoreID, migrationDetails(string(store.Reason), store.Err)))
		}
	}
	return blockers
}

func migrationOperationBlockers(operations []engine.OperationReadinessReport) []string {
	var blockers []string
	for _, operation := range operations {
		if !operation.Ready() {
			blockers = append(blockers, fmt.Sprintf("Operation %s: %s",
				operation.OperationID, migrationDetails(operation.Summary, operation.Err)))
		}
		for _, finding := range operation.Findings {
			detail := finding.Message
			if finding.Remediation != "" {
				detail += "; remediation: " + finding.Remediation
			}
			blockers = append(blockers, fmt.Sprintf("Operation %s [%s]: %s",
				operation.OperationID, finding.Code, detail))
		}
	}
	return blockers
}

func writeMigrationBlockerList(output *bytes.Buffer, blockers []string) {
	if len(blockers) == 0 {
		return
	}
	fmt.Fprintln(output, "\nBlockers and findings")
	for _, blocker := range blockers {
		fmt.Fprintf(output, "- %s\n", blocker)
	}
}

func migrationDetails(value string, err error) string {
	value = strings.TrimSpace(value)
	if err == nil {
		return displayMigrationValue(value)
	}
	if value == "" {
		return err.Error()
	}
	return value + ": " + err.Error()
}

func displayMigrationValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "YES"
	}
	return "NO"
}
