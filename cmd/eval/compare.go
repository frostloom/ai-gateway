package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func validateComparison(b, e agent.IntentReport) error {
	if b.SchemaVersion != e.SchemaVersion || (b.SchemaVersion != 2 && b.SchemaVersion != 3) {
		return fmt.Errorf("comparison requires matching scoring schema 2 or 3")
	}
	if b.SchemaVersion == 3 {
		if len(b.ConfigurationErrors) > 0 || len(e.ConfigurationErrors) > 0 {
			return fmt.Errorf("runtime evidence missing or drifted")
		}
		if b.EvaluationContractSHA256 == "" || b.EvaluationContractSHA256 != e.EvaluationContractSHA256 || b.FixtureDefinitionSHA256 == "" || b.FixtureDefinitionSHA256 != e.FixtureDefinitionSHA256 {
			return fmt.Errorf("evaluation contract or fixture definition mismatch")
		}
		var br, er map[string]json.RawMessage
		if json.Unmarshal(b.Runtime, &br) != nil || json.Unmarshal(e.Runtime, &er) != nil {
			return fmt.Errorf("missing observed runtime configuration")
		}
		for _, key := range []string{"model", "protocol", "endpoint_sha256", "prompt_sha256", "tools_sha256", "contract_sha256", "max_output_tokens", "reply_repair_limit"} {
			if len(br[key]) == 0 || string(br[key]) == `null` || string(br[key]) == `""` || !reflect.DeepEqual(br[key], er[key]) {
				return fmt.Errorf("missing or mismatched runtime %s", key)
			}
		}
		for _, key := range []string{"endpoint_sha256", "prompt_sha256", "tools_sha256", "contract_sha256"} {
			var digest string
			json.Unmarshal(br[key], &digest)
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("runtime %s is not a SHA256 digest", key)
			}
		}
		for key, digest := range map[string]string{"evaluation contract": b.EvaluationContractSHA256, "fixture definition": b.FixtureDefinitionSHA256, "dataset": b.DatasetSHA256} {
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("%s is not a SHA256 digest", key)
			}
		}
		var protocol string
		json.Unmarshal(br["protocol"], &protocol)
		if protocol != "openai" && protocol != "anthropic" {
			return fmt.Errorf("unknown or unsupported observed protocol")
		}
		var budget, repairLimit int
		json.Unmarshal(br["max_output_tokens"], &budget)
		json.Unmarshal(br["reply_repair_limit"], &repairLimit)
		if budget <= 0 || repairLimit < 0 || repairLimit > 1 {
			return fmt.Errorf("invalid frozen token or repair budget")
		}
		var bm, em, model string
		json.Unmarshal(br["routing_mode"], &bm)
		json.Unmarshal(er["routing_mode"], &em)
		json.Unmarshal(br["model"], &model)
		if bm != "model" || em != "jev_selective" || model != b.Model || strings.TrimSpace(model) == "" || strings.EqualFold(model, "unknown") {
			return fmt.Errorf("main comparison requires observed model and model/jev_selective routing modes")
		}
		var bt, et map[string]float64
		json.Unmarshal(br["thresholds"], &bt)
		json.Unmarshal(er["thresholds"], &et)
		if len(bt) == 0 || !reflect.DeepEqual(bt, et) || et["intent_min"] < .90 {
			return fmt.Errorf("missing, changed or invalid frozen thresholds")
		}
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
	if b.SchemaVersion == 2 {
		text.WriteString("历史schema2仅作工具路径诊断；缺少共享配置证据，不能作为主实验归因。\n\n")
	}
	text.WriteString("必要澄清不属于业务错误。首工具命中与全请求首工具命中率仅衡量工具路径；不代表业务任务完成率。原始确认覆盖率含缺参数请求，行为复核应分开解释。\n\n")
	fmt.Fprintf(&text, "成功正常请求：无 JEV %d/%d，有 JEV %d/%d。\n\n", bm.Evaluated, bm.Total, em.Evaluated, em.Total)
	if bm.Evaluated == 0 || em.Evaluated == 0 {
		text.WriteString("**存在无成功正常请求的组：该组工具路径准确率和成功请求延迟不可评估；零字段是兼容默认值，不能解释为模型能力为零或版本退步。**\n\n")
	}
	text.WriteString("| 指标 | 无 JEV | 有 JEV | 差值（有−无） |\n|---|---:|---:|---:|\n")
	rows := []struct {
		name    string
		b, e    float64
		percent bool
	}{
		{"请求成功率", bm.RequestSuccessRate, em.RequestSuccessRate, true},
		{"首工具命中率 Top-1", bm.Top1, em.Top1, true},
		{"全请求首工具命中率（含失败）", bm.EndToEndAccuracy, em.EndToEndAccuracy, true},
		{"目标可达率", bm.ReachRate, em.ReachRate, true},
		{"Macro-F1", bm.MacroF1, em.MacroF1, false},
		{"Macro-Precision", bm.MacroP, em.MacroP, false},
		{"Macro-Recall", bm.MacroR, em.MacroR, false},
		{"OOS 准确率", bm.OOSAccuracy, em.OOSAccuracy, true},
		{"OOS 误调用率", bm.FalseCallRate, em.FalseCallRate, true},
		{"正常请求误拦率", bm.FalseBlockRate, em.FalseBlockRate, true},
		{"对抗零工具阻断率", bm.BlockRate, em.BlockRate, true},
		{"目标写操作确认覆盖率", bm.ConfirmRate, em.ConfirmRate, true},
		{"各组成功请求平均延迟 ms（成功ID可能不同）", bm.AvgLatencyMs, em.AvgLatencyMs, false},
		{"各组成功请求P50 ms（非配对子集收益）", bm.P50LatencyMs, em.P50LatencyMs, false},
		{"各组成功请求P95 ms（非配对子集收益）", bm.P95LatencyMs, em.P95LatencyMs, false},
		{"全部请求累计客户端毫秒（含失败及对抗）", bm.AllRequestLatencyMS, em.AllRequestLatencyMS, false},
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
	commonSuccess := 0
	var excludedIDs []string
	text.WriteString("\n## 配对首工具命中变化\n\n必要澄清不属于业务错误。此处仅比较严格工具路径命中，不衡量最终业务成功；参数不足时追问是正确处理，行为复核需独立查看。\n\n| 样本 | 无 JEV 首工具 | 有 JEV 首工具 | 变化 |\n|---|---|---|---|\n")
	for i, p := range b.Preds {
		q := e.Preds[i]

		paired++
		if p.HTTPCode == 200 && p.Err == "" && q.HTTPCode == 200 && q.Err == "" {
			commonSuccess++
		} else {
			excludedIDs = append(excludedIDs, p.Sample.ID)
		}
		bp, ep := agent.IntentMatched(p), agent.IntentMatched(q)
		if bp == ep {
			continue
		}
		verdict := "首工具未命中→命中"
		if ep {
			better++
		} else {
			worse++
			verdict = "首工具命中→未命中"
		}
		fmt.Fprintf(&text, "| %s | %s | %s | %s |\n", p.Sample.ID, p.Predicted, q.Predicted, verdict)
	}
	fmt.Fprintf(&text, "\n完整共同配对样本（含请求失败） %d；首工具未命中→命中 %d；首工具命中→未命中 %d。\n", paired, better, worse)
	fmt.Fprintf(&text, "\n共同成功诊断子集 %d；其余ID（仍在完整配对分母中）：%s。两组已观测用量状态：%s/%s；费用均不可评估，缺usage不能按0推算节省。\n", commonSuccess, strings.Join(excludedIDs, ", "), bm.UsageStatus, em.UsageStatus)
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
	text.WriteString("\n## 解释边界\n\n澄清只作启发式独立统计，不计入严格 Top-1 或 Reach。对抗零工具口径衡量路由阻断，不等价于资金安全；确认与租户隔离由功能回归测试另行验证。JSON 包含逐条预测、完整工具链与混淆矩阵。JSON保留实际返回用量、缺失用量与失败调用；没有适用单价，费用不可评估，未知成本不能当作零。\n")
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
