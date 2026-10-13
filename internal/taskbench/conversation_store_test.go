package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only the dialogue/routing is a deterministic local double. All fixture,
// operation and final-state evidence below comes from the real store, an
// isolated MySQL database, and private Redis; no agent/model/JEV is started.
type storeConversationDouble struct {
	t        *testing.T
	db       *gorm.DB
	rdb      *redis.Client
	st       *store.Store
	profiles runtimeProfileArchive
	plans    map[string]store.Plan
	c        ConversationCase
	f        Fixture
	foreign  uint64
	index    int
	bound    *Pending
}

func (d *storeConversationDouble) snapshot(ctx context.Context) (ConversationState, error) {
	backend := &LiveBackend{db: d.db, rdb: d.rdb, st: d.st}
	own, err := backend.Snapshot(ctx, d.f, "")
	if err != nil {
		return ConversationState{}, err
	}
	other, err := backend.Snapshot(ctx, Fixture{TenantID: d.foreign}, "")
	if err != nil {
		return ConversationState{}, err
	}
	me, err := d.st.MeOverview(ctx, d.f.TenantID)
	if err != nil {
		return ConversationState{}, err
	}
	return ConversationState{Own: own, Allowance: me.Allowance, Controls: map[uint64]Snapshot{d.foreign: other}, CapturedAt: time.Now()}, nil
}
func (d *storeConversationDouble) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	d.c = c
	d.index = 0
	d.bound = nil
	d.f, d.foreign = instantiateRuntimeAccount(d.t, ctx, d.db, d.rdb, d.st, c.Scenario.Profile, d.profiles.Accounts[c.Scenario.Profile], d.plans, time.Now())
	snap, err := d.snapshot(ctx)
	return d.f, snap, err
}
func (d *storeConversationDouble) Final(ctx context.Context, _ Fixture, _ string) (ConversationState, error) {
	return d.snapshot(ctx)
}
func (d *storeConversationDouble) arguments() map[string]any {
	op := d.c.Scenario.Operation
	args := map[string]any{}
	if v, ok := op.Arguments["amount_money"]; ok {
		args["amount_money"] = v
	}
	if _, ok := op.Arguments["order_no"]; ok {
		args["order_no"] = d.f.OrderNo
	}
	if ref, ok := op.Arguments["plan_reference"].(string); ok && op.Tool != "cancel_subscription" {
		args["plan_id"] = int64(d.plans[ref].ID)
	}
	return args
}
func (d *storeConversationDouble) Step(ctx context.Context, _ Fixture, session, message string) (ConversationObservation, error) {
	rule := d.c.Scenario.Turns[d.index]
	d.index++
	if session == "" {
		session = "local-store-" + randomID()
	}
	obs := ConversationObservation{Turn: Turn{Status: 200, Message: message, Response: Response{SessionID: session}}}
	obs.Action = rule.ExpectedActions[0]
	switch {
	case rule.BoundPendingRequired:
		obs.Action = "execute"
		if d.bound == nil {
			return obs, fmt.Errorf("double has no pending operation")
		}
		op, args := d.bound.Tool, d.bound.Args
		var err error
		switch op {
		case "recharge":
			var order *store.RechargeOrder
			order, err = d.st.CreateRechargeOrder(ctx, d.f.TenantID, conversationInteger(args["amount_money"]), "conversation-"+randomID())
			if err == nil {
				_, err = d.st.PayRecharge(ctx, order.OrderNo)
			}
		case "refund_recharge":
			if no, ok := args["order_no"].(string); ok {
				_, err = d.st.RefundRecharge(ctx, d.f.TenantID, no)
			} else {
				_, err = d.st.RefundAmount(ctx, d.f.TenantID, conversationInteger(args["amount_money"]), "conversation-"+randomID())
			}
		case "subscribe_plan":
			_, err = d.st.SubscribePlan(ctx, d.f.TenantID, uint64(conversationInteger(args["plan_id"])))
		case "change_plan":
			err = d.st.ChangePlan(ctx, d.f.TenantID, uint64(conversationInteger(args["plan_id"])))
		case "cancel_subscription":
			_, err = d.st.CancelSubscription(ctx, d.f.TenantID)
		default:
			err = fmt.Errorf("unknown double operation")
		}
		if err != nil {
			return obs, err
		}
		raw, _ := json.Marshal(args)
		obs.Turn.Response.Trace = []Trace{{Tool: op, Args: string(raw), ResolvedArgs: string(raw), Confirmed: true, Phase: "execute", Outcome: "success"}}
		obs.Turn.Response.Reply = "本地路由替身：已执行 " + op
		d.bound = nil
	case rule.PendingRequired:
		obs.Action = "preview"
		d.bound = &Pending{Tool: d.c.Scenario.Operation.Tool, TenantID: d.f.TenantID, Args: d.arguments()}
		obs.Pending = d.bound
		obs.Turn.Response.Pending = true
		obs.Turn.Response.PendingTool = d.bound.Tool
		raw, _ := json.Marshal(d.bound.Args)
		obs.Turn.Response.Trace = []Trace{{Tool: d.bound.Tool, Args: string(raw), ResolvedArgs: string(raw), Phase: "preview", Outcome: "success"}}
		obs.Turn.Response.Reply = "本地路由替身：请确认 " + d.bound.Tool
	case rule.PendingCleared:
		obs.Action = "cancel"
		d.bound = nil
		obs.Turn.Response.Reply = "本地路由替身：操作已取消"
	case rule.NecessaryClarification:
		obs.Action = "clarify"
		obs.Turn.Response.Reply = "本地路由替身：请提供必要信息"
	default:
		obs.Turn.Response.Reply = "本地路由替身：不执行写操作"
		if obs.Action == "read" {
			found := false
			for _, tool := range rule.AllowedTools {
				if !isConversationWrite(tool) {
					obs.Turn.Response.Trace = append(obs.Turn.Response.Trace, Trace{Tool: tool, Phase: "read", Outcome: "success"})
					found = true
					break
				}
			}
			if !found && hasString(rule.ExpectedActions, "respond") {
				obs.Action = "respond"
			}
		}
	}
	obs.Turn.TelemetryRaw = json.RawMessage(`{"llm_calls":0,"jev_calls":0}`)
	var err error
	obs.State, err = d.snapshot(ctx)
	return obs, err
}

func TestConversationRealStoreBranches(t *testing.T) {
	suite, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	db, rdb, st := runtimeProfileTestDatabase(t)
	profiles := loadRuntimeProfileArchive(t)
	plans := seedRuntimeCatalogue(t, db, profiles)
	selections := []struct {
		tool     string
		branches []string
	}{
		{"recharge", []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm", "invalid_amount_followup"}},
		{"refund_recharge", []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm", "balance_cap", "owned_order_then_confirm", "invalid_amount_followup"}},
		{"subscribe_plan", []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm"}},
		{"change_plan", []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm"}},
		{"cancel_subscription", []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm", "no_subscription"}},
	}
	input := ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub"}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for _, selection := range selections {
		for _, branch := range selection.branches {
			var picked *ConversationCase
			for i := range suite.Cases {
				c := &suite.Cases[i]
				family := "recharge-"
				if selection.tool == "refund_recharge" {
					family = "refund-"
				}
				matches := c.Scenario.Operation != nil && c.Scenario.Operation.Tool == selection.tool
				if branch == "invalid_amount_followup" {
					matches = strings.HasPrefix(c.SourceID, family)
				}
				if c.Scenario.ID == branch && matches {
					picked = c
					break
				}
			}
			if picked == nil {
				t.Fatalf("missing branch %s/%s", selection.tool, branch)
			}
			t.Run(selection.tool+"/"+branch, func(t *testing.T) {
				driver := &storeConversationDouble{t: t, db: db, rdb: rdb, st: st, profiles: profiles, plans: plans}
				result := RunConversation(ctx, *picked, driver)
				if !result.StructuralPass || result.Status != "needs_review" || result.Success {
					t.Fatalf("real-store branch failed: %v status=%s", result.Errors, result.Status)
				}
				input.Episodes = append(input.Episodes, ConversationEpisode{Key: picked.Key(), Fixture: result.Fixture, Initial: result.Initial, Steps: result.Observations, Final: result.Final})
			})
		}
	}
	if t.Failed() {
		return
	}
	if len(input.Episodes) != 30 {
		t.Fatalf("branches=%d want30", len(input.Episodes))
	}
	report, err := ReplayConversations(suite, input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.StructuralPassed != 30 || report.Metrics.PendingReview != 30 || report.Metrics.CompletionRate != nil || report.ObservedLLMCalls == nil || *report.ObservedLLMCalls != 0 {
		t.Fatalf("bad replay counts: %+v", report.Metrics)
	}
	if dir := os.Getenv("CONVERSATION_LOCAL_ARTIFACT_DIR"); dir != "" {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(input, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "transcript.json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(append(raw, '\n'))
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
	t.Log("30 complete-task branches; real isolated store operations and control snapshots; deterministic dialogue double; answers remain unreviewed; zero model/JEV calls; database cleanup registered")
}
