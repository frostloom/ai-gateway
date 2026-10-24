// knowledge-live defaults to a credential-free policy/input check. This build
// supports only explicit loopback fixture execution; paid claims always refuse.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
func run(args []string, out io.Writer) int {
	f := flag.NewFlagSet("knowledge-live", flag.ContinueOnError)
	f.SetOutput(out)
	root := f.String("root", ".", "explicit knowledge source root")
	scope := f.String("scope", "", "public knowledge scope; defaults inside root")
	billing := f.String("billing-evidence", "", "explicit billing evidence")
	source := f.String("billing-source", "", "explicit tariff source")
	execute := f.Bool("execute", false, "paid execution (unavailable until shared budget coordination)")
	f.String("revision", "", "reviewed execution revision")
	f.String("release", "", "evaluation release")
	f.String("env-file", "", "explicit private env; never loaded by this build")
	dir := f.String("out", "", "new private local output directory")
	local := f.String("local-stub", "", "explicit literal HTTP loopback provider only")
	dsn := f.String("fixture-admin-dsn", "", "explicit local MySQL admin DSN without database")
	ids := f.String("local-cases", "", "optional comma-separated local-only case IDs")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return 2
	}
	fail := func(msg string) int { fmt.Fprintln(out, msg); return 2 }
	if *scope == "" {
		*scope = filepath.Join(*root, "eval/knowledge/control-v1/scope.example.json")
	}
	raw, e := knowledgepilot.ReadBounded(*scope)
	if e != nil {
		return fail("scope unavailable")
	}
	var b, s []byte
	if *billing != "" {
		b, e = knowledgepilot.ReadBounded(*billing)
		if e != nil {
			return fail("billing evidence unavailable")
		}
	}
	if *source != "" {
		s, e = knowledgepilot.ReadBounded(*source)
		if e != nil {
			return fail("billing source unavailable")
		}
	}
	checked, e := knowledgepilot.CheckScope(raw, b, s)
	if e != nil {
		return fail("scope/billing contract rejected")
	}
	inputs, e := knowledgepilot.LoadInputs(*root, checked.Scope)
	if e != nil {
		return fail("frozen inputs or runtime identities rejected")
	}
	if *execute {
		if *local != "" {
			return fail("paid execute cannot select local mode")
		}
		return fail(knowledgepilot.BudgetBlocker + ": paid execution disabled before credentials, claims or fixtures")
	}
	if *local == "" {
		if *ids != "" || *dsn != "" {
			return fail("local-stub required for fixture options")
		}
		if json.NewEncoder(out).Encode(checked) != nil {
			return 1
		}
		return 0
	}
	if *dir == "" || *dsn == "" {
		return fail("local fixture execution requires new out and explicit administration DSN")
	}
	// Local output parent must exist. Claim and journal cannot be reopened to resume.
	claim, e := knowledgepilot.OpenLocalClaim(*dir+".local-claim", *local, checked.Scope)
	if e != nil {
		return fail("literal loopback and fresh local claim required")
	}
	journal, e := knowledgepilot.OpenJournal(*dir + ".attempts.jsonl")
	if e != nil {
		return fail("fresh local journal required")
	}
	defer journal.Close()
	tr := knowledgepilot.NewTransport(http.DefaultTransport, claim, checked.Scope.Limits, journal)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var selected []string
	if *ids != "" {
		selected = strings.Split(*ids, ",")
	}
	receipt, e := knowledgepilot.Run(ctx, knowledgepilot.RunOptions{AllowExecution: true, Root: *root, Inputs: inputs, Claim: claim, Transport: tr, AdminDSN: *dsn, Out: *dir, CaseIDs: selected})
	code := 0
	if e != nil {
		code = 1
	}
	json.NewEncoder(out).Encode(map[string]any{"mode": "local_stub", "status": receipt.Status, "tasks": len(receipt.Recording.Tasks), "cleanup": receipt.Cleanup, "effect_claims_allowed": false, "ready_for_paid_run": false, "jev_calls": 0})
	return code
}
