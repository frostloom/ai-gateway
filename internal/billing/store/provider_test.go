package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// M10 渠道管理单测：UpsertProvider 按 name 幂等；ListProvidersWithMargin 售价/成本/毛利口径正确。

func TestUpsertProviderIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	p := &Provider{Name: "ch-test", BaseURL: "https://u1", UpstreamKey: "sk-a", Models: `["deepseek-chat"]`, Weight: 1, CostInCent: 100, CostOutCent: 200}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatalf("upsert #1: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("insert should populate provider ID")
	}

	// 同名 upsert：更新转发参数/成本，不新增行
	p2 := &Provider{Name: "ch-test", BaseURL: "https://u2", UpstreamKey: "sk-b", Models: `["*"]`, Weight: 3, CostInCent: 50, CostOutCent: 90, Status: 1}
	if err := s.UpsertProvider(ctx, p2); err != nil {
		t.Fatalf("upsert #2: %v", err)
	}

	var count int64
	if err := s.db.Model(&Provider{}).Where("name = ?", "ch-test").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}

	var got Provider
	if err := s.db.Where("name = ?", "ch-test").First(&got).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("id changed: %d != %d", got.ID, p.ID)
	}
	if got.BaseURL != "https://u2" || got.UpstreamKey != "sk-b" || got.CostInCent != 50 || got.CostOutCent != 90 || got.Weight != 3 || got.Status != 1 {
		t.Fatalf("update fields not applied: %+v", got)
	}
}

func TestListProvidersWithMargin(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	tid := seedTenant(t, s, 1_000)
	// 成本 分/百万 token：in=100 out=200
	p := &Provider{Name: "ch-margin", BaseURL: "https://u", Models: `["deepseek-chat"]`, CostInCent: 100, CostOutCent: 200}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	pid := p.ID

	now := time.Now()
	inToks, outToks := int64(500_000), int64(250_000)
	// 两条 settle 账单：售价 Σ = 150 + 250 = 400 分
	// 成本 = (500000+1000000)×100/1e6 + 250000×200/1e6 = 150 + 50 = 200 分
	bills := []struct {
		actual int64
		in     int64
		out    int64
	}{
		{150, inToks, outToks},
		{250, 1_000_000, 0},
	}
	for i, bb := range bills {
		actual, in, out := bb.actual, bb.in, bb.out
		b := Bill{
			RequestID: fmt.Sprintf("m10-margin-%d", i), Phase: PhaseSettle, Status: StatusSettled,
			TenantID: tid, APIKeyID: 0, ProviderID: &pid, Model: "deepseek-chat",
			ActualQuota: &actual, PromptTokens: &in, CompletionTokens: &out,
			CreatedAt: now,
		}
		if err := s.db.Create(&b).Error; err != nil {
			t.Fatalf("insert bill: %v", err)
		}
	}

	rows, err := s.ListProvidersWithMargin(ctx)
	if err != nil {
		t.Fatalf("list margin: %v", err)
	}
	var got *ProviderMargin
	for i := range rows {
		if rows[i].ID == pid {
			got = &rows[i]
			break
		}
	}
	if got == nil {
		t.Fatal("provider not in list")
	}
	if got.SellingCent != 400 || got.CostCent != 200 || got.MarginCent != 200 {
		t.Fatalf("margin wrong: selling=%d cost=%d margin=%d, want 400/200/200", got.SellingCent, got.CostCent, got.MarginCent)
	}
}
