package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/jev"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFullLocalWindowPreparationRejectsMissingAndDuplicateKeys(t *testing.T) {
	source, profiles := fullAdapterSource(t)
	for _, keys := range [][]string{nil, {}, make([]string, 101), {"models-001/answer_complete", "models-001/answer_complete"}, {"recharge-002/cancel"}, {"models-001/first_turn"}, {"not-real/answer_complete"}} {
		if _, err := PrepareFullLocalCustomerWindow(source, profiles, keys, "test"); err == nil {
			t.Fatal("invalid window accepted", keys)
		}
		dir := filepath.Join(t.TempDir(), "run")
		calls := 0
		_, err := runFullLocalCustomerWindow(context.Background(), dir, FullLocalCampaignOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Environment: &PairedFixtureEnvironment{profiles: profiles}, Revision: "test", Keys: keys}, func(context.Context) (*localCampaignDependencies, error) {
			calls++
			return nil, fmt.Errorf("must not construct")
		})
		if err == nil || calls != 0 {
			t.Fatal("invalid selection reached dependency construction")
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("invalid selection created output")
		}
	}
}

func TestFullLocalWindowActualThirteenSourceFamilies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	keys := []string{"adv-001/answer_complete", "balance-001/answer_complete", "bills-001/answer_complete", "cancel_subscription-001/confirm", "change_plan-001/supplement_then_confirm", "chitchat-001/answer_complete", "consumption-001/answer_complete", "models-001/answer_complete", "plans-001/answer_complete", "recharge-001/supplement_then_confirm", "refund-001/supplement_then_confirm", "subscribe-001/supplement_then_confirm", "subscription-001/answer_complete"}
	dir := filepath.Join(t.TempDir(), "window")
	o := FullLocalCampaignOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Environment: env, Revision: "test", Keys: keys}
	r, err := RunFullLocalCustomerWindow(ctx, dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed_local" || r.CompletedBatches != 13 || r.PlannedArms != 26 || r.PlannedUserTurns != 44 || r.Observations.Tasks != 26 || r.Observations.Turns != 44 || r.Observations.Generations != r.ModelAttempts {
		t.Fatalf("window counts wrong: %+v", r)
	}
	if r.AllowPaid || r.ReadyForPaidRun || r.ExternalModelCalls != 0 || r.ExternalJEVCalls != 0 || r.ActualFull1220Complete {
		t.Fatal("local result upgraded to actual evaluation")
	}
	if r.SelectionSHA256 != "b9b6f48daccffa6bd7c0f86e3f8d4857eedc719404406014d3426b0656edd2f8" {
		t.Fatal("full selection identity lost")
	}
	for i, b := range r.Batches {
		if b.Key != keys[i] || b.Status != "checked_local" || b.Report == nil {
			t.Fatal("case missing")
		}
		for _, row := range b.Report.Rows {
			if !row.StructuralPass || row.Status != "needs_review" || row.FactErrors != 0 {
				t.Fatalf("synthetic quality incorrectly scored: %+v", row)
			}
		}
	}
	var evidence struct {
		Spans []fullLocalSpanEvidence `json:"spans"`
	}
	captureRaw, captureErr := os.ReadFile(filepath.Join(dir, "local-observations.json"))
	if captureErr != nil || json.Unmarshal(captureRaw, &evidence) != nil || conversationHash(captureRaw) != r.LocalOTLPSHA256 {
		t.Fatal("local capture evidence missing or unbound")
	}
	counts := map[string]int{}
	for _, span := range evidence.Spans {
		counts[span.Name]++
	}
	if counts["evaluation.task"] != r.Observations.Tasks || counts["agent.turn"] != r.Observations.Turns || counts["llm.attempt"] != r.Observations.Generations {
		t.Fatal("raw capture category counts differ", counts)
	}
	var saved FullLocalCampaignReceipt
	raw, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Status != r.Status {
		t.Fatal("receipt not durable")
	}
	if _, err := RunFullLocalCustomerWindow(ctx, dir, o); err == nil {
		t.Fatal("existing window was resumed/overwritten")
	}
}

func TestFullLocalWindowDependencyFailureIsDurable(t *testing.T) {
	_, profiles := fullAdapterSource(t)
	dir := filepath.Join(t.TempDir(), "window")
	r, err := runFullLocalCustomerWindow(context.Background(), dir, FullLocalCampaignOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Environment: &PairedFixtureEnvironment{profiles: profiles}, Revision: "test", Keys: []string{"models-001/answer_complete"}}, func(context.Context) (*localCampaignDependencies, error) {
		return nil, fmt.Errorf("injected dependency failure")
	})
	if err == nil || r.Status != "stopped" || r.FailedStage != "dependencies" || r.CompletedBatches != 0 {
		t.Fatal("dependency failure hidden")
	}
	var saved FullLocalCampaignReceipt
	raw, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Status != "stopped" || saved.Batches[0].Status != "not_started" {
		t.Fatal("failed window receipt lost")
	}
}

func TestFullLocalWindowStopsOnInjectedFaults(t *testing.T) {
	ctx := context.Background()
	env, err := OpenPairedFixtureEnvironment(ctx, PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := env.Close(); e != nil {
			t.Error(e)
		}
	}()
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := loadFullLocalReadParameters("../../eval/task-definitions/2026-10-12-full-dev-v1", source.DefinitionSHA256)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for ref, p := range env.profiles.archive.Catalogue.Plans {
		names[ref] = p.Name
	}
	for _, fault := range []string{"provider", "judge", "capture", "budget", "cancel", "report", "receipt", "cleanup"} {
		t.Run(fault, func(t *testing.T) {
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			dir := filepath.Join(t.TempDir(), "window")
			calls := 0
			r, err := runFullLocalCustomerWindow(runCtx, dir, FullLocalCampaignOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Environment: env, Revision: "test", Keys: []string{"models-001/answer_complete", "plans-001/answer_complete"}}, func(ctx context.Context) (*localCampaignDependencies, error) {
				d, e := newFullLocalDependencies(ctx, parameters, names)
				if e != nil {
					return nil, e
				}
				original := d.model
				d.model = func(c ConversationCase) http.RoundTripper {
					transport := original(c)
					return pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						switch fault {
						case "provider":
							return nil, fmt.Errorf("injected provider failure")
						case "cancel":
							cancel()
							return nil, context.Canceled
						case "report":
							if calls == 1 {
								if e := os.Mkdir(filepath.Join(dir, "campaign", "batch-001", "report.json"), 0700); e != nil {
									t.Fatal(e)
								}
							}
						}
						response, e := transport.RoundTrip(req)
						if fault == "budget" && e == nil {
							raw, _ := io.ReadAll(response.Body)
							response.Body.Close()
							raw = bytes.Replace(raw, []byte(`"input_tokens":100`), []byte(`"input_tokens":20000`), 1)
							response.Body = io.NopCloser(bytes.NewReader(raw))
						}
						return response, e
					})
				}
				if fault == "judge" {
					d.judge = func(ConversationCase) jev.Evaluator { return policyFailureJEV{} }
				}
				if fault == "capture" {
					d.capture.Lock()
					d.capture.failed = true
					d.capture.Unlock()
				}
				closeDeps := d.close
				d.close = func() error {
					e := closeDeps()
					if fault == "receipt" {
						e = errors.Join(e, os.Mkdir(filepath.Join(dir, "receipt.json"), 0700))
					}
					if fault == "cleanup" {
						e = errors.Join(e, fmt.Errorf("injected cleanup failure"))
					}
					return e
				}
				return d, nil
			})
			if err == nil || r.Status != "stopped" || r.FailedStage == "" {
				t.Fatalf("fault hidden: %+v %v", r, err)
			}
			if fault != "receipt" && fault != "cleanup" {
				if r.CompletedBatches != 0 || r.Batches[1].Status != "not_started" {
					t.Fatal("next case ran after failure")
				}
			}
			if fault == "provider" && calls != 1 {
				t.Fatal("provider failure retried", calls)
			}
			if fault == "receipt" {
				if r.FailedStage != "receipt_write" {
					t.Fatal(r.FailedStage)
				}
			} else {
				raw, e := os.ReadFile(filepath.Join(dir, "receipt.json"))
				if e != nil {
					t.Fatal(e)
				}
				var saved FullLocalCampaignReceipt
				if json.Unmarshal(raw, &saved) != nil || saved.Status != "stopped" {
					t.Fatal("stopped receipt lost")
				}
			}
		})
	}
}
