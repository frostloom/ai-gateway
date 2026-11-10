package store

import (
	"context"
	"testing"
	"time"
)

func TestConsumptionCalendarRangeMatchesModelTotals(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	tenant := seedTenant(t, s, 0)
	seedTierModel(t, s, "calendar-usage", `[]`)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -6)
	for i, created := range []time.Time{start.Add(-time.Hour), start.Add(time.Hour)} {
		amount := int64(100)
		row := Bill{RequestID: []string{"calendar-old", "calendar-current"}[i], Phase: PhaseSettle, Status: StatusSettled, TenantID: tenant, Model: "calendar-usage", ActualQuota: &amount, CreatedAt: created}
		if err := s.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	daily, err := s.ConsumptionStats(context.Background(), tenant, 7)
	if err != nil {
		t.Fatal(err)
	}
	models, err := s.ConsumptionByModel(context.Background(), tenant, 7)
	if err != nil {
		t.Fatal(err)
	}
	var dayTotal, modelTotal int64
	for _, d := range daily {
		dayTotal += d.Tokens
	}
	for _, m := range models {
		modelTotal += m.CostCent
	}
	if dayTotal != 100 || modelTotal != 100 {
		t.Fatalf("calendar totals daily%d models%d want100", dayTotal, modelTotal)
	}
}

func TestConsumptionRangeBound(t *testing.T) {
	_, days := consumptionRange(1000000000)
	if days != 90 {
		t.Fatalf("days=%d", days)
	}
	_, days = consumptionRange(-1)
	if days != 7 {
		t.Fatalf("default days=%d", days)
	}
}
