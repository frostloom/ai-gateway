package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
)

func TestConversationAuthorityRealAgentReads(t *testing.T) {
	db, rdb, st := runtimeProfileTestDatabase(t)
	profiles := loadRuntimeProfileArchive(t)
	plans := seedRuntimeCatalogue(t, db, profiles)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	token := "local-authority-control"
	t.Setenv(controlplane.Env, token)
	billingServer := httptest.NewServer(admin.Handler(st, log, token))
	defer billingServer.Close()
	for _, tc := range []struct{ tool, message string }{
		{"list_plans", "有哪些套餐"}, {"get_my_subscription", "我当前订阅是什么"},
		{"get_recent_bills", "查最近三笔消费"}, {"get_consumption", "最近七天消耗是多少"},
	} {
		for _, mode := range []string{"model", "jev_selective"} {
			t.Run(tc.tool+"_"+mode, func(t *testing.T) {
				oracle := &agentConversationOracle{&storeConversationDouble{t: t, db: db, rdb: rdb, st: st, profiles: profiles, plans: plans}}
				var calls atomic.Int64
				// Echo the real tool result, not the independently captured oracle.
				llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var req struct {
						Messages []agent.Message `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						http.Error(w, "invalid local request", 400)
						return
					}
					message := localToolMessage(tc.tool, map[string]any{})
					if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool" {
						message = agent.Message{Role: "assistant", Content: req.Messages[len(req.Messages)-1].Content}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
				}))
				defer llm.Close()
				h := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClient(billingServer.URL)), agent.NewLLMClient(llm.URL, "local-only", "local-script"), st, rdb, log)
				if err := h.SetRoutingMode(mode); err != nil {
					t.Fatal(err)
				}
				if mode == "jev_selective" {
					h.SetJev(jev.NewFake(localJevScript(tc.tool, .99)))
				}
				h.SetAdminAccess(token, 0)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Header.Set("X-Agent-Internal", token)
					r.Header.Set("X-Agent-Tenant", fmt.Sprint(oracle.f.TenantID))
					h.ServeHTTP(w, r)
				})
				driver, err := NewHandlerConversationDriver(handler, oracle)
				if err != nil {
					t.Fatal(err)
				}
				c := ConversationCase{SourceID: "authority-local-" + tc.tool, Scenario: ConversationScenario{ID: "read", Profile: "active_standard", MeasurementScope: "complete_task", FullTaskMetricEligible: true, Turns: []ConversationRule{{User: tc.message, ExpectedActions: []string{"read"}, AllowedTools: []string{tc.tool}, BusinessStateUnchanged: true, NoWrite: true}}, Terminal: map[string]any{"business_state_unchanged": true, "authorized_write_count": float64(0)}}}
				r, facts := RunFactCheckedConversation(ctx, c, driver)
				if !r.StructuralPass || r.Status != "needs_review" || factMismatch(facts) {
					t.Fatalf("errors=%v facts=%+v", r.Errors, facts)
				}
				matches := 0
				for _, f := range facts {
					if f.Status == "match" {
						matches++
					}
				}
				if matches == 0 || r.Observations[0].State.AnswerAuthority == nil {
					t.Fatal("no independent facts were checked")
				}
				if mode == "model" && calls.Load() != 2 {
					t.Fatalf("local model attempts=%d", calls.Load())
				}
				if mode == "jev_selective" && tc.tool != "get_recent_bills" && calls.Load() != 0 {
					t.Fatal("eligible high-confidence read called model")
				}
				// A changed supported factual claim must fail the offline check.
				bad := r
				bad.Observations = append([]ConversationObservation(nil), r.Observations...)
				reply := bad.Observations[0].Turn.Response.Reply
				switch tc.tool {
				case "list_plans":
					reply = strings.Replace(reply, "月费 ¥", "月费 ¥9", 1)
				case "get_my_subscription":
					reply = strings.Replace(reply, "本期到 ", "本期到 1999-01-01 ", 1)
				case "get_recent_bills":
					reply = strings.Replace(reply, "合计扣费 ¥", "合计扣费 ¥9", 1)
				case "get_consumption":
					reply = strings.Replace(reply, "合计 ¥", "合计 ¥9", 1)
				}
				bad.Observations[0].Turn.Response.Reply = reply
				if !factMismatch(CheckConversationFacts(bad)) {
					t.Fatalf("corrupted reply accepted: %s", reply)
				}
				t.Logf("independent facts matched=%d; local model requests=%d; semantic review pending; external requests=0", matches, calls.Load())
			})
		}
	}
}
