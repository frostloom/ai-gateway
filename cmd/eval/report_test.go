package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func TestIntentReportDoesNotCallNecessaryClarificationBusinessFailure(t *testing.T) {
	p := agent.IntentPrediction{Sample: agent.IntentSample{ID: "missing-amount", Text: "帮我充值", Intent: "recharge", NeedsConfirm: true}, HTTPCode: 200, Reply: "请问要充值多少元？"}
	r := agent.IntentReport{Preds: []agent.IntentPrediction{p}, Metrics: agent.ComputeIntentMetrics([]agent.IntentPrediction{p}, nil)}
	path := writeIntentReport(r, t.TempDir())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	md := string(raw)
	if !strings.Contains(md, "必要澄清不属于业务错误") || !strings.Contains(md, "首工具未命中明细") || strings.Contains(md, "## 错分明细") {
		t.Fatal("report still equates a missing first tool with a business failure")
	}
	if r.Metrics.Correct != 0 || r.Metrics.ConfirmCovered != 0 {
		t.Fatal("clarification must not manufacture a tool call or confirmation")
	}
}

func TestIntentReportMarksNoSuccessfulRequestsAsUnavailable(t *testing.T) {
	p := agent.IntentPrediction{Sample: agent.IntentSample{ID: "auth-failed", Text: "查余额", Intent: "get_balance"}, HTTPCode: 500, Err: "HTTP 500"}
	r := agent.IntentReport{Preds: []agent.IntentPrediction{p}, Metrics: agent.ComputeIntentMetrics([]agent.IntentPrediction{p}, nil)}
	raw, err := os.ReadFile(writeIntentReport(r, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "无成功正常请求，工具路径准确率和成功请求延迟不可评估") {
		t.Fatal("zero successful requests must not be interpreted as zero model capability")
	}
}

func TestComparisonKeepsClarificationAndEmptySamplesSeparateFromBusinessQuality(t *testing.T) {
	dir := t.TempDir()
	paths := []string{dir + "/off.json", dir + "/on.json"}
	for i, mode := range []string{"off", "on"} {
		p := agent.IntentPrediction{Sample: agent.IntentSample{ID: "missing-amount", Text: "帮我充值", Intent: "recharge"}, HTTPCode: 500, Err: "HTTP 500"}
		r := agent.IntentReport{SchemaVersion: 2, JevMode: mode, Model: "same-model", DatasetSHA256: "same-data", Workers: 4, Preds: []agent.IntentPrediction{p}}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[i], raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runComparison(paths[0], paths[1], dir)
	raw, err := os.ReadFile(dir + "/comparison.md")
	if err != nil {
		t.Fatal(err)
	}
	md := string(raw)
	if !strings.Contains(md, "必要澄清不属于业务错误") || !strings.Contains(md, "不可评估") || !strings.Contains(md, "全请求首工具命中率") || strings.Contains(md, "端到端准确率（含失败）") {
		t.Fatal("comparison misrepresents business correctness or empty samples")
	}
}
