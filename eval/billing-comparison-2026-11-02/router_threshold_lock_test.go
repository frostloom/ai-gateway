package comparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Characterizes the threshold write under a real InnoDB row lock; PASS exposes blocking.
func TestRouterThresholdDBLockBlocksOtherRoute(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("threshold-lock-%d", time.Now().UnixNano())
	a := store.Provider{Name: tag + "-a", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s-a"]`, tag), Weight: 1}
	b := store.Provider{Name: tag + "-b", BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s-b"]`, tag), Weight: 1}
	for _, p := range []*store.Provider{&a, &b} {
		if err := db.Create(p).Error; err != nil {
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 1
	reg.AutobanAfter = 20
	if p, err := reg.Route(tag + "-b"); err != nil || p == nil || p.ID != b.ID {
		t.Fatalf("warm=%+v err=%v", p, err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var locked store.Provider
	if err := tx.Raw("SELECT * FROM providers WHERE id=? FOR UPDATE", a.ID).Scan(&locked).Error; err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	name := tag + "-callback"
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		select {
		case entered <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(name)
	reportDone := make(chan struct{})
	go func() { defer close(reportDone); reg.Report(a.ID, false) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("report did not reach update")
	}
	select {
	case <-reportDone:
		t.Fatal("report returned despite locked row")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	routeDone := make(chan error, 1)
	go func() { _, err := reg.RouteExcludeContext(ctx, tag+"-b", nil); routeDone <- err }()
	select {
	case err := <-routeDone:
		t.Fatalf("route returned before lock release: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-reportDone:
	case <-time.After(3 * time.Second):
		t.Fatal("report failed to finish after unlock")
	}
	select {
	case err := <-routeDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("route error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("route failed to finish after unlock")
	}
	var after store.Provider
	if err := db.First(&after, a.ID).Error; err != nil || after.ConsecutiveFail != 1 {
		t.Fatalf("provider=%+v err=%v", after, err)
	}
	t.Log("threshold update blocked by locked provider; other cached provider route exceeded 50ms deadline until row release; then deadline exceeded")
}
