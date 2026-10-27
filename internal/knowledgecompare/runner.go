package knowledgecompare

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
	"time"
)

type Options struct {
	Root                  string
	Suite                 Suite
	Guard                 *Guard
	AdminDSN, Out, APIKey string
	Observer              *observability.Observer
	LocalVerification     bool
	CaseIDs               []string
}
type Turn struct {
	Index      int                 `json:"index"`
	Question   string              `json:"question"`
	Reply      string              `json:"reply"`
	HTTPStatus int                 `json:"http_status"`
	Trace      json.RawMessage     `json:"trace"`
	Telemetry  agent.TurnTelemetry `json:"telemetry"`
	Pending    bool                `json:"pending"`
	Snapshot   json.RawMessage     `json:"snapshot"`
	DurationMS float64             `json:"request_ms"`
}
type Task struct {
	CaseID, Arm                                string
	Tenant                                     uint64
	Session                                    string
	Runtime                                    agent.RuntimeMetadata
	Before                                     json.RawMessage
	Turns                                      []Turn
	StatePass, BusinessUnchanged, OwnedCleanup bool
	TerminatedReason                           string `json:"terminated_reason,omitempty"`
}
type Result struct {
	AllTasksDisposed                                bool       `json:"all_tasks_disposed"`
	Schema                                          string     `json:"schema"`
	Status                                          string     `json:"status"`
	EvidenceKind                                    string     `json:"evidence_kind"`
	Contract                                        Contract   `json:"contract"`
	Suite                                           Suite      `json:"suite"`
	Tasks                                           []Task     `json:"tasks"`
	Events                                          []Event    `json:"events"`
	Budget                                          GuardState `json:"budget"`
	Cleanup, Complete                               bool
	PlannedTasks, DefinedUserTurns, ActualUserTurns int
	WallMS                                          float64 `json:"wall_ms"`
}

func Run(ctx context.Context, o Options) (r Result, runErr error) {
	r = Result{Schema: "knowledge-answer100-recording.v1", Status: "stopped", EvidenceKind: "real_provider", Suite: o.Suite, PlannedTasks: 200, DefinedUserTurns: 224}
	if o.LocalVerification {
		r.EvidenceKind = "closed_transport_synthetic"
	}
	if o.Guard == nil || o.Out == "" || o.APIKey == "" || (!o.LocalVerification && o.Observer == nil) {
		return r, fmt.Errorf("guard/observer/explicit credentials required")
	}
	r.Contract = o.Guard.c
	suite, e := LoadSuite(o.Root)
	if e != nil || suite.SHA != o.Guard.c.SuiteSHA || !reflect.DeepEqual(suite, o.Suite) {
		return r, fmt.Errorf("frozen suite differs")
	}
	corpus, e := knowledge.Load(os.DirFS(filepath.Join(o.Root, "knowledge/platform")))
	if e != nil {
		return r, e
	}
	if !o.LocalVerification && corpus.Version() != o.Guard.c.CorpusSHA {
		return r, fmt.Errorf("corpus differs")
	}
	selected := map[string]bool{}
	for _, id := range o.CaseIDs {
		if selected[id] {
			return r, fmt.Errorf("duplicate local case")
		}
		selected[id] = true
	}
	if len(selected) > 0 && !o.LocalVerification {
		return r, fmt.Errorf("real scope cannot select subset")
	}
	if e = os.Mkdir(o.Out, 0700); e != nil {
		return r, e
	}
	start := time.Now()
	defer func() {
		r.Events = o.Guard.Events()
		r.Budget = o.Guard.State()
		r.WallMS = float64(time.Since(start)) / float64(time.Millisecond)
		if runErr != nil {
			r.Status = "stopped"
			r.Complete = false
		}
		reviewErr := writeReview(o.Out, r)
		if reviewErr != nil {
			r.Status = "stopped"
			r.Complete = false
		}
		b, e := json.MarshalIndent(r, "", "  ")
		if e == nil {
			e = writeNew(filepath.Join(o.Out, "recording.json"), append(b, '\n'))
		}
		runErr = errors.Join(runErr, reviewErr, e)
	}()
	env, e := taskbench.OpenPairedFixtureEnvironment(ctx, taskbench.PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: o.AdminDSN, ProfilesDir: filepath.Join(o.Root, "eval/runtime-profiles/2026-10-13-six-profiles-v1")})
	if e != nil {
		return r, e
	}
	defer func() { e := env.Close(); r.Cleanup = e == nil; runErr = errors.Join(runErr, e) }()
	clock := time.Now()
	for i, c := range suite.Cases {
		if len(selected) > 0 && !selected[c.ID] {
			continue
		}
		arms := []string{"a", "b"}
		if i%2 == 1 {
			arms = []string{"b", "a"}
		}
		for _, arm := range arms {
			task, e := runOne(ctx, o, env, corpus, c, arm, clock)
			r.Tasks = append(r.Tasks, task)
			r.ActualUserTurns += len(task.Turns)
			if e != nil {
				return r, e
			}
		}
	}
	if len(selected) > 0 && len(r.Tasks) != 2*len(selected) {
		return r, fmt.Errorf("unknown local case")
	}
	r.setCoverage()
	r.Status = "checkpoint_pending"
	return r, nil
}
func ownedPending(raw json.RawMessage, tenant uint64) bool {
	var x struct {
		Pending *struct {
			Tool   string `json:"tool"`
			Tenant uint64 `json:"tenant_id"`
			Idem   string `json:"idem_key"`
		} `json:"pending_action"`
	}
	if json.Unmarshal(raw, &x) != nil {
		return false
	}
	return x.Pending == nil || (x.Pending.Tenant == tenant && x.Pending.Idem != "" && x.Pending.Tool != "")
}
func runOne(ctx context.Context, o Options, env *taskbench.PairedFixtureEnvironment, corpus *knowledge.Corpus, c Case, arm string, clock time.Time) (task Task, runErr error) {
	task = Task{CaseID: c.ID, Arm: arm, StatePass: true, BusinessUnchanged: true}
	f, e := env.NewKnowledgeTask(ctx, c.InitialPending, clock)
	if e != nil {
		return task, e
	}
	task.Tenant, task.Session = f.Tenant, f.Session
	// A task is unreachable after both local HTTP listeners have closed. Delete
	// only its owned session, and verify absence before another arm may start.
	defer func() {
		f.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, e := f.Redis.Del(cleanupCtx, "agent:session:"+f.Session).Result()
		left, e2 := f.Redis.Exists(cleanupCtx, "agent:session:"+f.Session).Result()
		task.OwnedCleanup = e == nil && e2 == nil && n == 1 && left == 0
		if !task.OwnedCleanup {
			runErr = errors.Join(runErr, fmt.Errorf("owned session cleanup unproved"))
		}
	}()
	before, business, _, e := f.Snapshot(ctx)
	if e != nil {
		return task, e
	}
	task.Before = before
	if !ownedPending(before, f.Tenant) {
		return task, fmt.Errorf("initial pending ownership mismatch")
	}
	cl, e := agent.NewEvaluationLLMClient("https://api.deepseek.com/v1", o.APIKey, "deepseek-flash", "openai", &http.Client{Transport: o.Guard, Timeout: 90 * time.Second}, 512)
	if e != nil {
		return task, e
	}
	proxy := adminProxy(f.AdminURL, o.Guard)
	defer proxy.Close()
	tb := agent.NewToolbox(agent.NewAdminClientWithToken(proxy.URL, f.Token))
	if arm == "b" {
		if e = tb.SetKnowledge(corpus); e != nil {
			return task, e
		}
	}
	h := agent.NewHandler(nil, tb, cl, f.Store, f.Redis, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.SetObserver(o.Observer)
	if e = h.SetRoutingMode("model"); e != nil {
		return task, e
	}
	h.SetAdminAccess(f.Token, 0)
	task.Runtime = *h.EvaluationRuntimeMetadata()
	var current atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithTurn(r.Context(), c.ID+"-"+arm, int(current.Load())))
		r.Header.Set("X-Agent-Internal", f.Token)
		r.Header.Set("X-Agent-Tenant", fmt.Sprint(f.Tenant))
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	for i, q := range c.Turns {
		if e = ctx.Err(); e != nil {
			return task, e
		}
		if i > 0 && q == "确认" && task.Turns[i-1].Pending {
			task.StatePass = false
			task.TerminatedReason = "unexpected_preview_before_empty_confirmation"
			break
		}
		current.Store(int32(i + 1))
		raw, _ := json.Marshal(map[string]string{"session_id": f.Session, "message": q})
		req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		start := time.Now()
		resp, e := server.Client().Do(req)
		if e != nil {
			return task, e
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
		resp.Body.Close()
		turn := Turn{Index: i + 1, Question: q, HTTPStatus: resp.StatusCode, DurationMS: float64(time.Since(start)) / float64(time.Millisecond)}
		var parsed struct {
			Reply     string              `json:"reply"`
			Trace     json.RawMessage     `json:"trace"`
			Telemetry agent.TurnTelemetry `json:"telemetry"`
		}
		if e != nil || len(body) > 2<<20 || json.Unmarshal(body, &parsed) != nil {
			return task, fmt.Errorf("handler response invalid")
		}
		turn.Reply, turn.Trace, turn.Telemetry = parsed.Reply, parsed.Trace, parsed.Telemetry
		snap, after, pending, e := f.Snapshot(ctx)
		turn.Pending, turn.Snapshot = pending, snap
		task.Turns = append(task.Turns, turn)
		if e != nil {
			return task, e
		}
		if !ownedPending(snap, f.Tenant) {
			return task, fmt.Errorf("pending ownership unproved")
		}
		if !knowledgepilot.UnchangedBusiness(business, after) {
			task.BusinessUnchanged = false
			return task, fmt.Errorf("actual business/control state changed")
		}
		if turn.Telemetry.JevCalls != 0 || turn.Telemetry.Runtime == nil || turn.Telemetry.Runtime.RoutingMode != "model" {
			return task, fmt.Errorf("runtime/JEV mismatch")
		}
		if o.Guard.State().Stopped || resp.StatusCode != 200 {
			return task, fmt.Errorf("provider/handler stopped")
		}
		var tools []struct{ Tool, Outcome, Result, Phase string }
		if json.Unmarshal(turn.Trace, &tools) != nil {
			return task, fmt.Errorf("tool trace invalid")
		}
		for _, tr := range tools {
			if tr.Outcome == "failure" {
				task.StatePass = false
				// Unknown tool failures have no trusted technical/quality type in the
				// production schema. Refuse continuation rather than guess from prose.
				return task, fmt.Errorf("untyped tool failure; scope halted")
			}
		}
		for _, required := range c.TurnTools[i] {
			found := false
			for _, tr := range tools {
				found = found || (tr.Tool == required && tr.Outcome == "success")
			}
			task.StatePass = task.StatePass && found
		}
		if !o.LocalVerification {
			if turn.Telemetry.LangfuseTraceID == "" {
				return task, fmt.Errorf("observation identity missing")
			}
			if e = o.Observer.ForceFlush(ctx); e != nil {
				return task, e
			}
			stats := o.Observer.Stats()
			if stats.Dropped != 0 || stats.Failed != 0 {
				return task, fmt.Errorf("observation export failed")
			}
		}
	}
	if len(task.Turns) > 0 {
		final := task.Turns[len(task.Turns)-1]
		if c.ID == "S001" || c.ID == "S002" {
			ok, _, e := knowledgepilot.StateGate(c.ID, before, final.Snapshot, f.Tenant)
			task.StatePass = task.StatePass && e == nil && ok
		} else {
			task.StatePass = task.StatePass && !final.Pending
		}
	}
	return task, nil
}

type reviewTurn struct {
	Index      int             `json:"index"`
	Question   string          `json:"question"`
	Reply      string          `json:"reply"`
	HTTPStatus int             `json:"http_status"`
	Pending    bool            `json:"pending"`
	Snapshot   json.RawMessage `json:"snapshot"`
}

// Runtime, traces, retrieval payload and timing remain in the full recording,
// but are withheld from answer graders to avoid directly revealing the arm.
func writeReview(dir string, r Result) error {
	type item struct {
		ID               string          `json:"review_id"`
		Case             Case            `json:"case"`
		Before           json.RawMessage `json:"before"`
		Turns            []reviewTurn    `json:"turns"`
		StatePass        bool            `json:"state_pass"`
		TerminatedReason string          `json:"terminated_reason,omitempty"`
	}
	var items []item
	mapping := map[string]string{}
	cases := map[string]Case{}
	for _, c := range r.Suite.Cases {
		cases[c.ID] = c
	}
	for _, t := range r.Tasks {
		var b [16]byte
		if _, e := rand.Read(b[:]); e != nil {
			return e
		}
		id := hex.EncodeToString(b[:])
		mapping[id] = t.CaseID + "-" + t.Arm
		var turns []reviewTurn
		for _, v := range t.Turns {
			turns = append(turns, reviewTurn{v.Index, v.Question, v.Reply, v.HTTPStatus, v.Pending, reviewSnapshot(v.Snapshot)})
		}
		items = append(items, item{id, cases[t.CaseID], reviewSnapshot(t.Before), turns, t.StatePass, t.TerminatedReason})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, v := range []struct {
		path  string
		value any
	}{{"review-map.json", mapping}, {"review-package.json", items}} {
		b, e := json.MarshalIndent(v.value, "", "  ")
		if e != nil {
			return e
		}
		if e = writeNew(filepath.Join(dir, v.path), append(b, '\n')); e != nil {
			return e
		}
	}
	return nil
}

func (r *Result) setCoverage() {
	r.AllTasksDisposed = len(r.Tasks) == 200
	r.Complete = r.AllTasksDisposed && r.ActualUserTurns == 224
}

// Audits are available in recording.json, never in the semantic scoring view.
func reviewSnapshot(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var strip func(any)
	strip = func(x any) {
		switch q := x.(type) {
		case map[string]any:
			delete(q, "audits")
			delete(q, "captured_at")
			for _, v := range q {
				strip(v)
			}
		case []any:
			for _, v := range q {
				strip(v)
			}
		}
	}
	strip(v)
	b, _ := json.Marshal(v)
	return b
}
