package comparison

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type capabilityServer struct {
	db             *gorm.DB
	recoverySecret string
}

func (s *capabilityServer) Transition(ctx context.Context, req *wrapperspb.StringValue) (*wrapperspb.Int64Value, error) {
	parts := strings.Split(req.Value, "|")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	var q *gorm.DB
	switch parts[1] {
	case "forwarding":
		hash := sha256.Sum256([]byte(parts[2]))
		q = s.db.WithContext(ctx).Exec("UPDATE bills SET execution_state='forwarding' WHERE request_id=? AND phase=? AND status=? AND execution_state='reserved' AND execution_owner_hash=? AND execution_lease_until>CURRENT_TIMESTAMP(6) AND usage_reported_at IS NULL", parts[0], store.PhaseReserve, store.StatusPending, hex.EncodeToString(hash[:]))
	case "reversing":
		if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(s.recoverySecret)) != 1 {
			return nil, status.Error(codes.PermissionDenied, "recovery capability rejected")
		}
		q = s.db.WithContext(ctx).Exec("UPDATE bills SET execution_state='reversing' WHERE request_id=? AND phase=? AND status=? AND execution_state='reserved' AND execution_lease_until<=CURRENT_TIMESTAMP(6) AND usage_reported_at IS NULL", parts[0], store.PhaseReserve, store.StatusPending)
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid operation")
	}
	if q.Error != nil {
		return nil, status.Error(codes.Unavailable, "transition failed")
	}
	return wrapperspb.Int64(q.RowsAffected), nil
}

func TestExecutionRPCCapabilityAndLease(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing state overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("exec-cap-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: base, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const method = "/eval.execution.v1.Execution/Transition"
	call := func(id, op, secret string) (int64, error) {
		var out wrapperspb.Int64Value
		err := conn.Invoke(ctx, method, wrapperspb.String(id+"|"+op+"|"+secret), &out)
		return out.Value, err
	}
	for _, scenario := range []string{"live", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			id := base + "-" + scenario
			tenant := store.Tenant{Name: id, InitialQuota: 500}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
				t.Fatal(err)
			}
			bill, err := s.ReserveWithReplaySignal(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84})
			if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(bill, err)
			}
			ownerHash := sha256.Sum256([]byte("synthetic-owner"))
			interval := "DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL 30 SECOND)"
			if scenario == "expired" {
				interval = "DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND)"
			}
			q := db.Exec("UPDATE bills SET execution_owner_hash=?,execution_lease_until="+interval+" WHERE id=? AND execution_state='reserved'", hex.EncodeToString(ownerHash[:]), bill.BillId)
			if q.Error != nil || q.RowsAffected != 1 {
				t.Fatal(q.RowsAffected, q.Error)
			}
			if n, err := call(id, "forwarding", "wrong-owner"); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			if _, err := call(id, "reversing", "wrong-recovery"); status.Code(err) != codes.PermissionDenied {
				t.Fatal(err)
			}
			if scenario == "live" {
				if n, err := call(id, "reversing", "synthetic-recovery"); err != nil || n != 0 {
					t.Fatal(n, err)
				}
			}
			type result struct {
				op  string
				n   int64
				err error
			}
			ch := make(chan result, 2)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for _, op := range []string{"forwarding", "reversing"} {
				wg.Add(1)
				go func(op string) {
					defer wg.Done()
					<-start
					secret := "synthetic-owner"
					if op == "reversing" {
						secret = "synthetic-recovery"
					}
					n, err := call(id, op, secret)
					ch <- result{op, n, err}
				}(op)
			}
			close(start)
			wg.Wait()
			close(ch)
			winner := ""
			for res := range ch {
				if res.err != nil {
					t.Fatal(res.err)
				}
				if res.n == 1 {
					if winner != "" {
						t.Fatal("two winners")
					}
					winner = res.op
				}
			}
			expected := "forwarding"
			if scenario == "expired" {
				expected = "reversing"
			}
			if winner != expected {
				t.Fatal(winner, expected)
			}
			if scenario == "live" {
				if _, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id}); status.Code(err) != codes.FailedPrecondition {
					t.Fatal(err)
				}
				if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
					t.Fatal(err)
				}
				done, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
				if err != nil || !done.Settled {
					t.Fatal(done, err)
				}
			} else {
				if n, err := call(id, "forwarding", "synthetic-owner"); err != nil || n != 0 {
					t.Fatal(n, err)
				}
				done, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
				if err != nil || !done.Reversed {
					t.Fatal(done, err)
				}
			}
			bal, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || bal != ledger {
				t.Fatal(bal, ledger, err)
			}
			t.Logf("scenario=%s winner=%s wrong_owner=denied wrong_recovery=denied balance=%d ledger=%d", scenario, winner, bal, ledger)
		})
	}
}
