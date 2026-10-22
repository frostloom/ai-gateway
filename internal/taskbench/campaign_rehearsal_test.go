package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func localCampaignOptions(t *testing.T) LocalCampaignOptions {
	t.Helper()
	var plan CampaignPreparationPlan
	raw, err := os.ReadFile("../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	env, err := OpenPairedFixtureEnvironment(ctx, PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Error(err)
		}
	})
	return LocalCampaignOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Environment: env, Revision: "test-local-rehearsal", Plan: plan}
}

func TestLocalCampaignStopsOnDependencyFailures(t *testing.T) {
	o := localCampaignOptions(t)
	for _, kind := range []string{"model-http", "model-usage", "jev", "otlp-export", "otlp-capture", "report-write"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "fault")
			factory := func(ctx context.Context) (*localCampaignDependencies, error) {
				d, err := newLocalCampaignDependencies(ctx)
				if err != nil {
					return nil, err
				}
				model, judge := d.model, d.judge
				d.model = func(c ConversationCase) http.RoundTripper {
					up := model(c)
					if c.Key() != o.Plan.Batches[1].Key {
						return up
					}
					if kind == "otlp-capture" {
						d.capture.Lock()
						d.capture.failed = true
						d.capture.Unlock()
					}
					if kind == "report-write" {
						if err := os.Mkdir(filepath.Join(dir, "campaign", "batch-002", "report.json"), 0700); err != nil {
							t.Fatal(err)
						}
					}
					return pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
						res, err := up.RoundTrip(req)
						if err != nil {
							return res, err
						}
						if kind == "model-http" {
							res.StatusCode = 503
						}
						if kind == "model-usage" {
							var body map[string]any
							json.NewDecoder(res.Body).Decode(&body)
							res.Body.Close()
							delete(body, "usage")
							raw, _ := json.Marshal(body)
							res.Body = io.NopCloser(strings.NewReader(string(raw)))
						}
						return res, nil
					})
				}
				d.judge = func(c ConversationCase) jev.Evaluator {
					if kind == "jev" && c.Key() == o.Plan.Batches[1].Key {
						return jev.NewFake()
					}
					return judge(c)
				}
				if kind == "otlp-export" {
					// The collector accepts batch one, then fails real exports.
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						d.capture.Lock()
						done := len(d.capture.spans) > 0
						d.capture.Unlock()
						if done {
							w.WriteHeader(503)
							return
						}
						d.capture.ServeHTTP(w, r)
					}))
					observer, e := observability.New(ctx, observability.Config{Enabled: true, Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "pk-test", SecretKey: "sk-test", HashKey: strings.Repeat("h", 32), Environment: "evaluation", SampleRate: 1, QueueSize: 256, ExportTimeout: time.Second})
					if e != nil {
						t.Fatal(e)
					}
					close := d.close
					d.observer = observer
					d.close = func() error {
						ctx, cancel := localCampaignCleanupContext()
						defer cancel()
						e := observer.Shutdown(ctx)
						server.Close()
						return errors.Join(e, close())
					}
				}
				return d, nil
			}
			r, err := runLocalCampaign(context.Background(), dir, o, factory)
			if err == nil || r.Status != "stopped" || r.CompletedBatches > 1 || r.Batches[2].Status != "not_started" {
				t.Fatalf("expanded after %s: %+v %v", kind, r, err)
			}
			if kind != "otlp-export" && r.CompletedBatches != 1 {
				t.Fatalf("failed before injected fault: %+v %v", r, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "campaign", "batch-003")); !os.IsNotExist(err) {
				t.Fatal("later batch reserved")
			}
			if _, err := os.Stat(filepath.Join(dir, "receipt.json")); err != nil {
				t.Fatal("partial receipt lost")
			}
		})
	}
}

func TestLocalCampaignReceiptFailureCannotReportCompleted(t *testing.T) {
	o := localCampaignOptions(t)
	o.Plan.Batches = o.Plan.Batches[:1]
	dir := filepath.Join(t.TempDir(), "write-fault")
	r, err := runLocalCampaign(context.Background(), dir, o, func(ctx context.Context) (*localCampaignDependencies, error) {
		if err := os.Mkdir(filepath.Join(dir, "receipt.json"), 0700); err != nil {
			t.Fatal(err)
		}
		return newLocalCampaignDependencies(ctx)
	})
	if err == nil || r.Status != "stopped" || r.FailedStage != "receipt_write" {
		t.Fatalf("receipt failure reported completion: %+v %v", r, err)
	}
}

func TestLocalCampaignFactCheckpoint(t *testing.T) {
	d, err := newLocalCampaignDependencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	var report SmallPairReport
	report.Schema = "small-paired-report.v1"
	report.EvidenceKind = "local_stub"
	report.FinishedArms = 2
	report.PlannedArms = 2
	zero := 0
	cost := int64(0)
	for _, arm := range []string{"model", "jev_selective"} {
		_, run := d.observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "local-check", CaseKey: "case", Arm: arm, EvidenceKind: "local_stub"})
		run.End(observability.EvaluationResult{Status: "needs_review", StructuralPass: true})
		report.Rows = append(report.Rows, PairReportRow{Key: "case", Arm: arm, LangfuseTraceID: run.TraceID(), Status: "needs_review", StructuralPass: true, ModelAttempts: &zero, PricedModelMicroCNY: &cost})
	}
	if err := d.observer.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := PairBudgetState{CostMicroCNY: &cost}
	if _, err := checkLocalCampaignBatch(report, state, d.capture); err != nil {
		t.Fatal("baseline checkpoint invalid", err)
	}
	report.Rows[1].FactErrors = 1
	if _, err := checkLocalCampaignBatch(report, state, d.capture); err == nil {
		t.Fatal("fact mismatch accepted at checkpoint")
	}
}

func TestLocalCampaignSixBatches(t *testing.T) {
	// Inherited provider and observer settings cannot replace closed substitutes.
	t.Setenv("AGENT_LLM_API_KEY", "inherited-model-marker")
	t.Setenv("AGENT_LLM_BASE_URL", "https://provider.invalid")
	t.Setenv("JEV_API_KEY", "inherited-jev-marker")
	t.Setenv("TASKBENCH_LANGFUSE_SMOKE", "1")
	o := localCampaignOptions(t)
	dir := filepath.Join(t.TempDir(), "rehearsal")
	if base := os.Getenv("TASKBENCH_LOCAL_CAMPAIGN_RECEIPTS"); base != "" {
		dir = filepath.Join(base, "six-batches")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	r, err := RunLocalCampaign(ctx, dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.CompletedBatches != 6 || r.PlannedArms != 12 || r.PlannedUserTurns != 26 || r.ModelAttempts != 20 || r.JEVAttempts != 6 {
		t.Fatalf("wrong local totals: %+v", r)
	}
	if r.AllowPaid || r.ReadyForPaidRun || r.ExternalModelCalls != 0 || r.ExternalJEVCalls != 0 || r.EvidenceKind != "local_stub" || r.Status != "completed_local" {
		t.Fatalf("local evidence misrepresented: %+v", r)
	}
	if r.Observations.Tasks != 12 || r.Observations.Turns != 26 || r.Observations.Generations != 20 {
		t.Fatalf("OTLP readback incomplete: %+v", r.Observations)
	}
	ids := map[uint64]bool{}
	for i, b := range r.Batches {
		if b.Key != o.Plan.Batches[i].Key || b.Status != "checked_local" || b.Report == nil || b.Report.FinishedArms != 2 || b.Report.EvidenceKind != "local_stub" || b.Report.EffectClaimsAllowed {
			t.Fatalf("batch order or report invalid: %+v", b)
		}
		path := filepath.Join(dir, "campaign", b.ID, "run.jsonl")
		j, err := loadPairJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		if j.cfg.AllowPaid || j.cfg.Model.ExecutionRevision != o.Revision {
			t.Fatal("lost local revision/boundary")
		}
		for _, row := range b.Report.Rows {
			if row.Status != "needs_review" || !row.StructuralPass || row.FactErrors != 0 || row.HTTPFailures != 0 || row.PricedModelMicroCNY == nil || row.ModelAttempts == nil {
				t.Fatalf("report is not checked, pending semantic review: %+v", row)
			}
			id := j.finished[row.Key+":"+row.Arm].Result.Fixture.TenantID
			if ids[id] {
				t.Fatal("batch/arm reused an account")
			}
			ids[id] = true
		}
		var saved SmallPairReport
		raw, err := os.ReadFile(filepath.Join(dir, "campaign", b.ID, "report.json"))
		if err != nil || json.Unmarshal(raw, &saved) != nil || saved.JournalSHA256 != b.Report.JournalSHA256 {
			t.Fatal("report not persisted")
		}
	}
	for _, name := range []string{"receipt.json", "campaign/campaign.jsonl", "campaign/batch-001/report.json"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatalf("unsafe evidence mode: %s", name)
		}
	}
	before, _ := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if _, err = RunLocalCampaign(ctx, dir, o); err == nil {
		t.Fatal("reused output directory")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if string(before) != string(after) {
		t.Fatal("overwrote completed evidence")
	}
	if strings.Contains(string(before), "inherited-") {
		t.Fatal("inherited credential serialized")
	}
	t.Logf("local rehearsal: batches=6 arms=12 turns=26 model=20 JEV=6; OTLP=12/26/20; external=0")
}

func TestLocalCampaignInvalidLaterBatchHasNoOutput(t *testing.T) {
	o := localCampaignOptions(t)
	for _, kind := range []string{"unknown", "unsupported-script", "budget", "revision", "closed-environment"} {
		t.Run(kind, func(t *testing.T) {
			copy := o
			raw, _ := json.Marshal(o.Plan)
			json.Unmarshal(raw, &copy.Plan)
			switch kind {
			case "unknown":
				copy.Plan.Batches[5].Key = "unknown/case"
			case "unsupported-script":
				copy.Plan.Batches[5].Key = "plans-001/answer_complete"
			case "budget":
				copy.Plan.Limits.MaxModelCalls = 23
			case "revision":
				copy.Revision = ""
			case "closed-environment":
				copy.Environment = nil
			}
			dir := filepath.Join(t.TempDir(), "invalid")
			if _, err := RunLocalCampaign(context.Background(), dir, copy); err == nil {
				t.Fatal("invalid rehearsal accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("invalid input created output")
			}
		})
	}
}

func TestLocalCampaignStopsAfterSecondBatchBudgetFailure(t *testing.T) {
	o := localCampaignOptions(t)
	o.Plan.Batches[1].ModelLimits.MaxCalls = 1
	dir := filepath.Join(t.TempDir(), "stopped")
	r, err := RunLocalCampaign(context.Background(), dir, o)
	if err == nil || r.Status != "stopped" || r.CompletedBatches != 1 || len(r.Batches) != 6 || r.Batches[1].Status != "failed" {
		t.Fatalf("failure lost: %+v err=%v", r, err)
	}
	for _, b := range r.Batches[2:] {
		if b.Status != "not_started" {
			t.Fatal("failed campaign expanded")
		}
		if _, err := os.Stat(filepath.Join(dir, "campaign", b.ID)); !os.IsNotExist(err) {
			t.Fatal("reserved a later batch")
		}
	}
	var saved LocalCampaignReceipt
	raw, e := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if e != nil || json.Unmarshal(raw, &saved) != nil || saved.Status != "stopped" || saved.CompletedBatches != 1 {
		t.Fatal("partial receipt lost")
	}
}
