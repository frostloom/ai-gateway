package client

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type replayContractServer struct {
	billingv1.UnimplementedBillingServiceServer
	ack    bool
	code   billingv1.ReserveCode
	writes *atomic.Int64
}

func (s *replayContractServer) Reserve(ctx context.Context, _ *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("x-agw-reserve-mode")
	if len(values) != 1 || values[0] != "replay-v1" {
		return nil, status.Error(codes.InvalidArgument, "missing contract request")
	}
	if s.writes != nil {
		s.writes.Add(1)
	}
	if s.ack {
		if e := grpc.SetHeader(ctx, metadata.Pairs("x-agw-reserve-contract", "replay-v1")); e != nil {
			return nil, e
		}
	}
	return &billingv1.ReserveResponse{Code: s.code, BillId: 1}, nil
}
func TestBillingReserveRequiresReplayContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		ack  bool
		code billingv1.ReserveCode
		want codes.Code
	}{
		{"first", true, billingv1.ReserveCode_RESERVE_OK, codes.OK},
		{"duplicate", true, billingv1.ReserveCode_RESERVE_DUPLICATE, codes.OK},
		{"old server", false, billingv1.ReserveCode_RESERVE_OK, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			billingv1.RegisterBillingServiceServer(server, &replayContractServer{ack: tc.ack, code: tc.code})
			go func() { _ = server.Serve(listener) }()
			defer server.Stop()
			conn, e := grpc.NewClient("passthrough:///replay-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			b := &Billing{conn: conn, cli: billingv1.NewBillingServiceClient(conn)}
			resp, e := b.Reserve(context.Background(), &billingv1.ReserveRequest{RequestId: "test"})
			if got := status.Code(e); got != tc.want {
				t.Fatalf("status=%s want=%s error=%v", got, tc.want, e)
			}
			if e == nil && (resp == nil || resp.Code != tc.code) {
				t.Fatalf("response=%v", resp)
			}
		})
	}
}

// An old server can commit before the client discovers it did not acknowledge
// the new contract. The gateway must not forward; rollout must still account
// for an ambiguous/pending legacy reservation.
func TestOldBillingMayMutateBeforeMissingAck(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	var writes atomic.Int64
	billingv1.RegisterBillingServiceServer(server, &replayContractServer{ack: false, code: billingv1.ReserveCode_RESERVE_OK, writes: &writes})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, e := grpc.NewClient("passthrough:///replay-old", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	b := &Billing{conn: conn, cli: billingv1.NewBillingServiceClient(conn)}
	resp, e := b.Reserve(context.Background(), &billingv1.ReserveRequest{RequestId: "test"})
	if resp != nil || status.Code(e) != codes.Unavailable || writes.Load() != 1 {
		t.Fatalf("response=%v error=%v simulated writes=%d", resp, e, writes.Load())
	}
}

type v2ContractTestServer struct {
	billingv1.UnimplementedBillingServiceServer
	ack   string
	calls atomic.Int64
}

func (s *v2ContractTestServer) Reserve(ctx context.Context, _ *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if v := md.Get(controlplane.ReserveModeKey); len(v) != 1 || v[0] != controlplane.ReserveReplayV2 {
		return nil, status.Error(codes.InvalidArgument, "wrong mode")
	}
	if v := md.Get(controlplane.ReserveContentKey); len(v) != 1 || v[0] != strings.Repeat("a", 64) {
		return nil, status.Error(codes.InvalidArgument, "wrong digest")
	}
	s.calls.Add(1)
	if s.ack != "" {
		_ = grpc.SetHeader(ctx, metadata.Pairs(controlplane.ReserveContractKey, s.ack))
	}
	return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: 1}, nil
}
func TestBillingV2RequiresExactAcknowledgement(t *testing.T) {
	for _, ack := range []string{"", controlplane.ReserveReplayV1, controlplane.ReserveReplayV2} {
		t.Run("ack="+ack, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			service := &v2ContractTestServer{ack: ack}
			billingv1.RegisterBillingServiceServer(server, service)
			go func() { _ = server.Serve(listener) }()
			defer server.Stop()
			conn, err := grpc.NewClient("passthrough:///v2-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			client := &Billing{conn: conn, cli: billingv1.NewBillingServiceClient(conn)}
			ctx := controlplane.WithReserveContent(context.Background(), strings.Repeat("a", 64))
			resp, err := client.Reserve(ctx, &billingv1.ReserveRequest{RequestId: "test"})
			if service.calls.Load() != 1 {
				t.Fatal(service.calls.Load())
			}
			if ack == controlplane.ReserveReplayV2 {
				if err != nil || resp == nil {
					t.Fatal(resp, err)
				}
			} else if status.Code(err) != codes.Unavailable || resp != nil {
				t.Fatal(resp, err)
			}
		})
	}
}
