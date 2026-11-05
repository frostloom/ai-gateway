package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
)

func TestRouterSuccessParallelFailureWaits(t *testing.T) {
	if os.Getenv("TEST_ROUTER_SUCCESS_ASSERT") != "1" {
		t.Skip("explicit candidate assertion only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("success-parallel-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: `["test"]`, Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var writes atomic.Int32
	callback := fmt.Sprintf("eval:parallel-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			n := writes.Add(1)
			entered <- struct{}{}
			if n <= 2 {
				<-release
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(callback)
	done := make(chan error, 3)
	go func() { done <- reg.Report(p.ID, true) }()
	go func() { done <- reg.Report(p.ID, true) }()
	count := 0
	for count < 2 {
		select {
		case <-entered:
			count++
		case <-time.After(800 * time.Millisecond):
			close(release)
			for i := 0; i < 2; i++ {
				<-done
			}
			t.Fatalf("only %d successful reports entered persistence before release", count)
		}
	}
	go func() { done <- reg.Report(p.ID, false) }()
	select {
	case <-entered:
		close(release)
		for i := 0; i < 3; i++ {
			<-done
		}
		t.Fatal("failure entered while successes pending")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("report stuck")
		}
	}
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil || row.ConsecutiveFail != 1 {
		t.Fatal(row.ConsecutiveFail, err)
	}
}

func TestRouterSuccessParallelBlockedRowPoolPressure(t *testing.T) {
	if os.Getenv("TEST_ROUTER_POOL_PRESSURE") != "1" {
		t.Skip("isolated pool pressure test only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	p := store.Provider{Name: fmt.Sprintf("pool-pressure-%d", time.Now().UnixNano()), BaseURL: "http://127.0.0.1:1", Models: `["test"]`, Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	if err := lock.Exec("SELECT id FROM providers WHERE id=? FOR UPDATE", p.ID).Error; err != nil {
		lock.Rollback()
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	done := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() { done <- reg.Report(p.ID, true) }()
	}
	deadline := time.Now().Add(2 * time.Second)
	target := 2
	if os.Getenv("TEST_ROUTER_SUCCESS_PARALLEL") == "1" {
		target = 8
		if os.Getenv("TEST_ROUTER_SUCCESS_LIMIT") == "2" {
			target = 3
		}
	}
	for pool.Stats().InUse < target && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	before := pool.Stats()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	var one int
	queryErr := db.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error
	cancel()
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reports did not drain")
		}
	}
	if os.Getenv("TEST_ROUTER_SUCCESS_PARALLEL") == "1" && os.Getenv("TEST_ROUTER_SUCCESS_LIMIT") != "2" {
		if before.InUse != 8 || queryErr == nil {
			t.Fatalf("candidate expected saturated pool: inuse=%d err=%v", before.InUse, queryErr)
		}
	} else if before.InUse >= 8 || queryErr != nil || one != 1 {
		t.Fatalf("baseline expected query success: inuse=%d value=%d err=%v", before.InUse, one, queryErr)
	}
	t.Logf("inuse=%d max=%d unrelated_query_error=%v wait_count=%d", before.InUse, before.MaxOpenConnections, queryErr, before.WaitCount)
}

func TestRouterMultipleBlockedProvidersPoolPressure(t *testing.T) {
	if os.Getenv("TEST_ROUTER_MULTI_PRESSURE") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	var providers []store.Provider
	for i := 0; i < 4; i++ {
		p := store.Provider{Name: fmt.Sprintf("multi-block-%d-%d", time.Now().UnixNano(), i), BaseURL: "http://127.0.0.1:1", Models: `["test"]`, Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		providers = append(providers, p)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	for _, p := range providers {
		if err := lock.Exec("SELECT id FROM providers WHERE id=? FOR UPDATE", p.ID).Error; err != nil {
			lock.Rollback()
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	done := make(chan error, 8)
	for _, p := range providers {
		for j := 0; j < 2; j++ {
			id := p.ID
			go func() { done <- reg.Report(id, true) }()
		}
	}
	target := 5
	if os.Getenv("TEST_ROUTER_SUCCESS_PARALLEL") == "1" && os.Getenv("TEST_ROUTER_SUCCESS_LIMIT") == "2" && os.Getenv("TEST_ROUTER_REPORT_GLOBAL") != "4" {
		target = 8
	}
	deadline := time.Now().Add(2 * time.Second)
	for pool.Stats().InUse < target && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	before := pool.Stats()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	var one int
	queryErr := db.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error
	cancel()
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reports did not drain")
		}
	}
	if target == 8 {
		if before.InUse != 8 || queryErr == nil {
			t.Fatalf("bounded candidate: inuse=%d err=%v", before.InUse, queryErr)
		}
	} else if before.InUse >= 8 || queryErr != nil || one != 1 {
		t.Fatalf("baseline: inuse=%d value=%d err=%v", before.InUse, one, queryErr)
	}
	t.Logf("providers=4 inuse=%d max=%d unrelated_query_error=%v", before.InUse, before.MaxOpenConnections, queryErr)
}

func TestRouterFailureReportsGlobalPoolPressure(t *testing.T) {
	if os.Getenv("TEST_ROUTER_FAILURE_PRESSURE") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	var providers []store.Provider
	for i := 0; i < 8; i++ {
		p := store.Provider{Name: fmt.Sprintf("fail-block-%d-%d", time.Now().UnixNano(), i), BaseURL: "http://127.0.0.1:1", Models: `["test"]`, Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		providers = append(providers, p)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	for _, p := range providers {
		if err := lock.Exec("SELECT id FROM providers WHERE id=? FOR UPDATE", p.ID).Error; err != nil {
			lock.Rollback()
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	done := make(chan error, 8)
	for _, p := range providers {
		id := p.ID
		go func() { done <- reg.Report(id, false) }()
	}
	capped := os.Getenv("TEST_ROUTER_CAP_FAILURES") == "1"
	target := 8
	if capped {
		target = 5
	}
	deadline := time.Now().Add(2 * time.Second)
	for pool.Stats().InUse < target && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	before := pool.Stats()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	var one int
	queryErr := db.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error
	cancel()
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reports did not drain")
		}
	}
	if capped {
		if before.InUse != 5 || queryErr != nil || one != 1 {
			t.Fatalf("capped inuse=%d value=%d err=%v", before.InUse, one, queryErr)
		}
	} else if before.InUse != 8 || queryErr == nil {
		t.Fatalf("uncapped inuse=%d err=%v", before.InUse, queryErr)
	}
	var failures int64
	if err := db.Model(&store.Provider{}).Where("id IN ? AND consecutive_fail=1", func() []uint64 {
		ids := make([]uint64, 0, len(providers))
		for _, p := range providers {
			ids = append(ids, p.ID)
		}
		return ids
	}()).Count(&failures).Error; err != nil || failures != 8 {
		t.Fatal(failures, err)
	}
	t.Logf("capped=%t inuse=%d max=%d unrelated_query_error=%v persisted_failures=%d", capped, before.InUse, before.MaxOpenConnections, queryErr, failures)
}

func TestRouterGlobalCapDelaysFailureState(t *testing.T) {
	if os.Getenv("TEST_ROUTER_FAILURE_DELAY") != "1" {
		t.Skip("isolated characterization")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	var providers []store.Provider
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("delay-fail-%d-%d", time.Now().UnixNano(), i)
		p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		providers = append(providers, p)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	pool.SetMaxIdleConns(12)
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	for _, p := range providers[:4] {
		if err := lock.Exec("SELECT id FROM providers WHERE id=? FOR UPDATE", p.ID).Error; err != nil {
			lock.Rollback()
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 1
	reg.Cooldown = time.Hour
	done := make(chan error, 5)
	for _, p := range providers[:4] {
		id := p.ID
		go func() { done <- reg.Report(id, true) }()
	}
	deadline := time.Now().Add(3 * time.Second)
	for pool.Stats().InUse < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pool.Stats().InUse < 5 {
		lock.Rollback()
		t.Fatal("blocked writes not ready")
	}
	failID := providers[4].ID
	go func() { done <- reg.Report(failID, false) }()
	deadline = time.Now().Add(2 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		for _, state := range reg.States() {
			if state.ProviderID == failID {
				seen = true
			}
		}
		if seen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		lock.Rollback()
		t.Fatal("failure not admitted to registry")
	}
	if os.Getenv("TEST_ROUTER_PUBLISH_BEFORE_CAP") == "1" {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			open := false
			for _, state := range reg.States() {
				if state.ProviderID == failID && state.State == "open" {
					open = true
				}
			}
			if open {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	picked, routeErr := reg.Route(providers[4].Name)
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reports stuck")
		}
	}
	if os.Getenv("TEST_ROUTER_PUBLISH_BEFORE_CAP") == "1" {
		if routeErr != nil || picked != nil {
			t.Fatalf("expected failed provider excluded, got=%v err=%v", picked, routeErr)
		}
	} else if routeErr != nil || picked == nil || picked.ID != failID {
		t.Fatalf("expected delayed failure while global slots held, got=%v err=%v", picked, routeErr)
	}
	final, err := reg.Route(providers[4].Name)
	if err != nil || final != nil {
		t.Fatalf("after release expected open: got=%v err=%v", final, err)
	}
	t.Logf("publish_before_cap=%s selected_while_blocked=%t", os.Getenv("TEST_ROUTER_PUBLISH_BEFORE_CAP"), picked != nil)
}

func TestRouterGlobalCapDefersCrossInstanceBan(t *testing.T) {
	if os.Getenv("TEST_ROUTER_BAN_DELAY") != "1" {
		t.Skip("isolated characterization")
	}
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	var providers []store.Provider
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("ban-delay-%d-%d", time.Now().UnixNano(), i)
		p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		providers = append(providers, p)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	pool.SetMaxIdleConns(12)
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	for _, p := range providers[:4] {
		if err := lock.Exec("SELECT id FROM providers WHERE id=? FOR UPDATE", p.ID).Error; err != nil {
			lock.Rollback()
			t.Fatal(err)
		}
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.AutobanAfter = 1
	reg.FailThreshold = 1
	done := make(chan error, 5)
	for _, p := range providers[:4] {
		id := p.ID
		go func() { done <- reg.Report(id, true) }()
	}
	deadline := time.Now().Add(3 * time.Second)
	for pool.Stats().InUse < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pool.Stats().InUse < 5 {
		lock.Rollback()
		t.Fatal("blocked writes not ready")
	}
	failID := providers[4].ID
	go func() { done <- reg.Report(failID, false) }()
	deadline = time.Now().Add(2 * time.Second)
	open := false
	for time.Now().Before(deadline) {
		for _, state := range reg.States() {
			if state.ProviderID == failID && state.State == "open" {
				open = true
			}
		}
		if open {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !open {
		lock.Rollback()
		t.Fatal("local ban not visible")
	}
	observer := router.New(db, slog.New(slog.DiscardHandler))
	picked, routeErr := observer.Route(providers[4].Name)
	var before store.Provider
	readErr := db.First(&before, failID).Error
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reports stuck")
		}
	}
	var after store.Provider
	if err := db.First(&after, failID).Error; err != nil {
		t.Fatal(err)
	}
	if routeErr != nil || readErr != nil || picked == nil || picked.ID != failID || before.Status != 0 || after.Status != 2 {
		t.Fatalf("route=%v routeErr=%v readErr=%v status_before=%d status_after=%d", picked, routeErr, readErr, before.Status, after.Status)
	}
	t.Log("local breaker open while fresh second Registry sees active DB row; ban persists only after write slots released")
}
