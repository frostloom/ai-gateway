// eval AI 客服评测 CLI（P2）。
//
// 两种评测：
//
//	【用例套件】功能正确性（脚本化断言：tenant 隔离、二次确认、审计留痕）
//	  离线（默认，确定性回归、零成本）：go run ./cmd/eval
//	  在线（真 LLM，花真实 token）：      go run ./cmd/eval -mode http -base http://127.0.0.1:18080/portal -key sk-xxx
//
//	【意图套件】路由准确性（分类指标：Top-1 / Macro-F1 / OOS 误调用率）
//	  go run ./cmd/eval -suite intent -mode http -base http://127.0.0.1:18080/portal -key sk-xxx
//	  go run ./cmd/eval -suite intent -mode http ... -sample 5      # 每类抽 5 条，日常回归
//	  go run ./cmd/eval -suite intent ... -jev off -out eval/reports/baseline   # 跑 JEV 前基线
//
// 输出：stdout 摘要 + eval/reports/ 下的 Markdown 报告。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func main() {
	suite := flag.String("suite", "cases", "评测套件: cases（功能用例）| intent（意图路由）")
	mode := flag.String("mode", "fake", "评测模式: fake（离线假 LLM）| http（在线真 agent）")
	dir := flag.String("dir", "", "用例目录（留空则按 suite 选默认）")
	base := flag.String("base", "http://127.0.0.1:9105", "http 模式 agent 基地址（意图套件需带 /portal）")
	key := flag.String("key", "", "http 模式租户 key（缺省用用例自带 key）")
	out := flag.String("out", "eval/reports", "报告输出目录")
	sample := flag.Int("sample", 0, "意图套件每类抽样条数（0 = 全量）")
	workers := flag.Int("workers", 4, "意图套件并发数（过高会让上游排队导致超时，污染指标）")
	jevMode := flag.String("jev", "unknown", "expected server-side JEV mode: on|off|unknown")
	model := flag.String("model", "", "LLM model identifier for reproducibility")
	baseline := flag.String("baseline", "", "baseline JSON for -suite compare")
	experiment := flag.String("experiment", "", "experiment JSON for -suite compare")
	flag.Parse()

	switch *suite {
	case "cases":
		runCases(*mode, *dir, *base, *key, *out)
	case "compare":
		runComparison(*baseline, *experiment, *out)
	case "intent":
		runIntent(*mode, *dir, *base, *key, *out, *sample, *workers, *jevMode, *model)
	default:
		fmt.Fprintf(os.Stderr, "未知套件 %q（cases|intent）\n", *suite)
		os.Exit(1)
	}
}

// ---------- 意图套件 ----------

func runIntent(mode, dir, base, key, out string, perClass, workers int, jevMode, model string) {
	if jevMode != "on" && jevMode != "off" && jevMode != "unknown" {
		fmt.Fprintln(os.Stderr, "-jev must be on, off or unknown")
		os.Exit(1)
	}
	if perClass < 0 || workers < 1 {
		fmt.Fprintln(os.Stderr, "sample must be nonnegative and workers positive")
		os.Exit(1)
	}
	if dir == "" {
		dir = "eval/intent-suite"
	}
	normal, adversarial, err := agent.LoadIntentSuite(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载意图套件失败: %v\n", err)
		os.Exit(1)
	}
	sampled := agent.SampleIntentSuite(normal, perClass)
	classes := map[string]int{}
	for _, s := range sampled {
		classes[s.Intent]++
	}
	fmt.Printf("==> 意图套件：%d 条（%d 类）+ 对抗 %d 条\n", len(sampled), len(classes), len(adversarial))
	if perClass > 0 {
		fmt.Printf("    每类抽样 %d 条\n", perClass)
	}

	if mode != "http" {
		fmt.Fprintln(os.Stderr, "意图套件只支持 -mode http（需要真实 agent 返回路由结果）")
		os.Exit(1)
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "意图套件需要 -key（租户 key）")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	lastPrint := time.Now()
	var progressMu sync.Mutex
	preds, adv, err := agent.RunIntentHTTP(ctx, sampled, base, key, adversarial, workers, func(done, total int) {
		progressMu.Lock()
		defer progressMu.Unlock()
		if time.Since(lastPrint) > 5*time.Second || done == total {
			fmt.Printf("\r    进度 %d/%d ...", done, total)
			lastPrint = time.Now()
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "评测失败: %v\n", err)
		os.Exit(1)
	}

	m := agent.ComputeIntentMetrics(preds, adv)
	rawSamples, _ := json.Marshal(append(append([]agent.IntentSample{}, sampled...), adversarial...))
	r := agent.IntentReport{SchemaVersion: 2, JevMode: jevMode, Model: model, DatasetSHA256: fmt.Sprintf("%x", sha256.Sum256(rawSamples)), Workers: workers, Mode: mode, StartedAt: start, Duration: time.Since(start), Metrics: m, Preds: preds, AdvPreds: adv}
	printIntentSummary(m)
	path := writeIntentReport(r, out)
	fmt.Printf("报告已写入: %s\n", path)

	// 有请求层失败 → 非零退出
	if jevMode != "unknown" {
		for _, p := range append(preds, adv...) {
			if p.HTTPCode != 200 || p.Err != "" {
				continue
			}
			if p.Telemetry == nil || p.Telemetry.JevEnabled != (jevMode == "on") {
				fmt.Fprintln(os.Stderr, "JEV telemetry missing or server mode does not match -jev")
				os.Exit(2)
			}
		}
	}
	if m.Failed > 0 || m.AdversarialFailed > 0 {
		os.Exit(2)
	}
}

func printIntentSummary(m agent.IntentMetrics) {
	fmt.Printf("\n=== 意图路由指标 ===\n")
	fmt.Printf("  样本        %d 条（已评测 %d，请求失败 %d）\n", m.Total, m.Evaluated, m.Failed)
	fmt.Printf("  Top-1 准确率 %.1f%%\n", m.Top1*100)
	fmt.Printf("  目标可达率   %.1f%%  ← 期望工具出现在工具链中（多轮探查也算）\n", m.ReachRate*100)
	fmt.Printf("  仅探查未执行 %d 条  ← 已调用其他工具，目标工具未出现\n", m.PartialPlan)
	fmt.Printf("  Macro-F1    %.3f   (P %.3f / R %.3f)\n", m.MacroF1, m.MacroP, m.MacroR)
	fmt.Printf("  OOS 准确率   %.1f%%  误调用率 %.1f%%\n", m.OOSAccuracy*100, m.FalseCallRate*100)
	fmt.Printf("  对抗拦截率   %.1f%%  误拦率 %.1f%%\n", m.BlockRate*100, m.FalseBlockRate*100)
	if m.ConfirmTotal > 0 {
		fmt.Printf("  确认覆盖率   %.1f%%\n", m.ConfirmRate*100)
	}
	fmt.Printf("  平均延迟     %.0f ms   平均工具调用 %.2f 次\n", m.AvgLatencyMs, m.AvgToolCalls)
	fmt.Printf("  JEV 短路     %d 条（未调 LLM）\n", m.JevShortCircuit)

	fmt.Printf("\n--- 分类明细 ---\n")
	fmt.Printf("  %-20s %10s %10s\n", "意图", "Top-1", "目标可达")
	for _, c := range m.PerClass {
		fmt.Printf("  %-20s %4d/%-4d %4d/%-4d\n", c.Name, c.Correct, c.Total, c.Reach, c.Total)
	}
	if len(m.PerDimension) > 0 {
		fmt.Printf("\n--- 说法维度 ---\n")
		fmt.Printf("  %-14s %10s %10s\n", "维度", "Top-1", "目标可达")
		for _, d := range m.PerDimension {
			fmt.Printf("  %-14s %8.1f%% %8.1f%%\n", d.Name, d.F1*100, d.ReachRate*100)
		}
	}
}

func writeIntentReport(r agent.IntentReport, out string) string {
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建报告目录失败: %v\n", err)
		os.Exit(1)
	}
	m := r.Metrics
	path := filepath.Join(out, "intent-"+time.Now().Format("20060102-150405.000")+".md")
	var b strings.Builder
	b.WriteString("# 意图路由评测报告\n\n")
	fmt.Fprintf(&b, "- 评分版本：2 · JEV：%s · 模型：%s · 并发：%d\n- 数据集 SHA256：`%s`\n", r.JevMode, r.Model, r.Workers, r.DatasetSHA256)
	b.WriteString("- Top-1 严格比较首工具；Reach 严格要求目标工具出现；澄清单列，不给主指标加分。\n- 对抗拦截为零工具且无待确认的保守口径，不能证明越权读写的端到端安全。\n- 延迟为成功请求完整响应时间；真实 LLM 调用次数只来自 telemetry，工具次数单列；未采集 token/费用。\n\n")
	fmt.Fprintf(&b, "- 模式：`%s` · 时间：%s · 耗时：%s\n", r.Mode,
		r.StartedAt.Format("2006-01-02 15:04:05"), r.Duration.Round(time.Second))
	fmt.Fprintf(&b, "- 样本：%d 条（已评测 %d，请求失败 %d）\n\n", m.Total, m.Evaluated, m.Failed)

	b.WriteString("## 核心指标\n\n")
	b.WriteString("| 指标 | 值 |\n|---|---|\n")
	fmt.Fprintf(&b, "| Top-1 准确率 | **%.1f%%** |\n", m.Top1*100)
	fmt.Fprintf(&b, "| 目标可达率（期望工具出现在工具链中） | **%.1f%%** |\n", m.ReachRate*100)
	fmt.Fprintf(&b, "| 仅探查未执行（目标工具未出现） | %d 条 |\n", m.PartialPlan)
	fmt.Fprintf(&b, "| Macro-F1 | %.3f |\n", m.MacroF1)
	fmt.Fprintf(&b, "| Macro-Precision | %.3f |\n", m.MacroP)
	fmt.Fprintf(&b, "| Macro-Recall | %.3f |\n", m.MacroR)
	fmt.Fprintf(&b, "| OOS 准确率（闲聊零工具调用） | %.1f%% |\n", m.OOSAccuracy*100)
	fmt.Fprintf(&b, "| OOS 误调用率 | %.1f%% |\n", m.FalseCallRate*100)
	fmt.Fprintf(&b, "| 对抗拦截率 | %.1f%% |\n", m.BlockRate*100)
	fmt.Fprintf(&b, "| 正常请求误拦率 | %.1f%% |\n", m.FalseBlockRate*100)
	fmt.Fprintf(&b, "| 写操作确认覆盖率 | %.1f%% |\n", m.ConfirmRate*100)
	fmt.Fprintf(&b, "| 平均延迟 | %.0f ms |\n", m.AvgLatencyMs)
	fmt.Fprintf(&b, "| 平均工具调用次数 | %.2f |\n", m.AvgToolCalls)
	fmt.Fprintf(&b, "| JEV 短路（未调 LLM） | %d 条 |\n", m.JevShortCircuit)

	fmt.Fprintf(&b, "| 请求成功率 | %.1f%% |\n| 端到端准确率（含失败） | %.1f%% |\n| P50 延迟 | %.0f ms |\n| P95 延迟 | %.0f ms |\n| 实测平均 LLM 请求次数 | %.2f |\n| Telemetry 样本 | %d |\n| JEV 请求失败数 | %d |\n| 对抗请求失败数 | %d |\n| 澄清启发式命中（独立口径） | %d/%d |\n", m.RequestSuccessRate*100, m.EndToEndAccuracy*100, m.P50LatencyMs, m.P95LatencyMs, m.AvgLLMCalls, m.TelemetrySamples, m.JevErrors, m.AdversarialFailed, m.AskBackHandled, m.AskBackTotal)
	fmt.Fprintf(&b, "| 对抗 JEV 请求失败数 | %d |\n", m.AdversarialJevErrors)
	b.WriteString("\n## 分类明细\n\n| 意图 | Top-1 正确 | 目标可达 | Precision | Recall | F1 |\n|---|---|---|---|---|---|\n")
	for _, c := range m.PerClass {
		fmt.Fprintf(&b, "| `%s` | %d/%d | %d/%d (%.0f%%) | %.3f | %.3f | %.3f |\n",
			c.Name, c.Correct, c.Total, c.Reach, c.Total, c.ReachRate*100, c.Precision, c.Recall, c.F1)
	}

	b.WriteString("\n## 说法维度（难点定位）\n\n| 维度 | Top-1 | 目标可达 |\n|---|---|---|\n")
	for _, d := range m.PerDimension {
		fmt.Fprintf(&b, "| `%s` | %d/%d (%.1f%%) | %d/%d (%.1f%%) |\n",
			d.Name, d.Correct, d.Total, d.F1*100, d.Reach, d.Total, d.ReachRate*100)
	}

	b.WriteString("\n## 请求失败明细\n\n")
	nf := 0
	for _, p := range r.Preds {
		if p.HTTPCode == 200 && p.Err == "" {
			continue
		}
		fmt.Fprintf(&b, "- `%s` HTTP=%d %s｜原话：%s\n", p.Sample.ID, p.HTTPCode, p.Err, p.Sample.Text)
		nf++
	}
	if nf == 0 {
		b.WriteString("（无）\n")
	}

	b.WriteString("\n## 错分明细\n\n")
	b.WriteString("| 样本 | 原话 | 期望 | 实际 | 维度 |\n|---|---|---|---|---|\n")
	n := 0
	for _, p := range r.Preds {
		if p.HTTPCode != 200 || p.Err != "" || intentMatched(p) {
			continue
		}
		got := p.Predicted
		if got == "" {
			got = "（未调用任何工具）"
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | `%s` | %s |\n", p.Sample.ID, p.Sample.Text, p.Sample.Intent, got, p.Sample.Dimension)
		n++
		if n >= 120 {
			b.WriteString("\n（仅列出前 120 条）\n")
			break
		}
	}
	if n == 0 {
		b.WriteString("（无错分）\n")
	}

	// 多工具调用：退款/退订这类操作，模型常先查一次余额/订阅再操作。
	// 首个工具与期望不符，但流程是合理的 —— 单列出来，避免误判为"路由错误"。
	b.WriteString("\n## 先查询后操作（首个工具不符，但流程合理）\n\n")
	b.WriteString("| 样本 | 原话 | 期望 | 首个工具 | 全部工具 |\n|---|---|---|---|---|\n")
	nq := 0
	for _, p := range r.Preds {
		if p.HTTPCode != 200 || p.Err != "" || intentMatched(p) || !agent.IntentReached(p) || len(p.AllTools) < 2 {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | `%s` | %s |\n",
			p.Sample.ID, p.Sample.Text, p.Sample.Intent, p.AllTools[0], strings.Join(p.AllTools, " → "))
		nq++
		if nq >= 60 {
			break
		}
	}
	if nq == 0 {
		b.WriteString("（无）\n")
	}

	b.WriteString("\n## 对抗子集逐条记录\n\n| 样本 | HTTP | JEV 拦截 | 待确认 | 工具链 | 错误 |\n|---|---|---|---|---|---|\n")
	for _, p := range r.AdvPreds {
		fmt.Fprintf(&b, "| %s | %d | %t | %t | %s | %s |\n", p.Sample.ID, p.HTTPCode, p.JevBlocked, p.Pending, strings.Join(p.AllTools, " → "), strings.ReplaceAll(p.Err, "|", "/"))
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(path, ".md")+".json", raw, 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写报告失败: %v\n", err)
		os.Exit(1)
	}
	return path
}

// ---------- 用例套件（原有逻辑） ----------

func runCases(mode, dir, base, key, out string) {
	if dir == "" {
		dir = "eval/cases"
	}
	cases, err := agent.LoadEvalCases(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载用例失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("==> 加载 %d 条用例 (%s)\n", len(cases), mode)

	ctx := context.Background()
	var report agent.EvalReport
	switch mode {
	case "fake":
		report, err = agent.RunFake(ctx, cases)
	case "http":
		report, err = agent.RunHTTP(ctx, cases, base, key)
	default:
		fmt.Fprintf(os.Stderr, "未知模式 %q（fake|http）\n", mode)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "评测失败: %v\n", err)
		os.Exit(1)
	}

	printSummary(report, cases)
	path := writeReport(report, cases, out)
	fmt.Printf("报告已写入: %s\n", path)

	if report.Passed != report.Total {
		os.Exit(2) // 有失败 → 非零退出（CI/脚本可判红）
	}
}

func printSummary(r agent.EvalReport, cases []agent.EvalCase) {
	byDim := map[string][2]int{} // pass, total
	for _, res := range r.Results {
		d := "?"
		for _, c := range cases {
			if c.ID == res.CaseID {
				d = c.Dimension
				break
			}
		}
		byDim[d] = [2]int{byDim[d][0], byDim[d][1] + 1}
		if res.Pass {
			byDim[d] = [2]int{byDim[d][0] + 1, byDim[d][1]}
		}
	}
	fmt.Printf("\n=== 汇总: %d/%d 通过 (%.0f%%) 耗时 %s ===\n",
		r.Passed, r.Total, pct(r.Passed, r.Total), r.Duration.Round(time.Millisecond))
	for _, res := range r.Results {
		mark := "PASS"
		if !res.Pass {
			mark = "FAIL"
		}
		title := ""
		for _, c := range cases {
			if c.ID == res.CaseID {
				title = c.Title
				break
			}
		}
		fmt.Printf("  [%s] %-28s %s\n", mark, res.CaseID, title)
		for _, f := range res.Failures {
			fmt.Printf("        - %s\n", f)
		}
	}
}

func writeReport(r agent.EvalReport, cases []agent.EvalCase, out string) string {
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建报告目录失败: %v\n", err)
		os.Exit(1)
	}
	path := filepath.Join(out, "report-"+time.Now().Format("20060102-150405.000")+".md")
	var b strings.Builder
	b.WriteString("# AI 客服评测报告\n\n")
	fmt.Fprintf(&b, "- 模式：`%s` · 时间：%s · 耗时：%s\n", r.Mode,
		r.StartedAt.Format("2006-01-02 15:04:05"), r.Duration.Round(time.Millisecond))
	fmt.Fprintf(&b, "- 结果：**%d / %d 通过（%.0f%%）**\n\n", r.Passed, r.Total, pct(r.Passed, r.Total))
	b.WriteString("## 逐条明细\n\n")
	b.WriteString("| 状态 | 用例 | 维度 | 说明 | 失败原因 |\n|---|---|---|---|---|\n")
	for _, res := range r.Results {
		var c agent.EvalCase
		for _, x := range cases {
			if x.ID == res.CaseID {
				c = x
				break
			}
		}
		status := "✅ PASS"
		reason := ""
		if !res.Pass {
			status = "❌ FAIL"
			reason = strings.Join(res.Failures, "; ")
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s |\n", status, res.CaseID, c.Dimension, c.Title, reason)
	}
	if len(r.Results) < 40 {
		b.WriteString("\n## 回复快照（失败用例）\n\n")
		for _, res := range r.Results {
			if !res.Pass && res.Detail != "" {
				fmt.Fprintf(&b, "- `%s`：%s\n", res.CaseID, res.Detail)
			}
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写报告失败: %v\n", err)
		os.Exit(1)
	}
	return path
}

func pct(p, t int) float64 {
	if t == 0 {
		return 0
	}
	return float64(p) / float64(t) * 100
}

// intentMatched 判定一条预测是否命中期望。
//
// 关键：闲聊(chitchat)的"命中"表现为**未调用任何工具**（Predicted 为空串），
// 不能直接拿 "" 和 "chitchat" 比 —— 否则报告里会把所有正确的闲聊都列进错分表。
func intentMatched(p agent.IntentPrediction) bool { return agent.IntentMatched(p) }
