// task-eval runs state-verified tasks on newly created simulation tenants.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func main() { os.Exit(run()) }

func run() int {
	c := runConfig{}
	flag.StringVar(&c.Base, "base", "http://127.0.0.1:9105", "local agent origin")
	flag.StringVar(&c.Out, "out", "", "new report JSON path; existing paths are rejected")
	flag.StringVar(&c.Version, "version", "", "implementation label")
	flag.StringVar(&c.JevMode, "jev", "", "actual agent JEV mode: off or on")
	flag.StringVar(&c.Model, "model", os.Getenv("AGENT_LLM_MODEL"), "actual agent model")
	flag.StringVar(&c.Source, "source", "", "source commit of the running agent binary")
	flag.StringVar(&c.AgentBinary, "agent-binary", "", "running agent binary to hash")
	flag.StringVar(&c.CasesFile, "cases", "", "frozen task cases JSON; default uses built-in v1 cases")
	flag.BoolVar(&c.AllowWrites, "allow-simulated-writes", false, "allow confirmations in newly created isolated simulation tenants")
	flag.Parse()
	if err := validateRun(c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	bin, err := os.ReadFile(c.AgentBinary)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read agent binary:", err)
		return 2
	}
	sha := sha256.Sum256(bin)
	evaluatorPath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	evaluatorBinary, err := os.ReadFile(evaluatorPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	evaluatorSHA := sha256.Sum256(evaluatorBinary)
	cases, err := loadCases(c.CasesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid task cases:", err)
		return 2
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "task dataset is empty")
		return 2
	}
	seen := map[string]bool{}
	for _, scenario := range cases {
		if scenario.ID == "" || scenario.Message == "" || seen[scenario.ID] {
			fmt.Fprintln(os.Stderr, "cases need unique nonempty IDs and messages")
			return 2
		}
		seen[scenario.ID] = true
	}
	if err := os.MkdirAll(filepath.Dir(c.Out), 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	lock, err := os.OpenFile(c.Out+".running", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot reserve output:", err)
		return 2
	}
	if err := lock.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	// An interrupted process leaves the lock as evidence; normal exits release it.
	defer os.Remove(c.Out + ".running")
	backend, err := taskbench.NewLiveFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "task fixture backend unavailable:", err)
		return 2
	}
	defer backend.Close()
	fmt.Printf("Task suite: %d cases; model=%s; JEV=%s; isolated simulation tenants\n", len(cases), c.Model, c.JevMode)
	report := taskbench.Run(context.Background(), taskbench.Options{
		BaseURL: c.Base, Backend: backend, Cases: cases,
		Metadata:   taskbench.Metadata{Version: c.Version, JevMode: c.JevMode, Model: c.Model, SourceGit: c.Source, BinarySHA256: hex.EncodeToString(sha[:]), EvaluatorSHA256: hex.EncodeToString(evaluatorSHA[:])},
		OnProgress: func(done, total int) { fmt.Printf("Verified task attempts: %d/%d\n", done, total) },
	})
	if err := taskbench.WriteReport(c.Out, report); err != nil {
		fmt.Fprintln(os.Stderr, "cannot preserve report:", err)
		return 2
	}
	f, err := os.OpenFile(c.Out+".md", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	_, writeErr := f.WriteString(renderReport(report))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "cannot write companion report:", writeErr, closeErr)
		return 2
	}
	fmt.Printf("Verified tasks: %d/%d (%.2f%%); report=%s; finished=%s\n", report.Metrics.Completed, report.Metrics.Total, report.Metrics.CompletionRate*100, c.Out, time.Now().Format(time.RFC3339))
	if report.Metrics.Completed != report.Metrics.Total {
		return 1
	}
	return 0
}
