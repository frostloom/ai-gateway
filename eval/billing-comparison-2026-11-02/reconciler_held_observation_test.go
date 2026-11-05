package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestReconcilerStatusReportsHeldObservationTime(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("held-observe-%d", time.Now().UnixNano())
	defer db.Where("request_id LIKE ?", tag+"%").Delete(&store.Bill{})
	oldest := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i, when := range []time.Time{oldest, oldest.Add(10 * time.Minute), time.Now()} {
		bill := store.Bill{RequestID: fmt.Sprintf("%s-%d", tag, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 1, APIKeyID: 1, Model: "synthetic", Funding: "balance", CreatedAt: when}
		if err := db.Create(&bill).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := reconciler.New(db, nil, slog.New(slog.DiscardHandler), time.Minute)
	empty := httptest.NewRecorder()
	r.HTTPHandler().ServeHTTP(empty, httptest.NewRequest("GET", "/status", nil))
	var initial map[string]any
	if err := json.Unmarshal(empty.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial["last_observed_at"] != nil || initial["last_held_oldest_created_at"] != nil {
		t.Fatalf("initial status=%v", initial)
	}
	before := time.Now()
	s, err := r.SweepOnce(context.Background())
	if err != nil || s.Held != 2 {
		t.Fatalf("sweep=%+v err=%v", s, err)
	}
	r.RecordStats(s)
	rec := httptest.NewRecorder()
	r.HTTPHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, fmt.Sprint(status["last_observed_at"]))
	if err != nil || stamp.Before(before) || stamp.After(time.Now()) {
		t.Fatalf("observed=%v err=%v", status["last_observed_at"], err)
	}
	got, err := time.Parse(time.RFC3339Nano, fmt.Sprint(status["last_held_oldest_created_at"]))
	if err != nil || !got.Equal(oldest) {
		t.Fatalf("oldest=%v want=%v err=%v", status["last_held_oldest_created_at"], oldest, err)
	}
	if status["last_held"] != float64(2) {
		t.Fatalf("status=%v", status)
	}
	r.RecordStats(reconciler.Stats{})
	rec = httptest.NewRecorder()
	r.HTTPHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["last_held_oldest_created_at"] != nil || status["last_held"] != float64(0) {
		t.Fatalf("cleared status=%v", status)
	}
}
