package comparison

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestParallelSweepProcessKillAndRecovery(t *testing.T) {
	if os.Getenv("TEST_RECON_PROCESS") != "1" {
		t.Skip("isolated subprocess test")
	}
	overlay := os.Getenv("TEST_RECON_OVERLAY_PATH")
	if overlay == "" {
		t.Fatal("requires test-only parallel overlay")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("sweepkill-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 7)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%d", tag, i)
		res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: ids[i], TenantId: int64(tenant.ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve=%v err=%v", res, err)
		}
		if i < 6 {
			if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[i], BillId: res.BillId, PromptTokens: 40}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "reconciler")
	build := exec.Command("go", "build", "-overlay", overlay, "-o", binary, "./cmd/reconciler")
	build.Dir = filepath.Clean(filepath.Join(cwd, "../.."))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build=%v output=%s", err, out)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var enabled atomic.Bool
	enabled.Store(true)
	entered := make(chan string, 4)
	committed := make(chan struct{}, 1)
	finished := make(chan struct{}, 4)
	intercept := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if req, ok := req.(*pb.SettleRequest); ok && enabled.Load() && strings.HasPrefix(req.RequestId, tag) {
			entered <- req.RequestId
			defer func() { finished <- struct{}{} }()
			if req.RequestId == ids[0] {
				resp, err := handler(ctx, req)
				if err != nil {
					return resp, err
				}
				committed <- struct{}{}
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return handler(ctx, req)
	}
	rpc := grpc.NewServer(grpc.ChainUnaryInterceptor(controlplane.UnaryServer("process-test"), intercept))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	child := func() *exec.Cmd {
		cmd := exec.Command(binary, "-once", "-no-rebuild", "-grace=0")
		cmd.Env = append(os.Environ(), "MYSQL_DSN="+os.Getenv("COMPARISON_MYSQL_DSN"), "BILLING_ADDR="+lis.Addr().String(), "CONTROL_PLANE_TOKEN=process-test", "LISTEN_HOST=127.0.0.1", "RECONCILER_PORT=0")
		return cmd
	}
	first := child()
	var output bytes.Buffer
	first.Stdout = &output
	first.Stderr = &output
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = first.Process.Kill()
			_ = first.Wait()
		}
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(20 * time.Second):
			t.Fatal("four controlled RPCs did not arrive")
		}
	}
	select {
	case <-committed:
	case <-time.After(20 * time.Second):
		t.Fatal("first commit did not complete")
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err == nil {
		t.Fatal("killed process unexpectedly succeeded")
	}
	reaped = true
	for i := 0; i < 4; i++ {
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("server calls did not stop after process death")
		}
	}
	var firstBill store.Bill
	if err := db.Where("request_id=? AND phase=?", ids[0], store.PhaseReserve).Take(&firstBill).Error; err != nil || firstBill.Status != store.StatusSettled {
		t.Fatalf("first=%+v err=%v", firstBill, err)
	}
	for _, id := range ids[1:] {
		var b store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&b).Error; err != nil || b.Status != store.StatusPending {
			t.Fatalf("before retry bill=%+v err=%v", b, err)
		}
	}
	enabled.Store(false)
	retryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	second := exec.CommandContext(retryCtx, binary, "-once", "-no-rebuild", "-grace=0")
	second.Env = append(os.Environ(), "MYSQL_DSN="+os.Getenv("COMPARISON_MYSQL_DSN"), "BILLING_ADDR="+lis.Addr().String(), "CONTROL_PLANE_TOKEN=process-test", "LISTEN_HOST=127.0.0.1", "RECONCILER_PORT=0")
	out, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("retry=%v output=%s", err, out)
	}
	for i, id := range ids {
		var b store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&b).Error; err != nil {
			t.Fatal(err)
		}
		want := store.StatusSettled
		if i == 6 {
			want = store.StatusPending
		}
		if b.Status != want {
			t.Fatalf("id=%s status=%s want=%s", id, b.Status, want)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			var count int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			expected := int64(1)
			if phase == "reverse" || (i == 6 && (phase == "usage" || phase == "settle")) {
				expected = 0
			}
			if count != expected {
				t.Fatalf("id=%s phase=%s count=%d", id, phase, count)
			}
		}
		var n int64
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		expected := int64(1)
		if i == 6 {
			expected = 0
		}
		if n != expected {
			t.Fatalf("settle rows=%d", n)
		}
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger || live != 660 {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Logf("reaped killed child: first commit had no reply; other five initially pending; restarted child settled six once, unknown retained, balance=%d", live)
}
