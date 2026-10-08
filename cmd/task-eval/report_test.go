package main

import (
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func TestReportStatesTaskDenominatorsAndSuccessfulLatency(t *testing.T) {
	r := taskbench.Report{Metrics: taskbench.Metrics{
		CategoryMetrics:       taskbench.CategoryMetrics{Total: 3, Completed: 2, CompletionRate: 2.0 / 3},
		ClarificationRequired: 2, ClarificationOffered: 2, ClarificationCompleted: 1,
		SuccessfulTasks: taskbench.Latencies{Count: 2, P50MS: 1000, P95MS: 2000},
		AllTasks:        taskbench.Latencies{Count: 3, P95MS: 9000},
	}}
	got := renderReport(r)
	for _, want := range []string{"2/3", "66.67%", "成功任务", "2000", "全部尝试", "9000", "2/2", "追问后任务完成", "1/2", "重复确认任务", "模拟用户思考时间", "固定任务回归集"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in report: %s", want, got)
		}
	}
}

func TestReportZeroSuccessCannotClaimZeroLatencyOrBusinessAbility(t *testing.T) {
	got := renderReport(taskbench.Report{Metrics: taskbench.Metrics{CategoryMetrics: taskbench.CategoryMetrics{Total: 2}, FixtureFailures: 2}})
	for _, want := range []string{"0/2", "不可评估", "fixture失败", "参数正确预览"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in report: %s", want, got)
		}
	}
}
