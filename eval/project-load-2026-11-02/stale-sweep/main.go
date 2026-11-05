// Controlled local-only interleaving using the real reconciler and billing RPC.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type between struct {
	pb.BillingServiceClient
	reported bool
}

func (b *between) Reverse(ctx context.Context, req *pb.ReverseRequest, opts ...grpc.CallOption) (*pb.ReverseResponse, error) {
	r, e := b.BillingServiceClient.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: req.RequestId, BillId: req.BillId, PromptTokens: 50, CompletionTokens: 10})
	if e != nil || r == nil || !r.Accepted {
		return nil, fmt.Errorf("injected usage failed: %v %v", r, e)
	}
	b.reported = true
	return b.BillingServiceClient.Reverse(ctx, req, opts...)
}
func main() {
	dsn, addr, token := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_BILLING_ADDR"), os.Getenv("CONTROL_PLANE_TOKEN")
	if dsn == "" || addr == "" || token == "" {
		panic("isolated test endpoints required")
	}
	db, e := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if e != nil {
		panic(e)
	}
	conn, e := controlplane.Dial(addr, token)
	if e != nil {
		panic(e)
	}
	defer conn.Close()
	cli := pb.NewBillingServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := fmt.Sprintf("stale-sweep-%d", time.Now().UnixNano())
	reserve, e := cli.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: 1, ApiKeyId: 1, Model: "test-model", EstPromptTokens: 84})
	if e != nil || reserve == nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		panic(fmt.Sprintf("reserve: %v %v", reserve, e))
	}
	bridge := &between{BillingServiceClient: cli}
	stats, e := reconciler.New(db, bridge, slog.New(slog.NewTextHandler(os.Stderr, nil)), 0, reconciler.WithAutoReverse()).SweepOnce(ctx)
	if e != nil {
		panic(e)
	}
	var row struct {
		Status          string
		UsageReportedAt *time.Time
	}
	if e = db.Table("bills").Select("status,usage_reported_at").Where("request_id = ? AND phase = ?", id, "reserve").Take(&row).Error; e != nil {
		panic(e)
	}
	if os.Getenv("TEST_SECOND_SWEEP") == "1" {
		second, err := reconciler.New(db, cli, slog.New(slog.NewTextHandler(os.Stderr, nil)), 0, reconciler.WithAutoReverse()).SweepOnce(ctx)
		if err != nil {
			panic(err)
		}
		if err = db.Table("bills").Select("status,usage_reported_at").Where("request_id = ? AND phase = ?", id, "reserve").Take(&row).Error; err != nil {
			panic(err)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"request_id": id, "usage_reported_before_reverse": bridge.reported, "status": row.Status, "usage_marker_present": row.UsageReportedAt != nil, "scanned": stats.Scanned, "reversed": stats.Reversed, "second_settled": second.Settled})
		return
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"request_id": id, "usage_reported_before_reverse": bridge.reported, "status": row.Status, "usage_marker_present": row.UsageReportedAt != nil, "scanned": stats.Scanned, "reversed": stats.Reversed})
}
