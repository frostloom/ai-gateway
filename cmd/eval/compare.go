package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func validateComparison(b, e agent.IntentReport) error {
	if b.SchemaVersion != 2 || e.SchemaVersion != 2 {
		return fmt.Errorf("comparison requires scoring schema 2")
	}
	if b.JevMode != "off" || e.JevMode != "on" {
		return fmt.Errorf("expected baseline JEV off and experiment JEV on")
	}
	if b.DatasetSHA256 == "" || b.DatasetSHA256 != e.DatasetSHA256 || b.Model == "" || b.Model != e.Model || b.Workers != e.Workers {
		return fmt.Errorf("dataset, model or concurrency mismatch")
	}
	if len(b.Preds) != len(e.Preds) || len(b.AdvPreds) != len(e.AdvPreds) {
		return fmt.Errorf("sample count mismatch")
	}
	for i, run := range []agent.IntentReport{b, e} {
		for _, p := range append(append([]agent.IntentPrediction{}, run.Preds...), run.AdvPreds...) {
			if p.HTTPCode == 200 && p.Err == "" && (p.Telemetry == nil || p.Telemetry.JevEnabled != (i == 1)) {
				return fmt.Errorf("missing or mismatched JEV telemetry for %s", p.Sample.ID)
			}
		}
	}
	for i, p := range b.Preds {
		if !reflect.DeepEqual(p.Sample, e.Preds[i].Sample) {
			return fmt.Errorf("unpaired sample at %d", i)
		}
	}
	for i, p := range b.AdvPreds {
		if !reflect.DeepEqual(p.Sample, e.AdvPreds[i].Sample) {
			return fmt.Errorf("unpaired adversarial sample at %d", i)
		}
	}
	return nil
}

func runComparison(baseline, experiment, out string) {
	var b, e agent.IntentReport
	for path, dst := range map[string]*agent.IntentReport{baseline: &b, experiment: &e} {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err = json.Unmarshal(raw, dst); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := validateComparison(b, e); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Recompute from saved predictions; do not trust rounded Markdown or cached metrics.
	bm, em := agent.ComputeIntentMetrics(b.Preds, b.AdvPreds), agent.ComputeIntentMetrics(e.Preds, e.AdvPreds)
	var text strings.Builder
	text.WriteString("# 客服意图路由：JEV 开关对照\n\n")
	fmt.Fprintf(&text, "- 模型：`%s`；并发：%d；数据集：`%s`\n- 基线：`%s`\n- 实验：`%s`\n- 同一数据与评分版本；两组分开运行，延迟仍可能受上游负载影响。\n\n", b.Model, b.Workers, b.DatasetSHA256, baseline, experiment)
	text.WriteString("| 指标 | 无 JEV | 有 JEV | 差值（有−无） |\n|---|---:|---:|---:|\n")
	rows := []struct {
		name    string
		b, e    float64
		percent bool
	}{
		{"请求成功率", bm.RequestSuccessRate, em.RequestSuccessRate, true},
		{"Top-1 准确率", bm.Top1, em.Top1, true},
		{"端到端准确率（含失败）", bm.EndToEndAccuracy, em.EndToEndAccuracy, true},
		{"目标可达率", bm.ReachRate, em.ReachRate, true},
		{"Macro-F1", bm.MacroF1, em.MacroF1, false},
		{"Macro-Precision", bm.MacroP, em.MacroP, false},
		{"Macro-Recall", bm.MacroR, em.MacroR, false},
		{"OOS 准确率", bm.OOSAccuracy, em.OOSAccuracy, true},
		{"OOS 误调用率", bm.FalseCallRate, em.FalseCallRate, true},
		{"正常请求误拦率", bm.FalseBlockRate, em.FalseBlockRate, true},
		{"对抗零工具阻断率", bm.BlockRate, em.BlockRate, true},
		{"目标写操作确认覆盖率", bm.ConfirmRate, em.ConfirmRate, true},
		{"平均延迟 ms", bm.AvgLatencyMs, em.AvgLatencyMs, false},
		{"P50 延迟 ms", bm.P50LatencyMs, em.P50LatencyMs, false},
		{"P95 延迟 ms", bm.P95LatencyMs, em.P95LatencyMs, false},
		{"平均工具次数", bm.AvgToolCalls, em.AvgToolCalls, false},
		{"实测平均 LLM 请求次数", bm.AvgLLMCalls, em.AvgLLMCalls, false},
		{"正常样本 JEV 短路数", float64(bm.JevShortCircuit), float64(em.JevShortCircuit), false},
		{"对抗 JEV 拦截数", float64(bm.AdversarialJevBlocked), float64(em.AdversarialJevBlocked), false},
		{"正常样本 JEV 请求失败数", float64(bm.JevErrors), float64(em.JevErrors), false},
		{"对抗 JEV 请求失败数", float64(bm.AdversarialJevErrors), float64(em.AdversarialJevErrors), false},
		{"对抗请求失败数", float64(bm.AdversarialFailed), float64(em.AdversarialFailed), false},
	}
	for _, r := range rows {
		if r.percent {
			fmt.Fprintf(&text, "| %s | %.2f%% | %.2f%% | %+.2f pp |\n", r.name, r.b*100, r.e*100, (r.e-r.b)*100)
		} else {
			fmt.Fprintf(&text, "| %s | %.3f | %.3f | %+.3f |\n", r.name, r.b, r.e, r.e-r.b)
		}
	}
	paired, better, worse := 0, 0, 0
	text.WriteString("\n## 配对错分变化\n\n| 样本 | 无 JEV 首工具 | 有 JEV 首工具 | 变化 |\n|---|---|---|---|\n")
	for i, p := range b.Preds {
		q := e.Preds[i]
		if p.HTTPCode != 200 || p.Err != "" || q.HTTPCode != 200 || q.Err != "" {
			continue
		}
		paired++
		bp, ep := agent.IntentMatched(p), agent.IntentMatched(q)
		if bp == ep {
			continue
		}
		verdict := "改善"
		if ep {
			better++
		} else {
			worse++
			verdict = "变差"
		}
		fmt.Fprintf(&text, "| %s | %s | %s | %s |\n", p.Sample.ID, p.Predicted, q.Predicted, verdict)
	}
	fmt.Fprintf(&text, "\n共同成功样本 %d；错→对 %d；对→错 %d。\n", paired, better, worse)
	text.WriteString("\n## 分类与表达维度\n\n| 分组 | 名称 | 无 JEV Top-1 | 有 JEV Top-1 | 无 JEV Reach | 有 JEV Reach |\n|---|---|---:|---:|---:|---:|\n")
	for _, group := range []struct {
		name string
		b, e []agent.ClassMetric
	}{{"意图", bm.PerClass, em.PerClass}, {"维度", bm.PerDimension, em.PerDimension}} {
		for _, bc := range group.b {
			for _, ec := range group.e {
				if bc.Name == ec.Name {
					fmt.Fprintf(&text, "| %s | %s | %d/%d | %d/%d | %.2f%% | %.2f%% |\n", group.name, bc.Name, bc.Correct, bc.Total, ec.Correct, ec.Total, bc.ReachRate*100, ec.ReachRate*100)
				}
			}
		}
	}
	text.WriteString("\n## 解释边界\n\n澄清只作启发式独立统计，不计入严格 Top-1 或 Reach。对抗零工具口径衡量路由阻断，不等价于资金安全；确认与租户隔离由功能回归测试另行验证。JSON 包含逐条预测、完整工具链与混淆矩阵。没有采集 token 与价格，因此不推算 token 节省或费用收益。\n")
	if bm.Failed+em.Failed+bm.AdversarialFailed+em.AdversarialFailed > 0 {
		text.WriteString("\n**本次存在请求失败，不能作为完整通过的能力评测；请先排查上游可用性。**\n")
	}
	if em.JevErrors+em.AdversarialJevErrors > 0 {
		text.WriteString("\n**JEV 存在调用失败并降级，开启组不是每条都获得有效判断。**\n")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		panic(err)
	}
	path := filepath.Join(out, "comparison.md")
	if err := os.WriteFile(path, []byte(text.String()), 0644); err != nil {
		panic(err)
	}
	fmt.Println("对比报告:", path)
}
