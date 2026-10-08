package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func renderReport(report taskbench.Report) string {
	var b strings.Builder
	m := report.Metrics
	fmt.Fprintf(&b, "评分规则：`%s`。\n\n", report.Schema)
	fmt.Fprintf(&b, "# 客服多轮任务评测\n\n这是固定任务回归集，不是对所有用户说法的泛化准确率估计。真实模型与真实本地模拟账务服务参与；每个任务使用独立租户。成功必须通过参数、确认流程和独立最终状态核验，必要追问属于正常流程。\n\n版本：%s；JEV：%s；模型：%s；源码：%s。\n\n数据集 SHA256：`%s`\n\n", report.Metadata.Version, report.Metadata.JevMode, report.Metadata.Model, report.Metadata.SourceGit, report.DatasetSHA256)
	fmt.Fprintf(&b, "| 指标 | 结果 |\n|---|---:|\n| 任务完成率（全部尝试，含失败） | %d/%d · %.2f%% |\n| fixture失败 | %d |\n| HTTP失败 | %d |\n| 必要追问提供数/应追问任务数 | %d/%d |\n| 参数充分时冗余追问候选 | %d |\n| 参数正确预览/观测到的预览 | %d/%d |\n| 确认前写入违规任务 | %d |\n| 取消无写入/取消任务 | %d/%d |\n| 实测LLM请求总数/有遥测轮数 | %d/%d |\n\n", m.Completed, m.Total, m.CompletionRate*100, m.FixtureFailures, m.FailedHTTP, m.ClarificationOffered, m.ClarificationRequired, m.RedundantClarifications, m.PreviewCorrect, m.PreviewObserved, m.PrematureWrites, m.CancelNoWrite, m.CancelCases, m.LLMCalls, m.TelemetryTurns)
	fmt.Fprintf(&b, "追问后任务完成：%d/%d；重复确认任务通过：%d/%d。提供追问与完成缺槽任务分别统计；重复确认需要核验没有第二次账务变更。\n\n", m.ClarificationCompleted, m.ClarificationRequired, m.DuplicateConfirmPassed, m.DuplicateConfirmCases)
	fmt.Fprintf(&b, "取消后的账务状态未变观测：%d/%d；成功取消且无写入（同时要求清除挂起）：%d/%d。状态未变不能替代取消成功，仍挂起的操作以后可能被确认。\n\n", m.CancelNoWriteObserved, m.CancelCases, m.CancelNoWrite, m.CancelCases)
	b.WriteString("未观测到预览时，参数正确率不可评估；不能把未观测误称为100%或0%的参数能力。追问文本判定仍有语言规则限制，须结合原始轮次复核。\n\n")
	b.WriteString("任务耗时从首个请求到最终状态核验完成，排除fixture准备与模拟用户思考时间；包括模型、工具和核验开销。失败耗时单独保留，不混入成功任务的完成耗时。\n\n| 耗时口径 | 数量 | 平均毫秒 | P50毫秒 | P95毫秒 |\n|---|---:|---:|---:|---:|\n")
	for _, row := range []struct {
		name string
		data taskbench.Latencies
	}{{"全部尝试", m.AllTasks}, {"成功任务", m.SuccessfulTasks}, {"到达预览", m.TimeToPreview}} {
		if row.data.Count == 0 {
			fmt.Fprintf(&b, "| %s | 0 | 不可评估 | 不可评估 | 不可评估 |\n", row.name)
		} else {
			fmt.Fprintf(&b, "| %s | %d | %.0f | %.0f | %.0f |\n", row.name, row.data.Count, row.data.AverageMS, row.data.P50MS, row.data.P95MS)
		}
	}
	b.WriteString("\n| 任务类别 | 完成/尝试 | 完成率 |\n|---|---:|---:|\n")
	var names []string
	for name := range m.ByCategory {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := m.ByCategory[name]
		fmt.Fprintf(&b, "| %s | %d/%d | %.2f%% |\n", name, c.Completed, c.Total, c.CompletionRate*100)
	}
	b.WriteString("\n| 案例 | 结果 | 耗时毫秒 | 失败原因 |\n|---|---|---:|---|\n")
	for _, r := range report.Results {
		status := "未完成"
		if r.Success {
			status = "完成"
		}
		reason := strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(strings.Join(r.Errors, "; "))
		fmt.Fprintf(&b, "| %s | %s | %.0f | %s |\n", r.Case.ID, status, r.DurationMS, reason)
	}
	for _, limitation := range report.Limitations {
		fmt.Fprintf(&b, "\n- %s\n", limitation)
	}
	b.WriteString("\n同名JSON保存每个任务的初始状态、全部轮次、工具参数、状态快照和失败信息；没有剔除未完成任务。\n")
	return b.String()
}
