package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestAuthorizedContinuation(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint("provider-failure=", failure), func(t *testing.T) {
			o := localCampaignOptions(t)
			a, p, b, e := authorizedEntryInputs(t)
			pilot, err := CheckAuthorizedPilot(a, p, b, e)
			if err != nil {
				t.Fatal(err)
			}
			d, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer d.close()
			source, _ := LoadConversations(o.DefinitionsDir)
			small, _ := PrepareSmallPair(source)
			var first, second ConversationCase
			for _, c := range small.Suite.Cases {
				if c.Key() == "balance-001/answer_complete" {
					first = c
				}
				if c.Key() == "balance-020/explicit_scope_followup" {
					second = c
				}
			}
			redirect := func(up http.RoundTripper) http.RoundTripper {
				return pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					clone := req.Clone(req.Context())
					u := *req.URL
					clone.URL = &u
					clone.URL.Host = "127.0.0.1"
					return up.RoundTrip(clone)
				})
			}
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: d.observer, Model: PairModelSettings{BaseURL: AuthorizedPilotModelBase, APIKey: "test", Model: AuthorizedPilotModel, Protocol: "anthropic"}, Upstream: redirect(d.model(first)), JEVFactory: func() jev.Evaluator { return authorizedTestJEV{d.judge(first), true} }}
			dir := filepath.Join(t.TempDir(), "first")
			r, err := RunAuthorizedFirstBatch(context.Background(), dir, opts)
			if err != nil {
				t.Fatal(err)
			}
			writeLocalCampaignJSON(filepath.Join(dir, "entry-outcome.json"), map[string]any{"schema": "paired-live-entry-outcome.v1", "status": "checkpoint_pending", "execution_receipt": "receipt.json", "cleanup_and_receipt_succeeded": true})
			review := map[string]any{"schema": "pi-first-live-independent-review.v1", "execution_revision": r.Revision, "journal_sha256": r.Report.JournalSHA256, "blocking_findings": []string{}, "rows": []any{}}
			rows := []any{}
			for _, row := range r.Report.Rows {
				qs := []any{}
				for _, q := range row.ReviewRequirements {
					raw, _ := json.Marshal(q)
					var v map[string]any
					json.Unmarshal(raw, &v)
					v["verdict"] = "passed"
					qs = append(qs, v)
				}
				rows = append(rows, map[string]any{"arm": row.Arm, "case_key": row.Key, "verdict": "passed", "requirements": qs})
			}
			review["rows"] = rows
			raw, _ := json.Marshal(review)
			c, err := CheckAuthorizedContinuation(pilot, dir, raw)
			if err != nil {
				t.Fatal(err)
			}
			if c.PreviousModelCalls() != 2 || c.PreviousJEVCalls() != 1 {
				t.Fatal("previous usage missing")
			}
			for _, bad := range []string{"journal_sha256", "execution_revision"} {
				copy := map[string]any{}
				json.Unmarshal(raw, &copy)
				copy[bad] = "wrong"
				broken, _ := json.Marshal(copy)
				if _, err := CheckAuthorizedContinuation(pilot, dir, broken); err == nil {
					t.Fatalf("accepted changed %s", bad)
				}
			}
			broken := append([]byte(nil), raw...)
			var v map[string]any
			json.Unmarshal(broken, &v)
			v["rows"] = []any{}
			broken, _ = json.Marshal(v)
			if _, err := CheckAuthorizedContinuation(pilot, dir, broken); err == nil {
				t.Fatal("missing reviews accepted")
			}
			for _, name := range []string{"budget.jsonl", "run.jsonl.jev.jsonl", "reservation.json"} {
				path := filepath.Join(dir, "campaign", "batch-001", name)
				saved, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, bad := range [][]byte{nil, []byte("{}\n"), saved[:len(saved)/2]} {
					os.WriteFile(path, bad, 0600)
					if _, err := CheckAuthorizedContinuation(pilot, dir, raw); err == nil {
						t.Fatalf("accepted corrupt accounting %s", name)
					}
				}
				os.WriteFile(path, saved, 0600)
			}
			opts.Upstream = redirect(d.model(second))
			opts.JEVFactory = func() jev.Evaluator { return authorizedTestJEV{d.judge(second), true} }
			if failure {
				opts.Upstream = pairedRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"test"}`))}, nil
				})
			}
			next := filepath.Join(t.TempDir(), "second")
			result, err := RunAuthorizedSecondBatch(context.Background(), next, opts, c)
			if failure {
				if err == nil || result.Status != "stopped" {
					t.Fatal("provider failure did not stop")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !failure && (result.Status != "checkpoint_pending" || result.Batches[1].Status != "checkpoint_pending" || result.Batches[2].Status != "not_started") {
				t.Fatalf("bad checkpoint: %+v", result)
			}
			if result.PreviousModelCalls != 2 || result.PreviousJEVAttempts != 1 || result.CampaignReservedModelCalls != 10 || result.CampaignReservedJEVCalls != 2 {
				t.Fatalf("lost cumulative budget: %+v", result)
			}
			if _, err := RunAuthorizedSecondBatch(context.Background(), filepath.Join(t.TempDir(), "repeat"), opts, c); err == nil {
				t.Fatal("replayed continuation")
			}
			if _, err := os.Stat(filepath.Join(dir, "campaign", "batch-003")); !os.IsNotExist(err) {
				t.Fatal("expanded scope")
			}
		})
	}
}
