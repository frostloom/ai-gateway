// full-customer-policy-audit only replays supplied complete local journals.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	f := flag.NewFlagSet("full-customer-policy-audit", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	refsPath := f.String("journals", "", "explicit JSON array of journal path/SHA references")
	out := f.String("out", "", "new read-only audit result path")
	definitions := f.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definitions")
	profiles := f.String("profiles", "eval/runtime-profiles/2026-10-13-six-profiles-v1", "frozen profiles")
	if f.Parse(args) != nil || f.NArg() != 0 || *refsPath == "" || *out == "" {
		return 2
	}
	if _, err := os.Lstat(*out); !os.IsNotExist(err) {
		return 2
	}
	raw, err := os.ReadFile(*refsPath)
	if err != nil || len(raw) > 1<<20 {
		return 2
	}
	var refs []taskbench.CollectionJournalReference
	if json.Unmarshal(raw, &refs) != nil {
		return 2
	}
	report, err := taskbench.AuditFullCollectionPolicy(*definitions, *profiles, refs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "local policy audit rejected evidence")
		return 2
	}
	raw, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		return 2
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 2
	}
	_, writeErr := file.Write(append(raw, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return 1
	}
	fmt.Printf("Read-only local policy replay: %d pairs, %d arms, %d observed turns; continue %d, halt %d; actual evaluation remains incomplete.\n", report.SuppliedPairs, report.RecordedArms, report.ObservedTurns, report.ContinueArms, report.HaltArms)
	return 0
}
