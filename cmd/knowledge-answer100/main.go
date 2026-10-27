// knowledge-answer100 is independent of the main100+1 funding pool. Its default
// mode reads only public suite files; execute requires a separately approved cap.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	kc "github.com/frostloom/ai-gateway/internal/knowledgecompare"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
func run(args []string, out io.Writer) int {
	f := flag.NewFlagSet("knowledge-answer100", flag.ContinueOnError)
	f.SetOutput(out)
	root := f.String("root", ".", "RAG source checkout")
	execute := f.Bool("execute", false, "one separately approved independent-budget execution")
	contract := f.String("contract", "", "frozen contract JSON")
	proof := f.String("cleanup-proof", "", "additional immutable zero-schema cleanup readback")
	prior := f.String("continue-from", "", "immutable stopped recording for explicitly authorized completion")
	priorJournal := f.String("prior-journal", "", "immutable matching original provider journal")
	approval := f.String("approval", "", "explicit independent extra-budget approval JSON")
	consent := f.String("consent", "", "saved actual user consent text; SHA bound in approval")
	prices := f.String("price-evidence", "", "frozen official price evidence JSON")
	envfile := f.String("env-file", "", "explicit private provider env file")
	obsfile := f.String("observer-env-file", "", "explicit private local Langfuse env file")
	dir := f.String("out", "", "fresh private output directory")
	dsn := f.String("fixture-admin-dsn", "", "explicit literal loopback admin DSN; no database")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return 2
	}
	fail := func(msg string) int { fmt.Fprintln(out, msg); return 2 }
	s, e := kc.LoadSuite(*root)
	if e != nil {
		return fail("frozen suite rejected")
	}
	if !*execute {
		json.NewEncoder(out).Encode(map[string]any{"mode": "check", "cases": len(s.Cases), "user_turns": 2 * s.UserTurns(), "suite_sha256": s.SHA, "funding_pool": "knowledge-independent", "proposed_extra_cny": 20, "approved": false, "model_calls": 0})
		return 0
	}
	var c kc.Contract
	if readJSON(*contract, &c) != nil || c.Validate() != nil || c.SuiteSHA != s.SHA {
		return fail("execution contract rejected")
	}
	var a kc.Approval
	if readJSON(*approval, &a) != nil || kc.CheckApproval(c, a) != nil {
		return fail("explicit independent extra-budget approval missing")
	}
	raw, e := knowledgepilot.ReadBounded(*consent)
	if e != nil || knowledgepilot.SHA(raw) != a.ConsentSHA {
		return fail("user consent digest differs")
	}
	// Identity gates precede credentials, database access and permanent claims.
	rev, e := git(*root, "rev-parse", "HEAD")
	if e != nil || rev != c.Revision {
		return fail("source revision differs")
	}
	branch, e := git(*root, "branch", "--show-current")
	if e != nil || branch != "codex/customer-knowledge-rag" {
		return fail("RAG branch required")
	}
	dirty, e := git(*root, "status", "--porcelain", "--untracked-files=all")
	if e != nil || dirty != "" {
		return fail("clean frozen checkout required")
	}
	binary, e := os.Executable()
	if e != nil || checkSHA(binary, c.BinarySHA) != nil {
		return fail("binary digest differs")
	}
	if checkSHA(*prices, c.PriceSHA) != nil || checkSHA(filepath.Join(*root, "eval/knowledge-reports/2026-10-27-main-baseline/receipt.json"), c.MainSHA) != nil {
		return fail("price/main reference evidence differs")
	}
	corpus, e := knowledge.Load(os.DirFS(filepath.Join(*root, "knowledge/platform")))
	if e != nil || corpus.Version() != c.CorpusSHA {
		return fail("corpus differs")
	}
	if *dir == "" || *dsn == "" {
		return fail("explicit new output and fixture DSN required")
	}
	if _, e := os.Lstat(*dir); !os.IsNotExist(e) {
		return fail("output must be absent")
	}
	if st, e := os.Stat(filepath.Dir(*dir)); e != nil || !st.IsDir() {
		return fail("output parent missing")
	}
	common, e := git(*root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return fail("common Git root unavailable")
	}
	// Fixed path independent of caller output path/revision prevents reissuing the
	// same campaign under a new working directory or executable.
	claimDir := filepath.Join(common, "knowledge-independent-claims")
	parentClaimPath := filepath.Join(claimDir, kc.CampaignID+".json")
	if c.PriorRecordingSHA != "" {
		var prev kc.Result
		if readJSON(*prior, &prev) != nil {
			return fail("prior recording rejected")
		}
		if prev.Contract.PriorRecordingSHA != "" {
			parentClaimPath = filepath.Join(claimDir, kc.CampaignID+".continue-"+prev.Contract.PriorRecordingSHA+".json")
		}
	}
	claimPath := parentClaimPath
	if c.PriorRecordingSHA != "" {
		if *prior == "" || *priorJournal == "" {
			return fail("completion evidence required")
		}
		claimPath = filepath.Join(claimDir, kc.CampaignID+".continue-"+c.PriorRecordingSHA+".json")
	} else if *prior != "" || *priorJournal != "" {
		return fail("completion must be bound in contract")
	}

	vals, e := readEnv(*envfile)
	if e != nil || vals["AGENT_LLM_API_KEY"] == "" {
		return fail("explicit provider credentials unavailable")
	}
	ov, e := readEnv(*obsfile)
	if e != nil {
		return fail("explicit observer credentials unavailable")
	}
	ov["AGENT_LANGFUSE_ENABLED"] = "true"
	ov["AGENT_LANGFUSE_CAPTURE_CONTENT"] = "true"
	ov["AGENT_LANGFUSE_SAMPLE_RATE"] = "1"
	ov["AGENT_LANGFUSE_ENVIRONMENT"] = "evaluation"
	ov["AGENT_RELEASE"] = kc.CampaignID + "-" + c.Revision
	oc, e := observability.ConfigFromEnv(func(k string) string { return ov[k] })
	if e != nil {
		return fail("observer configuration rejected")
	}
	u, e := url.Parse(oc.Endpoint)
	if e != nil || !loopback(u.Hostname()) {
		return fail("literal loopback observer required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	// Provider uses one explicit proxy, if configured in this private env file.
	// No environment-driven fallback, redirects or retries.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if proxy := vals["HTTPS_PROXY"]; proxy != "" {
		p, e := url.Parse(proxy)
		if e != nil || p.Scheme != "http" || !loopback(p.Hostname()) || p.User != nil {
			return fail("explicit loopback HTTP proxy required")
		}
		tr.Proxy = http.ProxyURL(p)
	}
	defer tr.CloseIdleConnections()
	if c.ObserverTimeoutMS != 0 {
		oc.ExportTimeout = time.Duration(c.ObserverTimeoutMS) * time.Millisecond
	}
	obs, e := observability.New(ctx, oc)
	if e != nil {
		return fail("observer unavailable")
	}
	defer func() {
		closeCtx, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		obs.Shutdown(closeCtx)
	}()
	if e = os.MkdirAll(claimDir, 0700); e != nil {
		return fail("claim directory unavailable")
	}
	var cap *kc.Capability
	if c.PriorRecordingSHA != "" {
		if c.CleanupProofSHA != "" {
			if *proof == "" || checkSHA(*proof, c.CleanupProofSHA) != nil || *dsn != "root:root@tcp(127.0.0.1:3307)/" {
				return fail("local cleanup proof required")
			}
			db, err := sql.Open("mysql", *dsn)
			if err != nil {
				return fail("cleanup recheck unavailable")
			}
			checkCtx, cc := context.WithTimeout(ctx, 5*time.Second)
			var remaining int
			err = db.QueryRowContext(checkCtx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name LIKE 'ai_gateway_pair_%'").Scan(&remaining)
			cc()
			db.Close()
			if err != nil || remaining != 0 {
				return fail("owned schema absence not verified")
			}
			cap, e = kc.AcquireCompletion(claimPath, c, a, parentClaimPath, *prior, *priorJournal, *proof)
		} else {
			cap, e = kc.AcquireCompletion(claimPath, c, a, parentClaimPath, *prior, *priorJournal)
		}
	} else {
		cap, e = kc.Acquire(claimPath, c, a)
	}
	if e != nil {
		return fail("permanent campaign claim refused; no retry or reissue")
	}
	guard, e := kc.NewGuard(*dir+".events.jsonl", cap, tr)
	if e != nil {
		return fail("fresh durable journal unavailable; claim remains consumed")
	}
	defer guard.Close()
	r, e := kc.Run(ctx, kc.Options{Root: *root, Suite: s, Guard: guard, AdminDSN: *dsn, Out: *dir, APIKey: vals["AGENT_LLM_API_KEY"], Observer: obs})
	json.NewEncoder(out).Encode(map[string]any{"status": r.Status, "tasks": len(r.Tasks), "actual_user_turns": r.ActualUserTurns, "defined_user_turns": r.DefinedUserTurns, "cleanup": r.Cleanup, "complete": r.Complete, "independent_budget": r.Budget, "accuracy": nil})
	if e != nil {
		return 1
	}
	return 0
}
func git(root string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	b, e := c.Output()
	return strings.TrimSpace(string(b)), e
}
func readJSON(path string, v any) error {
	b, e := knowledgepilot.ReadBounded(path)
	if e != nil {
		return e
	}
	return knowledgepilot.StrictDecode(b, v)
}
func checkSHA(path, want string) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if knowledgepilot.SHA(b) != want {
		return fmt.Errorf("digest differs")
	}
	return nil
}
func loopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func readEnv(path string) (map[string]string, error) {
	b, e := knowledgepilot.ReadBounded(path)
	if e != nil {
		return nil, e
	}
	v := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, x, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid private env")
		}
		v[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(x), "\"'")
	}
	return v, nil
}
