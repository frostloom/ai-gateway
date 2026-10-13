// paired-eval prepares a small development pilot or reviews its local journal.
// It intentionally cannot discover credentials or execute a paid provider.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func main() { os.Exit(run(os.Args[1:])) }
func writeExclusive(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	return f.Sync()
}
func run(args []string) int {
	flags := flag.NewFlagSet("paired-eval", flag.ContinueOnError)
	mode := flags.String("mode", "prepare", "prepare or report; never live")
	defs := flags.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definition directory")
	journal := flags.String("journal", "", "local paired run journal for offline report")
	out := flags.String("out", "", "new output JSON path; report also creates .md")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() != 0 || *out == "" || (*mode != "prepare" && *mode != "report") || (*mode == "report" && *journal == "") || (*mode == "prepare" && *journal != "") {
		fmt.Fprintln(os.Stderr, "prepare/report and new -out required; no live mode")
		return 2
	}
	paths := []string{*out}
	if *mode == "report" {
		paths = append(paths, *out+".md")
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil || !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "output already exists or cannot be checked")
			return 2
		}
	}
	var value any
	var md string
	if *mode == "prepare" {
		s, err := taskbench.LoadConversations(*defs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		p, err := taskbench.PrepareSmallPair(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		value = p
	} else {
		r, err := taskbench.ReportPreparedPair(*journal)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		value = r
		md = pairMarkdown(r)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		err = writeExclusive(*out, append(raw, '\n'))
	}
	if err == nil && md != "" {
		err = writeExclusive(*out+".md", []byte(md))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println("Offline paired preparation/report written; zero model/JEV calls.")
	return 0
}
func pairMarkdown(r taskbench.SmallPairReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 小规模配对报告\n\n证据：%s。计划%d案例/%d臂，已记录%d臂，未完成%d臂。缺审核不发布任务成功率；不发布成本或延迟收益。\n\n", r.EvidenceKind, r.PlannedCases, r.PlannedArms, r.FinishedArms, r.UnrunArms)
	b.WriteString("|案例|版本|重算状态|事实错误|待审要求|模型上游尝试|按配置计价微元|执行器耗时ms|\n|---|---|---|---:|---:|---:|---:|---:|\n")
	for _, row := range r.Rows {
		calls, cost, ms := "unknown", "unknown", "unknown"
		if row.ModelAttempts != nil {
			calls = fmt.Sprint(*row.ModelAttempts)
		}
		if row.PricedModelMicroCNY != nil {
			cost = fmt.Sprint(*row.PricedModelMicroCNY)
		}
		if row.RunnerElapsedMS != nil {
			ms = fmt.Sprintf("%.2f", *row.RunnerElapsedMS)
		}
		key := strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(row.Key)
		fmt.Fprintf(&b, "|%s|%s|%s|%d|%d|%s|%s|%s|\n", key, row.Arm, row.Status, row.FactErrors, len(row.ReviewRequirements), calls, cost, ms)
	}
	b.WriteString("\n必要追问逐轮信息、token及服务器耗时在JSON中。执行器耗时包含初始化、独立状态读取和清理，不是纯模型延迟，也不含用户思考时间。预留金额不是实际费用；JEV收费未知，不算全系统费用。\n\n")
	for _, limit := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", limit)
	}
	return b.String()
}
