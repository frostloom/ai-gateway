package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type pairedStoreOracle struct {
	env     *PairedFixtureEnvironment
	f       Fixture
	foreign uint64
}

func (o *pairedStoreOracle) Setup(ctx context.Context, c ConversationCase) (Fixture, ConversationState, error) {
	p, ok := o.env.profiles.archive.Accounts[c.Scenario.Profile]
	if !ok {
		return Fixture{}, ConversationState{}, fmt.Errorf("unknown runtime profile")
	}
	f, foreign, err := instantiatePairedAccount(ctx, o.env.db, o.env.rdb, o.env.st, p, o.env.plans, time.Now())
	o.f, o.foreign = f, foreign
	if err != nil {
		return f, ConversationState{}, err
	}
	s, err := o.State(ctx, f, "")
	return f, s, err
}
func (o *pairedStoreOracle) State(ctx context.Context, f Fixture, _ string) (ConversationState, error) {
	s := ConversationState{CapturedAt: time.Now()}
	if f.TenantID != o.f.TenantID || f.TenantID == 0 || o.foreign == 0 {
		return s, fmt.Errorf("fixture identity mismatch")
	}
	b := &LiveBackend{db: o.env.db, rdb: o.env.rdb, st: o.env.st}
	var err error
	s.Own, err = b.Snapshot(ctx, f, "")
	if err != nil {
		return s, err
	}
	other, err := b.Snapshot(ctx, Fixture{TenantID: o.foreign}, "")
	if err != nil {
		return s, err
	}
	s.Controls = map[uint64]Snapshot{o.foreign: other}
	me, err := o.env.st.MeOverview(ctx, f.TenantID)
	if err != nil {
		return s, err
	}
	s.Allowance = me.Allowance
	s.AnswerAuthority, err = CaptureConversationAnswerAuthority(ctx, o.env.st, f.TenantID, s.CapturedAt)
	return s, err
}
func (o *pairedStoreOracle) Pending(ctx context.Context, f Fixture, session string) (*Pending, error) {
	if f.TenantID != o.f.TenantID {
		return nil, fmt.Errorf("fixture identity mismatch")
	}
	if session == "" {
		return nil, nil
	}
	raw, err := o.env.rdb.Get(ctx, "agent:session:"+session).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("recorded server session missing")
	}
	if err != nil {
		return nil, err
	}
	var state struct {
		Pending *Pending `json:"pending_action"`
	}
	err = json.Unmarshal(raw, &state)
	return state.Pending, err
}
