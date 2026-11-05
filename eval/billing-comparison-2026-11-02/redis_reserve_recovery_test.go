package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestRedisStopThenRetrySameReservation(t *testing.T) {
	name := os.Getenv("COMPARISON_REDIS_CONTAINER")
	if !strings.HasPrefix(name, "auth-cache-test-redis-") {
		t.Skip("requires harness-owned Redis")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("redis-retry-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	out, err := exec.Command("docker", "stop", "--time", "2", name).CombinedOutput()
	if err != nil {
		t.Fatalf("stop: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "start", name).Run() })
	outage, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = s.ReserveWithReplaySignal(outage, req)
	cancel()
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected unavailable got %v", err)
	}
	var before int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&before).Error; err != nil || before != 0 {
		t.Fatal(before, err)
	}
	out, err = exec.Command("docker", "start", name).CombinedOutput()
	if err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	addr, err := exec.Command("docker", "port", name, "6379/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimSpace(string(addr))
	if !strings.HasPrefix(address, "127.0.0.1:") || strings.Contains(address, "\n") {
		t.Fatal(address)
	}
	// Docker may assign a new host port after restart; later -count runs in
	// this same process must connect to that new endpoint.
	if err := os.Setenv("COMPARISON_REDIS_ADDR", address); err != nil {
		t.Fatal(err)
	}
	restored := redis.NewClient(&redis.Options{Addr: address, DialTimeout: 300 * time.Millisecond, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, MaxRetries: -1})
	defer restored.Close()
	deadline := time.Now().Add(10 * time.Second)
	for restored.Ping(ctx).Err() != nil {
		if time.Now().After(deadline) {
			t.Fatal("redis restart timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	s = store.NewStore(db, restored)
	if _, err := s.RebuildBalance(ctx, tenant.ID); err != nil {
		t.Fatal(err)
	}
	first, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	repeat, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || repeat.Code != pb.ReserveCode_RESERVE_DUPLICATE || repeat.BillId != first.BillId {
		t.Fatal(repeat, err)
	}
	balance, err := restored.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || balance != 900 || ledger != balance {
		t.Fatal(balance, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("outage=Unavailable bills_during_outage=%d retry=%s repeat=%s balance=%d ledger=%d events=%d", before, first.Code, repeat.Code, balance, ledger, events)
}
