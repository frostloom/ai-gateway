// paired-eval prepares a small development pilot or reviews its local journal.
// It intentionally cannot discover credentials or execute a paid provider.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	mode := flags.String("mode", "prepare", "prepare, preflight, campaign-preflight or report; never live")
	campaignPlan := flags.String("campaign-plan", "", "explicit offline batch/budget JSON for campaign-preflight")
	profiles := flags.String("profiles", "", "pinned runtime profile directory for preflight")
	base := flags.String("model-base", "", "public model base URL for offline preflight")
	model := flags.String("model-name", "", "explicit model name for offline preflight")
	protocol := flags.String("protocol", "", "openai or anthropic for offline preflight")
	revision := flags.String("revision", "", "reviewed execution revision for offline preflight")
	jevModel := flags.String("jev-model", "", "pinned JEV model for offline preflight")
	defs := flags.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definition directory")
	journal := flags.String("journal", "", "local paired run journal for offline report")
	out := flags.String("out", "", "new output JSON path; report also creates .md")
	if flags.Parse(args) != nil {
		return 2
	}
	campaignPreflight := *mode == "campaign-preflight"
	preflight := *mode == "preflight" || campaignPreflight
	publicArgs := *profiles != "" || *base != "" || *model != "" || *protocol != "" || *revision != "" || *jevModel != ""
	if flags.NArg() != 0 || *out == "" || (*mode != "prepare" && *mode != "report" && !preflight) || (*mode == "report" && *journal == "") || (*mode != "report" && *journal != "") || (!preflight && publicArgs) || (preflight && (*profiles == "" || *base == "" || *model == "" || *protocol == "" || *revision == "" || *jevModel == "")) || (campaignPreflight != (*campaignPlan != "")) {
		fmt.Fprintln(os.Stderr, "offline prepare/preflight/campaign-preflight/report and new -out required; no live mode")
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
	if *mode == "prepare" || preflight {
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
		if preflight {
			profiles, err := taskbench.LoadPairedProfiles(*profiles)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			if campaignPreflight {
				plan, err := readCampaignPlan(*campaignPlan)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				value, err = taskbench.PrepareCampaignProposal(s, profiles, taskbench.PairModelSettings{BaseURL: *base, Model: *model, Protocol: *protocol}, *revision, *jevModel, plan)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
			} else {
				cfg, err := taskbench.PrepareAgentPairConfig(p.Suite, profiles, taskbench.PairModelSettings{BaseURL: *base, Model: *model, Protocol: *protocol}, "recorded_run", *revision, *jevModel)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				value = struct {
					Ready         bool                    `json:"ready_for_paid_run"`
					Calls         int                     `json:"provider_calls"`
					Configuration taskbench.PairRunConfig `json:"configuration"`
					Limitations   []string                `json:"limitations"`
				}{false, 0, cfg, []string{"Offline configuration proposal only; no database or provider calls.", "Channel prices, account charges and execution authorization remain unverified."}}
			}
		}
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

func readCampaignPlan(path string) (taskbench.CampaignPreparationPlan, error) {
	var plan taskbench.CampaignPreparationPlan
	f, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&plan); err != nil {
		return plan, fmt.Errorf("invalid campaign plan: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return plan, fmt.Errorf("campaign plan must contain one JSON value")
	}
	return plan, nil
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
