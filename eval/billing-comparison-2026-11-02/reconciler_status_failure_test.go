package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type failedReconcileSettle struct{ pb.BillingServiceClient }

func (failedReconcileSettle) Settle(context.Context, *pb.SettleRequest, ...grpc.CallOption) (*pb.SettleResponse, error) {
	return nil, errors.New("synthetic settle outage")
}

func (failedReconcileSettle) Reverse(context.Context, *pb.ReverseRequest, ...grpc.CallOption) (*pb.ReverseResponse, error) {
	return nil, errors.New("synthetic reverse outage")
}
func (failedReconcileSettle) RebuildBalance(context.Context, *pb.RebuildBalanceRequest, ...grpc.CallOption) (*pb.RebuildBalanceResponse, error) {
	return nil, errors.New("synthetic rebuild outage")
}

func TestReconcilerStatusReportsFailures(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("recon-status-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: res.BillId, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	run := func(runner *reconciler.Reconciler) map[string]any {
		t.Helper()
		c, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); runner.Run(c, time.Hour, false, true) }()
		defer func() { cancel(); <-done }()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			w := httptest.NewRecorder()
			runner.HTTPHandler().ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["last_sweep_attempted"] == true {
				return body
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("status never reports attempted sweep")
		return nil
	}
	stat := run(reconciler.New(db, failedReconcileSettle{}, slog.New(slog.DiscardHandler), 10*time.Minute))
	if stat["last_sweep_error"] != false || stat["last_settle_errors"] != float64(1) || stat["last_scanned"] != float64(1) {
		t.Errorf("settle outage status: %+v", stat)
	}
	// Test legacy reverse failure and a per-tenant rebuild failure in a separate cycle.
	unknown, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id + "-unknown", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", unknown.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	runner := reconciler.New(db, failedReconcileSettle{}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); runner.Run(runCtx, time.Hour, false, false) }()
	deadline := time.Now().Add(2 * time.Second)
	var snapshot map[string]any
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		runner.HTTPHandler().ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
		if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot["last_rebuild_attempted"] == true {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	if snapshot["last_reverse_errors"] != float64(1) || snapshot["last_settle_errors"] != float64(1) || snapshot["last_rebuild_errors"] != float64(1) || snapshot["last_rebuild_error"] != false {
		t.Errorf("individual RPC failures: %+v", snapshot)
	}
	if err := db.Where("id=?", unknown.BillId).Delete(&store.Bill{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id=?", res.BillId).Delete(&store.Bill{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Tenant{}).Where("id=?", tenant.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	stat = run(reconciler.New(db, failedReconcileSettle{}, slog.New(slog.DiscardHandler), 10*time.Minute))
	if stat["last_sweep_error"] != true {
		t.Errorf("DB outage status: %+v", stat)
	}
}

func TestReconcilerStatusDisabledTasks(t *testing.T) {
	runner := reconciler.New(nil, nil, slog.New(slog.DiscardHandler), time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.Run(ctx, time.Hour, true, true)
	w := httptest.NewRecorder()
	runner.HTTPHandler().ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
	var stat map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &stat); err != nil {
		t.Fatal(err)
	}
	if stat["last_sweep_attempted"] != false || stat["last_rebuild_attempted"] != false {
		t.Fatalf("disabled tasks: %+v", stat)
	}
}
