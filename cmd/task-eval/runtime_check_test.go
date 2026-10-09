package main

import (
	"encoding/json"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func TestSuccessfulTasksCannotHideConfigurationFailure(t *testing.T) {
	report := taskbench.Report{ConfigurationErrors: []string{"observed model differs"}}
	report.Metrics.Total, report.Metrics.Completed = 1, 1
	if got := reportExitCode(report); got != 2 {
		t.Fatalf("invalid comparison exited %d, want infrastructure failure", got)
	}
}

func TestActualRoutingMustMatchPlannedMode(t *testing.T) {
	for _, actual := range []string{"legacy", "jev_hint", "jev_selective", ""} {
		t.Run(actual, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"routing_mode": actual})
			report := taskbench.Report{Runtime: raw}
			validateReportRouting(&report, "model")
			if len(report.ConfigurationErrors) == 0 {
				t.Fatal("contradictory or missing observed routing accepted")
			}
		})
	}
}

func TestMatchingRuntimePreservesTaskFailureExitCode(t *testing.T) {
	report := taskbench.Report{Runtime: json.RawMessage(`{"routing_mode":"model"}`)}
	validateReportRouting(&report, "model")
	report.Metrics.Total = 1
	if got := reportExitCode(report); got != 1 {
		t.Fatalf("task failure exit=%d", got)
	}
	report.Metrics.Completed = 1
	if got := reportExitCode(report); got != 0 {
		t.Fatalf("valid success exit=%d", got)
	}
}
