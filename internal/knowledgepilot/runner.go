package knowledgepilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type RunOptions struct {
	AllowExecution bool
	Root           string
	Inputs         Inputs
	Claim          *Claim
	Transport      *Transport
	AdminDSN       string
	Out            string
	CaseIDs        []string
	Observer       *observability.Observer
}
type Receipt struct {
	Status    string    `json:"status"`
	Cleanup   bool      `json:"cleanup"`
	Recording Recording `json:"recording"`
	WallMS    float64   `json:"runner_wall_ms"`
	Failure   string    `json:"failure,omitempty"`
}
type forbiddenJEV struct{}

func (forbiddenJEV) Evaluate(context.Context, any, map[string]jev.Question) (*jev.Result, error) {
	return nil, fmt.Errorf("JEV forbidden in knowledge experiment")
}
func Run(ctx context.Context, o RunOptions) (receipt Receipt, runErr error) {
	start := time.Now()
	receipt.Status = "stopped"
	if !o.AllowExecution || o.Claim == nil || !o.Claim.local || o.Transport == nil || o.Transport.claim != o.Claim || o.Out == "" {
		return receipt, fmt.Errorf("explicit local execution capability required")
	}
	in, err := LoadInputs(o.Root, o.Claim.scope)
	if err != nil {
		return receipt, err
	}
	if !reflect.DeepEqual(in.Cases, o.Inputs.Cases) || !reflect.DeepEqual(in.Profiles, o.Inputs.Profiles) {
		return receipt, fmt.Errorf("inputs differ")
	}
	ids := map[string]bool{}
	for _, id := range o.CaseIDs {
		if ids[id] || in.Profiles[id].Pending == "" {
			return receipt, fmt.Errorf("unknown or duplicate local case")
		}
		ids[id] = true
	}
	if err = os.Mkdir(o.Out, 0700); err != nil {
		return receipt, fmt.Errorf("new private output required")
	}
	receipt.Recording = Recording{EvidenceKind: "local_stub", Scope: in.Scope, Suite: in.Cases}
	defer func() {
		receipt.WallMS = float64(time.Since(start)) / float64(time.Millisecond)
		receipt.Recording.Events = o.Transport.journal.Events()
		if runErr != nil {
			receipt.Failure = "execution_stopped"
		}
		b, e := json.MarshalIndent(receipt, "", "  ")
		if e == nil {
			e = writeExclusive(filepath.Join(o.Out, "receipt.json"), append(b, '\n'))
		}
		runErr = errors.Join(runErr, e, WriteReviewPackage(o.Out, receipt.Recording))
	}()
	env, err := taskbench.OpenPairedFixtureEnvironment(ctx, taskbench.PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: o.AdminDSN, ProfilesDir: filepath.Join(o.Root, "eval/runtime-profiles/2026-10-13-six-profiles-v1")})
	if err != nil {
		return receipt, err
	}
	defer func() {
		e := env.Close()
		receipt.Cleanup = e == nil
		runErr = errors.Join(runErr, e)
		if e != nil {
			receipt.Status = "stopped"
			receipt.Recording.Complete = false
		}
	}()
	cases := map[string]knowledgeeval.Case{}
	for _, c := range in.Cases {
		cases[c.ID] = c
	}
	clock := time.Now()
	for _, scheduled := range BuildOrder(in.Cases) {
		if len(ids) > 0 && !ids[scheduled.CaseID] {
			continue
		}
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		task, err := runTask(ctx, o, in, env, scheduled, cases[scheduled.CaseID].Question, clock)
		receipt.Recording.Tasks = append(receipt.Recording.Tasks, task)
		if err != nil {
			return receipt, err
		}
	}
	if o.Observer != nil {
		if err = o.Observer.ForceFlush(ctx); err != nil {
			return receipt, err
		}
		stats := o.Observer.Stats()
		if stats.Failed != 0 || stats.Dropped != 0 {
			return receipt, fmt.Errorf("observer export incomplete")
		}
	}
	receipt.Recording.Complete = len(receipt.Recording.Tasks) == 144
	receipt.Status = "checkpoint_pending"
	return receipt, nil
}
func runTask(ctx context.Context, o RunOptions, in Inputs, env *taskbench.PairedFixtureEnvironment, s ScheduledTask, question string, clock time.Time) (task RecordedTask, err error) {
	task = RecordedTask{CaseID: s.CaseID, Arm: s.Arm, Identity: s.Identity}
	f, err := env.NewKnowledgeTask(ctx, in.Profiles[s.CaseID].Pending == "recharge_100", clock)
	if err != nil {
		return task, err
	}
	defer f.Close()
	task.Tenant, task.Session = f.Tenant, f.Session
	before, business, pending, err := f.Snapshot(ctx)
	if err != nil {
		return task, err
	}
	task.Before, task.PendingBefore = before, pending
	client, err := agent.NewEvaluationLLMClient(o.Claim.endpoint, "local-unused", in.Scope.Model, "openai", &http.Client{Transport: o.Transport, Timeout: 30 * time.Second}, in.Scope.Limits.Output)
	if err != nil {
		return task, err
	}
	tb := agent.NewToolbox(agent.NewAdminClientWithToken(f.AdminURL, f.Token))
	if s.Arm == "b" {
		if err = tb.SetKnowledge(in.Corpus); err != nil {
			return task, err
		}
	}
	h := agent.NewHandler(nil, tb, client, f.Store, f.Redis, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.SetObserver(o.Observer)
	h.SetJev(forbiddenJEV{})
	if err = h.SetRoutingMode("model"); err != nil {
		return task, err
	}
	h.SetAdminAccess(f.Token, 0)
	task.Runtime = *h.EvaluationRuntimeMetadata()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithTask(r.Context(), s.Identity))
		r.Header.Set("X-Agent-Internal", f.Token)
		r.Header.Set("X-Agent-Tenant", fmt.Sprint(f.Tenant))
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	raw, _ := json.Marshal(map[string]string{"session_id": f.Session, "message": question})
	req, _ := http.NewRequestWithContext(WithTask(ctx, s.Identity), "POST", server.URL, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	begin := time.Now()
	resp, e := server.Client().Do(req)
	if e != nil {
		return task, e
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	task.DurationMS = float64(time.Since(begin)) / float64(time.Millisecond)
	task.HTTPStatus = resp.StatusCode
	var result struct {
		Reply     string              `json:"reply"`
		Trace     json.RawMessage     `json:"trace"`
		Telemetry agent.TurnTelemetry `json:"telemetry"`
	}
	if e != nil || json.Unmarshal(body, &result) != nil {
		return task, fmt.Errorf("handler response unreadable")
	}
	task.Reply, task.Trace, task.Telemetry = result.Reply, result.Trace, result.Telemetry
	after, current, afterPending, e := f.Snapshot(ctx)
	if e != nil {
		return task, e
	}
	task.After, task.PendingAfter = after, afterPending
	task.BusinessUnchanged = UnchangedBusiness(business, current)
	task.StateOK = task.BusinessUnchanged
	if s.CaseID == "S011" {
		task.StateOK = task.StateOK && task.PendingBefore && !task.PendingAfter
	}
	if s.CaseID == "S012" {
		task.StateOK = task.StateOK && !task.PendingBefore && !task.PendingAfter
	}
	var traces []struct {
		Tool   string `json:"tool"`
		Result string `json:"result"`
	}
	json.Unmarshal(task.Trace, &traces)
	for _, tr := range traces {
		if tr.Tool != "search_platform_knowledge" {
			continue
		}
		var r knowledge.Result
		if json.Unmarshal([]byte(tr.Result), &r) != nil {
			return task, fmt.Errorf("knowledge result unreadable")
		}
		refs := []json.RawMessage{}
		for _, hit := range r.Hits {
			b, _ := json.Marshal(knowledgeeval.Evidence{ChunkID: hit.ChunkID, SHA256: hit.SHA256, CorpusVersion: r.CorpusVersion})
			refs = append(refs, b)
		}
		task.Searches = append(task.Searches, refs)
	}
	for _, ev := range o.Transport.journal.Events() {
		if ev.TaskID != s.Identity {
			continue
		}
		if ev.Kind == "completed" && ev.Attempt != nil {
			task.Attempts = append(task.Attempts, *ev.Attempt)
		} else if ev.Kind == "failed" {
			task.Attempts = append(task.Attempts, knowledgeeval.Attempt{Outcome: "failed", Currency: "CNY"})
		}
	}
	if !task.BusinessUnchanged {
		return task, fmt.Errorf("business state changed")
	}
	if task.Telemetry.JevCalls != 0 {
		return task, fmt.Errorf("unexpected JEV call")
	}
	if resp.StatusCode != 200 {
		return task, fmt.Errorf("handler execution failed")
	}
	return task, nil
}
