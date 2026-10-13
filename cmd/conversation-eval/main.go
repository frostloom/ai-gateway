// conversation-eval reviews recorded development conversations offline. There
// is intentionally no live mode, model endpoint, .env or credential handling.
package main

import (
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"os"
)

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	flags := flag.NewFlagSet("conversation-eval", flag.ContinueOnError)
	definitions := flags.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen development definition directory")
	transcript := flags.String("transcript", "", "local conversation-replay.v1 JSON")
	out := flags.String("out", "", "new output path (never overwrites)")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() != 0 || *transcript == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "-transcript and -out required; offline local files only")
		return 2
	}
	suite, err := taskbench.LoadConversations(*definitions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	input, err := taskbench.LoadConversationReplay(*transcript)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	report, err := taskbench.ReplayConversations(suite, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err = taskbench.WriteConversationReplayReport(*out, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Offline %s: full=%d, diagnostic=%d, structural_pass=%d, pending_review=%d, completed=%d, failed=%d, setup_failed=%d, diagnostic_failed=%d\n", report.EvidenceKind, report.Metrics.FullTasks, report.Metrics.FirstTurnDiagnostics, report.Metrics.StructuralPassed, report.Metrics.PendingReview, report.Metrics.Completed, report.Metrics.Failed, report.Metrics.SetupFailures, report.Metrics.DiagnosticFailures+report.Metrics.DiagnosticSetupFailures)
	if report.Metrics.Failed > 0 || report.Metrics.SetupFailures > 0 || report.Metrics.DiagnosticFailures > 0 || report.Metrics.DiagnosticSetupFailures > 0 {
		return 1
	}
	return 0
}
