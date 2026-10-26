// paired-live checks the approved pilot or executes one explicitly selected batch.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/frostloom/ai-gateway/internal/taskbench"
)

type envFiles []string

func (e *envFiles) String() string     { return "explicit env files" }
func (e *envFiles) Set(v string) error { *e = append(*e, v); return nil }
func main()                            { os.Exit(runWithOutput(os.Args[1:], os.Stdout)) }

func boundedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("explicit input unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("input exceeds bounded file size")
	}
	return raw, nil
}
func runWithOutput(args []string, out io.Writer) int {
	f := flag.NewFlagSet("paired-live", flag.ContinueOnError)
	f.SetOutput(out)
	auth := f.String("authorization", "eval/paired-pilots/2026-10-22-authorized-small-pilot/authorization.json", "explicit approved scope JSON")
	plan := f.String("plan", "eval/paired-pilots/2026-10-22-campaign-preflight/plan.json", "frozen campaign plan")
	billing := f.String("billing-evidence", "", "operator billing attestation JSON; never inferred from permission")
	source := f.String("billing-source", "", "local evidence file matching attestation hash")
	defs := f.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definitions")
	profiles := f.String("profiles", "eval/runtime-profiles/2026-10-13-six-profiles-v1", "frozen profiles")
	revision := f.String("revision", "", "reviewed 40-character execution revision")
	dir := f.String("out", "", "new private output directory; never reused")
	release := f.String("release", "", "explicit evaluation release")
	previous := f.String("continue-from", "", "original successful batch-001 output; selects batch-002 only")
	review := f.String("review", "", "bound passing first-batch independent review JSON")
	permission := f.String("continuation-authorization", "", "explicit batch-002 permission; required only for execution")
	retest := f.Bool("retest", false, "separate fixed one-case evaluation; requires independent scope and billing")
	repeated := f.Bool("repeated", false, "fixed four-block repeated cohort; separate exact authorization")
	repeatedDiagnostic := f.Bool("repeated-diagnostic", false, "independent repeated cohort with export diagnostic bindings")
	cohort := f.Bool("cohort", false, "fixed current-v5 six-case cohort; separate exact authorization")
	execute := f.Bool("execute", false, "explicitly run batch-001 only when all evidence is ready")
	var files envFiles
	f.Var(&files, "env-file", "explicit private env file (repeat); never loaded by check")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return 2
	}
	fail := func(msg string) int { fmt.Fprintln(out, msg); return 2 }
	if *repeatedDiagnostic && (*repeated || *cohort || *retest || *previous != "" || *review != "" || *permission != "") {
		return fail("repeated diagnostic cannot use other modes or continuation")
	}
	if *repeated && (*cohort || *retest || *previous != "" || *review != "" || *permission != "") {
		return fail("repeated cannot use other modes or continuation")
	}
	if *cohort && (*retest || *previous != "" || *review != "" || *permission != "") {
		return fail("cohort cannot use retest or continuation inputs")
	}
	if *retest && (*previous != "" || *review != "" || *permission != "") {
		return fail("retest cannot use continuation inputs")
	}
	a, err := boundedFile(*auth)
	if err != nil {
		return fail("authorization input unavailable")
	}
	p, err := boundedFile(*plan)
	if err != nil {
		return fail("plan input unavailable")
	}
	var b, e []byte
	if *billing != "" {
		b, err = boundedFile(*billing)
		if err != nil {
			return fail("billing attestation input unavailable")
		}
	}
	if *source != "" {
		e, err = boundedFile(*source)
		if err != nil {
			return fail("billing source input unavailable")
		}
	}
	var pilot taskbench.AuthorizedPilot
	if *repeatedDiagnostic {
		pilot, err = taskbench.CheckAuthorizedRepeatedDiagnostic(a, p, b, e)
	} else if *repeated {
		pilot, err = taskbench.CheckAuthorizedRepeated(a, p, b, e)
	} else if *cohort {
		pilot, err = taskbench.CheckAuthorizedCohort(a, p, b, e)
	} else if *retest {
		pilot, err = taskbench.CheckAuthorizedRetest(a, p, b, e)
	} else {
		pilot, err = taskbench.CheckAuthorizedPilot(a, p, b, e)
	}
	if err != nil {
		return fail("authorization or billing evidence binding rejected")
	}
	var continuation *taskbench.AuthorizedContinuation
	if *previous != "" {
		raw, err := boundedFile(*review)
		if err != nil {
			return fail("explicit independent review required")
		}
		checked, err := taskbench.CheckAuthorizedContinuation(pilot, *previous, raw)
		if err != nil {
			return fail("continuation checkpoint or independent review rejected")
		}
		continuation = &checked
	} else if *review != "" || *permission != "" {
		return fail("continue-from required for continuation inputs")
	}
	if !*execute {
		if continuation != nil {
			return writeContinuationCheck(out, pilot, *continuation)
		}
		return writeCheck(out, pilot)
	}
	if continuation != nil {
		consent, err := boundedFile(*permission)
		sum := sha256.Sum256(p)
		if err != nil || validateContinuationPermission(consent, hex.EncodeToString(sum[:]), continuation.ReceiptSHA(), continuation.ReviewSHA()) != nil {
			return fail("explicit bound batch-002 permission required before credentials or fixtures")
		}
	}
	if !pilot.Ready() {
		return fail("billing evidence is incomplete; execution stopped before credentials or fixtures")
	}
	if *dir == "" || *release == "" || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(*revision) || len(files) == 0 {
		return fail("new output, release, reviewed revision and explicit env files required")
	}
	if _, err := os.Lstat(*dir); !os.IsNotExist(err) {
		return fail("new output directory required; no retry/resume")
	}
	if err := pilot.ValidateFrozenInputs(*defs, *profiles, *revision); err != nil {
		return fail("frozen definitions or profiles rejected before credentials or fixtures")
	}
	root, err := repositoryRoot()
	if err != nil {
		return fail("canonical repository unavailable")
	}
	built, modified := executingBuildRevision()
	if err := verifyExecutionRevision(root, *revision, built, modified); err != nil {
		return fail("execution requires verified HEAD-matching build provenance and reviewed frozen inputs")
	}
	env, err := readEnvFiles(files)
	if err != nil {
		return fail("private configuration rejected")
	}
	env["AGENT_RELEASE"] = *release
	model := taskbench.PairModelSettings{BaseURL: env["AGENT_LLM_BASE_URL"], APIKey: env["AGENT_LLM_API_KEY"], Model: env["AGENT_LLM_MODEL"], Protocol: env["AGENT_LLM_PROTOCOL"]}
	bound := pilot.ModelSettings()
	if model.BaseURL != bound.BaseURL || model.Model != bound.Model || model.Protocol != bound.Protocol || strings.TrimSpace(model.APIKey) == "" || env["JEV_MODEL"] != taskbench.AuthorizedPilotJEVModel || strings.TrimSpace(env["JEV_API_KEY"]) == "" || (env["JEV_ENDPOINT"] != "" && env["JEV_ENDPOINT"] != jev.DefaultEndpoint) {
		return fail("private provider settings differ from authorized models/endpoints")
	}
	cfg, err := observability.ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil || !cfg.Enabled || !cfg.CaptureContent || cfg.Environment != "evaluation" || cfg.SampleRate != 1 {
		return fail("explicit full-content/full-sampling evaluation observer required")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Hostname() != "localhost" && (net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback())) {
		return fail("Langfuse must be local")
	}
	if env["PAIRED_FIXTURE_ADMIN_DSN"] == "" {
		return fail("explicit owned-fixture administration configuration required")
	}
	var claimErr error
	if pilot.IsRepeatedDiagnostic() {
		claimErr = claimRepeatedDiagnostic(root, pilot, *dir)
	} else if pilot.IsRepeated() {
		claimErr = claimRepeated(root, pilot, *dir)
	} else if pilot.IsCohort() {
		claimErr = claimCohort(root, pilot, *dir)
	} else if pilot.IsRetest() {
		claimErr = claimRetest(root, pilot, *dir)
	} else if continuation != nil {
		claimErr = claimContinuation(root, p, *previous, *dir)
	} else {
		_, claimErr = claimAuthorization(root, p, *dir)
	}
	if claimErr != nil {
		return fail("authorization already consumed or durable claim failed; reconcile before any further execution")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, 5*time.Minute)
	defer deadline()
	receipt, err := executeFirst(ctx, *dir, *defs, *profiles, *revision, pilot, model, cfg, env, continuation)
	if err != nil {
		fmt.Fprintln(out, "selected batch stopped; inspect private evidence, never automatically retry")
		return 1
	}
	if pilot.IsRepeatedDiagnostic() {
		fmt.Fprintf(out, "%s: fixed repeated export diagnostic completed; inspect all evidence; no further execution\n", receipt.Status)
		return 0
	}
	if pilot.IsRepeated() {
		fmt.Fprintf(out, "%s: fixed 24-pair repeated cohort completed; inspect all evidence; no further execution\n", receipt.Status)
		return 0
	}
	if pilot.IsCohort() {
		fmt.Fprintf(out, "%s: fixed six-case cohort completed; inspect all traces and independent review; no further execution\n", receipt.Status)
		return 0
	}
	batchID := "batch-001"
	if continuation != nil {
		batchID = "batch-002"
	}
	fmt.Fprintf(out, "%s: %s only; inspect traces, scores and semantic review before any later batch\n", receipt.Status, batchID)
	return 0
}
func writeCheck(out io.Writer, p taskbench.AuthorizedPilot) int {
	bound := p.ModelSettings()
	scope := "batch-001 only, then checkpoint"
	if p.IsRepeatedDiagnostic() {
		scope = "fixed 24-pair repeated export diagnostic only, then checkpoint; any failure stops"
	} else if p.IsRepeated() {
		scope = "fixed 24-pair repeated cohort only, then checkpoint; any failure stops"
	} else if p.IsCohort() {
		scope = "fixed six-case cohort only, then checkpoint; first structural/provider/JEV/evidence failure stops"
	}
	if json.NewEncoder(out).Encode(map[string]any{"human_authorized": p.HumanAuthorized(), "campaign_id": p.CampaignID(), "execution_revision": p.ExecutionRevision(), "ready_for_paid_run": p.Ready(), "blockers": p.Blockers(), "mode": "check", "model_base": bound.BaseURL, "model": bound.Model, "protocol": bound.Protocol, "execution_scope": scope, "jev_failure_policy": p.JEVFailurePolicy(), "jev_failure_charges_verified": p.JEVFailureChargesVerified(), "check_scope": "authorization_and_billing_policy_only", "model_calls": 0, "jev_calls": 0}) != nil {
		return 1
	}
	return 0
}
func readEnvFiles(files []string) (map[string]string, error) {
	env := map[string]string{}
	for _, path := range files {
		raw, err := boundedFile(path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok || !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(k) {
				return nil, fmt.Errorf("invalid private setting")
			}
			env[k] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return env, nil
}
func executeFirst(ctx context.Context, dir, defs, profiles, revision string, p taskbench.AuthorizedPilot, model taskbench.PairModelSettings, cfg observability.Config, env map[string]string, continuation ...*taskbench.AuthorizedContinuation) (r taskbench.AuthorizedFirstBatchReceipt, err error) {
	defer func() {
		if _, e := os.Stat(dir); e == nil {
			status := r.Status
			if err != nil {
				status = "stopped"
			}
			raw, _ := json.Marshal(map[string]any{"schema": "paired-live-entry-outcome.v1", "status": status, "execution_receipt": "receipt.json", "cleanup_and_receipt_succeeded": err == nil})
			f, e := os.OpenFile(filepath.Join(dir, "entry-outcome.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				err = errors.Join(err, fmt.Errorf("entry outcome persistence failed"))
				return
			}
			_, w := f.Write(append(raw, '\n'))
			s := f.Sync()
			c := f.Close()
			err = errors.Join(err, w, s, c)
		}
	}()
	fixture, err := taskbench.OpenPairedFixtureEnvironment(ctx, taskbench.PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: env["PAIRED_FIXTURE_ADMIN_DSN"], ProfilesDir: profiles})
	if err != nil {
		return r, fmt.Errorf("owned fixtures unavailable")
	}
	defer func() { err = errors.Join(err, fixture.Close()) }()
	observer, err := observability.New(ctx, cfg)
	if err != nil {
		return r, fmt.Errorf("observer unavailable")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = errors.Join(err, observer.Shutdown(closeCtx))
	}()
	// Fresh standard transports do not inherit a mutable global RoundTripper.
	// Explicit proxy values are parsed from the named private files only.
	modelTransport, err := explicitTransport(env["AGENT_LLM_HTTPS_PROXY"])
	if err != nil {
		return r, err
	}
	jevTransport, err := explicitTransport(env["JEV_HTTPS_PROXY"])
	if err != nil {
		return r, err
	}
	judge, err := jev.NewEvaluationClient(jev.DefaultEndpoint, env["JEV_API_KEY"], taskbench.AuthorizedPilotJEVModel, jevTransport)
	if err != nil {
		return r, fmt.Errorf("JEV client unavailable")
	}
	defer modelTransport.CloseIdleConnections()
	defer jevTransport.CloseIdleConnections()
	opts := taskbench.AuthorizedFirstBatchOptions{Pilot: p, DefinitionsDir: defs, Environment: fixture, Revision: revision, Observer: observer, Model: model, Upstream: modelTransport, JEVFactory: func() jev.Evaluator { return judge }}
	if len(continuation) > 0 && continuation[0] != nil {
		return taskbench.RunAuthorizedSecondBatch(ctx, dir, opts, *continuation[0])
	}
	if p.IsRepeatedDiagnostic() {
		return taskbench.RunAuthorizedRepeatedDiagnostic(ctx, dir, opts)
	}
	if p.IsRepeated() {
		return taskbench.RunAuthorizedRepeated(ctx, dir, opts)
	}
	if p.IsCohort() {
		return taskbench.RunAuthorizedCohort(ctx, dir, opts)
	}
	if p.IsRetest() {
		return taskbench.RunAuthorizedRetest(ctx, dir, opts)
	}
	return taskbench.RunAuthorizedFirstBatch(ctx, dir, opts)
}
func explicitTransport(proxy string) (*http.Transport, error) {
	t := &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 90 * time.Second, ForceAttemptHTTP2: true}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
			return nil, fmt.Errorf("invalid explicit proxy")
		}
		t.Proxy = http.ProxyURL(u)
	}
	return t, nil
}
