package comparison

import (
	"context"
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

// Test-only RPC: one shared control token, no tenant ownership or per-request
// authority. Result 0 is deliberately ambiguous (lost race or replay).
type executionRPC interface {
	Transition(context.Context, *wrapperspb.StringValue) (*wrapperspb.Int64Value, error)
}
type executionServer struct{ db *gorm.DB }

func (s *executionServer) Transition(ctx context.Context, req *wrapperspb.StringValue) (*wrapperspb.Int64Value, error) {
	parts := strings.Split(req.Value, "|")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "forwarding" && parts[1] != "reversing") {
		return nil, status.Error(codes.InvalidArgument, "invalid transition")
	}
	q := s.db.WithContext(ctx).Exec("UPDATE bills SET execution_state=? WHERE request_id=? AND phase=? AND status=? AND execution_state='reserved' AND usage_reported_at IS NULL", parts[1], parts[0], store.PhaseReserve, store.StatusPending)
	if q.Error != nil {
		return nil, status.Error(codes.Unavailable, "transition failed")
	}
	return wrapperspb.Int64(q.RowsAffected), nil
}
func executionTransitionHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(wrapperspb.StringValue)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(executionRPC).Transition(ctx, req.(*wrapperspb.StringValue))
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/eval.execution.v1.Execution/Transition"}, handler)
}

var executionDesc = grpc.ServiceDesc{ServiceName: "eval.execution.v1.Execution", HandlerType: (*executionRPC)(nil), Methods: []grpc.MethodDesc{{MethodName: "Transition", Handler: executionTransitionHandler}}}

func TestExecutionRPCCompetingPermissions(t *testing.T) {
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
	base := fmt.Sprintf("execution-rpc-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: base, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &executionServer{db})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	bad, err := controlplane.Dial(listener.Addr().String(), "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	const method = "/eval.execution.v1.Execution/Transition"
	var wrong wrapperspb.Int64Value
	if err := bad.Invoke(ctx, method, wrapperspb.String("unused|forwarding"), &wrong); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	wins := map[string]int{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
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
		type result struct {
			op  string
			n   int64
			err error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, op := range []string{"forwarding", "reversing"} {
			wg.Add(1)
			go func(op string) {
				defer wg.Done()
				<-start
				var out wrapperspb.Int64Value
				e := conn.Invoke(ctx, method, wrapperspb.String(id+"|"+op), &out)
				results <- result{op, out.Value, e}
			}(op)
		}
		close(start)
		wg.Wait()
		close(results)
		winner := ""
		for res := range results {
			if res.err != nil {
				t.Fatal(res.err)
			}
			if res.n == 1 {
				if winner != "" {
					t.Fatal("two permissions granted")
				}
				winner = res.op
			} else if res.n != 0 {
				t.Fatal(res.n)
			}
		}
		if winner == "" {
			t.Fatal("no winner")
		}
		wins[winner]++
		var replay wrapperspb.Int64Value
		if err := conn.Invoke(ctx, method, wrapperspb.String(id+"|"+winner), &replay); err != nil || replay.Value != 0 {
			t.Fatal(replay.Value, err)
		}
		if winner == "forwarding" {
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
	}
	t.Logf("forwarding=%d reversing=%d unauthenticated=rejected replay=no_new_permission", wins["forwarding"], wins["reversing"])
}
