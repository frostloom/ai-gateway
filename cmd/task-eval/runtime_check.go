package main

import (
	"encoding/json"
	"fmt"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func validateReportRouting(report *taskbench.Report, planned string) {
	var observed struct {
		RoutingMode string `json:"routing_mode"`
	}
	if err := json.Unmarshal(report.Runtime, &observed); err != nil || observed.RoutingMode == "" {
		report.ConfigurationErrors = append(report.ConfigurationErrors, "missing observed routing mode")
	} else if observed.RoutingMode != planned {
		report.ConfigurationErrors = append(report.ConfigurationErrors, fmt.Sprintf("observed routing %s differs from planned %s", observed.RoutingMode, planned))
	}
}

func reportExitCode(report taskbench.Report) int {
	if len(report.ConfigurationErrors) > 0 {
		return 2
	}
	if report.Metrics.Completed != report.Metrics.Total {
		return 1
	}
	return 0
}
