package comparison

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestAuditRejectsConflictingEventID(t *testing.T) {
	if os.Getenv("TEST_AUDIT_CONFLICT") != "1" {
		t.Skip("isolated services only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	s := store.NewStore(db, r)
	ctx := context.Background()
	id := fmt.Sprintf("audit-conflict-%d", time.Now().UnixNano())
	first := store.AuditEvent{EventID: id, TenantID: 1, RequestID: id, Phase: "settle", AmountCents: 40, Model: "synthetic", Funding: "balance", Payload: `{"amount_cents":40}`, Source: "kafka", ConsumedAt: time.Now()}
	if err := s.UpsertAudit(ctx, &first); err != nil {
		t.Fatal(err)
	}
	replay := first
	replay.ID = 0
	replay.KafkaOffset = 10
	replay.ConsumedAt = time.Now()
	if err := s.UpsertAudit(ctx, &replay); err != nil {
		t.Fatalf("same business event with different delivery metadata: %v", err)
	}
	const n = 8
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			duplicate := replay
			duplicate.ID = 0
			errs <- s.UpsertAudit(ctx, &duplicate)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent same event: %v", err)
		}
	}
	for name, mutate := range map[string]func(*store.AuditEvent){
		"amount":  func(a *store.AuditEvent) { a.AmountCents = 90; a.Payload = `{"amount_cents":90}` },
		"tenant":  func(a *store.AuditEvent) { a.TenantID = 2 },
		"payload": func(a *store.AuditEvent) { a.Payload = `{"amount_cents":40,"unexpected":"changed"}` },
	} {
		changed := replay
		changed.ID = 0
		mutate(&changed)
		if err := s.UpsertAudit(ctx, &changed); err == nil {
			t.Fatalf("same event ID with different %s silently acknowledged", name)
		}
	}
	var rows []store.AuditEvent
	if err := db.Where("event_id=?", id).Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].AmountCents != 40 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}
