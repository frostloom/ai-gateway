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
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouterEventReplayWithClientFoundRows(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENTS") != "1" {
		t.Skip("isolated only")
	}
	dsn := os.Getenv("COMPARISON_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("isolated DSN missing")
	}
	db, err := gorm.Open(mysql.Open(dsn+"&clientFoundRows=true"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("found-rows-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	first := store.Provider{Name: id + "-first-race", BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	const racers = 16
	ready := make(chan struct{})
	firstErrs := make(chan error, racers)
	var firstWG sync.WaitGroup
	for i := 0; i < racers; i++ {
		firstWG.Add(1)
		go func() {
			defer firstWG.Done()
			<-ready
			firstErrs <- testRouterApply(context.Background(), db, id+"-first-race", first.ID, false, false)
		}()
	}
	close(ready)
	firstWG.Wait()
	close(firstErrs)
	for err := range firstErrs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var firstResult store.Provider
	var firstEvents int64
	if err := db.First(&firstResult, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&routerTestEvent{}).Where("provider_id=?", first.ID).Count(&firstEvents).Error; err != nil {
		t.Fatal(err)
	}
	if firstResult.ConsecutiveFail != 1 || firstEvents != 1 {
		t.Fatalf("first concurrent submissions failures=%d events=%d", firstResult.ConsecutiveFail, firstEvents)
	}
	for i := 0; i < 2; i++ {
		if err := testRouterApply(context.Background(), db, id, p.ID, false, false); err != nil {
			t.Fatal(err)
		}
	}
	const concurrent = 16
	start := make(chan struct{})
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- testRouterApply(context.Background(), db, id, p.ID, false, false)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := testRouterApply(context.Background(), db, id, p.ID, true, false); !errors.Is(err, errRouterConflict) {
		t.Fatalf("different payload must conflict: %v", err)
	}
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	var events int64
	if err := db.Model(&routerTestEvent{}).Where("provider_id=?", p.ID).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if got.ConsecutiveFail != 1 || events != 1 {
		t.Fatalf("replay must apply once: failures=%d events=%d", got.ConsecutiveFail, events)
	}
	t.Logf("clientFoundRows=true same-event replay failures=%d events=%d", got.ConsecutiveFail, events)
}
