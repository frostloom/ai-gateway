package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

type authorizedTestJEV struct {
	inner jev.Evaluator
	usage bool
}

func (j authorizedTestJEV) Evaluate(ctx context.Context, s any, q map[string]jev.Question) (*jev.Result, error) {
	r, err := j.inner.Evaluate(ctx, s, q)
	if err == nil {
		r.Model = AuthorizedPilotJEVModel
		body := map[string]any{"model": r.Model, "answers": r.Answers}
		if j.usage {
			body["usage"] = map[string]int{"input_tokens": 100, "output_tokens": 0}
		}
		raw, _ := json.Marshal(body)
		r.Raw = string(raw)
	}
	return r, err
}

func TestAuthorizedFirstBatchRequiresEvidenceBeforeSideEffects(t *testing.T) {
	a, p, _, _ := authorizedEntryInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "never-created")
	_, err = RunAuthorizedFirstBatch(context.Background(), dir, AuthorizedFirstBatchOptions{Pilot: pilot})
	if err == nil {
		t.Fatal("unverified evidence allowed execution")
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("rejection created execution output")
	}
}

// The real handler, fixture ownership, guards, reports and observer all execute;
// only external inference is replaced, with exact pinned endpoint assertions.
func TestAuthorizedFirstBatchOneShotAndFailureStop(t *testing.T) {
	o := localCampaignOptions(t)
	a, p, b, e := authorizedEntryInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"complete", "model-http", "jev-usage", "receipt-write"} {
		t.Run(kind, func(t *testing.T) {
			d, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer d.close()
			dir := filepath.Join(t.TempDir(), "run")
			n := 0
			source, err := LoadConversations(o.DefinitionsDir)
			if err != nil {
				t.Fatal(err)
			}
			small, err := PrepareSmallPair(source)
			if err != nil {
				t.Fatal(err)
			}
			var first ConversationCase
			for _, c := range small.Suite.Cases {
				if c.Key() == "balance-001/answer_complete" {
					first = c
				}
			}
			upstream := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				n++
				if req.URL.Host != "maas.qianwenaiapi.com" || req.URL.Path != "/apps/anthropic/v1/messages" {
					t.Fatalf("wrong live endpoint: %s", req.URL)
				}
				if kind == "receipt-write" {
					if err := os.Mkdir(filepath.Join(dir, "receipt.json"), 0700); err != nil && !os.IsExist(err) {
						t.Fatal(err)
					}
				}
				if kind == "model-http" {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"private-key-must-not-be-archived"}`))}, nil
				}
				clone := req.Clone(req.Context())
				u := *req.URL
				clone.URL = &u
				clone.URL.Host = "127.0.0.1"
				return d.model(first).RoundTrip(clone)
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: d.observer, Model: PairModelSettings{BaseURL: AuthorizedPilotModelBase, APIKey: "test-private", Model: AuthorizedPilotModel, Protocol: "anthropic"}, Upstream: upstream, JEVFactory: func() jev.Evaluator { return authorizedTestJEV{d.judge(first), kind != "jev-usage"} }}
			receipt, err := RunAuthorizedFirstBatch(context.Background(), dir, opts)
			if kind == "complete" {
				if err != nil || receipt.Status != "checkpoint_pending" || receipt.ModelState.Calls != 2 || receipt.JEVAttempts != 1 || n != 2 {
					raw, _ := json.Marshal(receipt.Report)
					t.Fatalf("first batch: %s %v calls=%d", raw, err, n)
				}
				if receipt.Report == nil || receipt.Report.FinishedArms != 2 || receipt.Report.Rows[0].Status != "needs_review" {
					t.Fatal("invented quality success")
				}
			} else if err == nil || receipt.Status != "stopped" {
				t.Fatalf("failure accepted: %+v %v", receipt, err)
			}
			if len(receipt.Batches) != 6 || receipt.Batches[1].Status != "not_started" {
				t.Fatal("expanded past first batch")
			}
			if _, err := os.Stat(filepath.Join(dir, "campaign", "batch-002")); !os.IsNotExist(err) {
				t.Fatal("second batch reserved")
			}
			if kind != "receipt-write" {
				raw, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(raw), "test-private") || strings.Contains(string(raw), "private-key-must-not-be-archived") {
					t.Fatal("receipt leaked private error")
				}
				var stored AuthorizedFirstBatchReceipt
				if json.Unmarshal(raw, &stored) != nil || stored.Status != receipt.Status {
					t.Fatal("receipt missing durable outcome")
				}
			}
			before := n
			if _, err := RunAuthorizedFirstBatch(context.Background(), dir, opts); err == nil || n != before {
				t.Fatal("existing directory repeated provider")
			}
		})
	}
}
