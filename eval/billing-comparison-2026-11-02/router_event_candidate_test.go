package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type routerTestEvent struct {
	ID         string `gorm:"primaryKey;size:100"`
	ProviderID uint64
	OK         bool
}

func (routerTestEvent) TableName() string { return "eval_router_events" }

var errRouterConflict = errors.New("event conflict")
var errRouterInjected = errors.New("injected failure")

func testRouterApply(ctx context.Context, db *gorm.DB, id string, provider uint64, ok, fail bool) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		event := routerTestEvent{ID: id, ProviderID: provider, OK: ok}
		inserted := tx.Create(&event)
		if inserted.Error != nil {
			var duplicate *mysqldriver.MySQLError
			if !errors.As(inserted.Error, &duplicate) || duplicate.Number != 1062 {
				return inserted.Error
			}
			var prior routerTestEvent
			if err := tx.Where("id = ?", id).First(&prior).Error; err != nil {
				return err
			}
			if prior.ProviderID != provider || prior.OK != ok {
				return errRouterConflict
			}
			return nil
		}
		var p store.Provider
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, provider).Error; err != nil {
			return err
		}
		// Retain the event but do not mutate non-active provider rows.
		if p.Status != 0 {
			return nil
		}
		count := p.ConsecutiveFail
		status := p.Status
		if ok {
			count = 0
		} else {
			count++
			if count >= 20 && status == 0 {
				status = 2
			}
		}
		if err := tx.Model(&p).Updates(map[string]any{"consecutive_fail": count, "status": status}).Error; err != nil {
			return err
		}
		if fail {
			return errRouterInjected
		}
		return nil
	})
}

func TestRouterTransactionalEvents(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENTS") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("event-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const count = 20
	start := make(chan struct{})
	errs := make(chan error, count*2)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		event := fmt.Sprintf("%s-%d", id, i)
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; errs <- testRouterApply(ctx, db, event, p.ID, false, false) }()
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&routerTestEvent{}).Where("provider_id=?", p.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 20 || got.ConsecutiveFail != 20 || got.Status != 2 {
		t.Fatalf("events=%d failures=%d status=%d", n, got.ConsecutiveFail, got.Status)
	}
	if err := testRouterApply(ctx, db, id+"-0", p.ID, true, false); !errors.Is(err, errRouterConflict) {
		t.Fatalf("mismatched duplicate=%v", err)
	}
	rollbackProvider := store.Provider{Name: id + "-rollback-provider", BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1, ConsecutiveFail: 3}
	if err := db.Create(&rollbackProvider).Error; err != nil {
		t.Fatal(err)
	}
	if err := testRouterApply(ctx, db, id+"-rollback", rollbackProvider.ID, true, true); !errors.Is(err, errRouterInjected) {
		t.Fatalf("rollback=%v", err)
	}
	if err := db.Model(&routerTestEvent{}).Where("id=?", id+"-rollback").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("rollback count=%d err=%v", n, err)
	}
	var rollbackResult store.Provider
	if err := db.First(&rollbackResult, rollbackProvider.ID).Error; err != nil || rollbackResult.ConsecutiveFail != 3 || rollbackResult.Status != 0 {
		t.Fatalf("rollback provider=%+v err=%v", rollbackResult, err)
	}
	t.Logf("events=20 concurrent_submissions=40 failures=%d status=%d replay_conflict_and_rollback=true", got.ConsecutiveFail, got.Status)
}

func TestRouterEventDisabledProvider(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENTS") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("event-disabled-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1, Status: 1, ConsecutiveFail: 7}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, e := range []struct {
		suffix string
		ok     bool
	}{{"failure", false}, {"success", true}} {
		eventID := id + e.suffix
		if err := testRouterApply(ctx, db, eventID, p.ID, e.ok, false); err != nil {
			t.Fatal(err)
		}
		var got store.Provider
		if err := db.First(&got, p.ID).Error; err != nil || got.Status != 1 || got.ConsecutiveFail != 7 {
			t.Fatalf("event=%s status=%d count=%d err=%v", e.suffix, got.Status, got.ConsecutiveFail, err)
		}
	}
	if err := db.Model(&store.Provider{}).Where("id=?", p.ID).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := testRouterApply(ctx, db, id+"failure", p.ID, false, false); err != nil {
		t.Fatal(err)
	}
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil || got.ConsecutiveFail != 7 {
		t.Fatalf("replay after enable provider=%+v err=%v", got, err)
	}
	var n int64
	if err := db.Model(&routerTestEvent{}).Where("provider_id=?", p.ID).Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("events=%d err=%v", n, err)
	}
	t.Logf("disabled_reports_preserved_count=7 retained_events=%d replay_after_enable_noop=true", n)
}
