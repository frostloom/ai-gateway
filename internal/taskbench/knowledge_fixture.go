package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"io"
	"log/slog"
	"net/http/httptest"
	"time"
)

// KnowledgeFixtureAccess is an owned task in a randomly created fixture DB and
// private Redis. It exposes no path to attach to an application database.
type KnowledgeFixtureAccess struct {
	Store                    *store.Store
	Redis                    *redis.Client
	Tenant                   uint64
	Session, Token, AdminURL string
	env                      *PairedFixtureEnvironment
	oracle                   *pairedStoreOracle
	server                   *httptest.Server
}

func (e *PairedFixtureEnvironment) NewKnowledgeTask(ctx context.Context, pending bool, clock time.Time) (*KnowledgeFixtureAccess, error) {
	if e == nil || e.closed || clock.IsZero() {
		return nil, fmt.Errorf("owned environment required")
	}
	p := e.profiles.archive.Accounts["active_standard"]
	p.Order.AmountMoney = 27
	p.Order.AmountCent = 2700
	p.Balance = 2500
	f, foreign, err := instantiatePairedAccount(ctx, e.db, e.rdb, e.st, p, e.plans, clock)
	if err != nil {
		return nil, err
	}
	a := &KnowledgeFixtureAccess{Store: e.st, Redis: e.rdb, Tenant: f.TenantID, Session: "knowledge-" + randomID(), Token: "knowledge-" + randomID(), env: e, oracle: &pairedStoreOracle{env: e, f: f, foreign: foreign}}
	a.server = httptest.NewServer(admin.Handler(e.st, slog.New(slog.NewTextHandler(io.Discard, nil)), a.Token))
	a.AdminURL = a.server.URL
	state := map[string]any{"history": []any{}}
	if pending {
		state["pending_action"] = map[string]any{"tool": "recharge", "args": map[string]any{"amount_money": 100}, "preview": "向本人账户充值100元，尚未执行。", "idem_key": "knowledge-pending-" + randomID(), "tenant_id": f.TenantID}
	}
	raw, _ := json.Marshal(state)
	if err = e.rdb.Set(ctx, "agent:session:"+a.Session, raw, 30*time.Minute).Err(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
func (a *KnowledgeFixtureAccess) Snapshot(ctx context.Context) (json.RawMessage, json.RawMessage, bool, error) {
	state, err := a.oracle.State(ctx, a.oracle.f, a.Session)
	if err != nil {
		return nil, nil, false, err
	}
	p, err := a.oracle.Pending(ctx, a.oracle.f, a.Session)
	if err != nil {
		return nil, nil, false, err
	}
	full, err := json.Marshal(state)
	if err != nil {
		return nil, nil, false, err
	}
	// Audits and capture timestamps are evidence, not changes in business state.
	state.Own.Audits = nil
	state.CapturedAt = time.Time{}
	if state.AnswerAuthority != nil {
		state.AnswerAuthority.CapturedAt = time.Time{}
	}
	for k, s := range state.Controls {
		s.Audits = nil
		state.Controls[k] = s
	}
	business, err := json.Marshal(state)
	return full, business, p != nil, err
}
func (a *KnowledgeFixtureAccess) Close() {
	if a != nil && a.server != nil {
		a.server.Close()
		a.server = nil
	}
}
