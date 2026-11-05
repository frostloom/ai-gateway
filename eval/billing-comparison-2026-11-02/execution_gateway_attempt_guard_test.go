package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type guardedAttemptBridge struct {
	*gatewayExecutionBridge
	db       *gorm.DB
	observed sync.Map
}

type experimentalCompletionUsage interface {
	ReportUsageWithAttemptCompletion(context.Context, *pb.ReportUsageRequest, string) error
}

func (b *guardedAttemptBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	if os.Getenv("TEST_COMPLETION_EVIDENCE") == "1" {
		if attemptID, ok := b.observed.Load(req.RequestId); ok {
			writer, supported := any(b.store).(experimentalCompletionUsage)
			if !supported {
				return nil, fmt.Errorf("completion evidence interface absent")
			}
			if err := writer.ReportUsageWithAttemptCompletion(ctx, req, attemptID.(string)); err != nil {
				return nil, err
			}
			return &pb.ReportUsageResponse{Accepted: true}, nil
		}
	}
	return b.gatewayExecutionBridge.ReportUsage(ctx, req)
}

func (b *guardedAttemptBridge) Reserve(ctx context.Context, req *pb.ReserveRequest) (*pb.ReserveResponse, error) {
	resp, err := b.gatewayExecutionBridge.Reserve(ctx, req)
	if err != nil || resp.Code != pb.ReserveCode_RESERVE_OK {
		return resp, err
	}
	err = b.db.WithContext(ctx).Exec("INSERT INTO eval_attempt_request (tenant_id,request_id,fingerprint) VALUES (?,?,?)", req.TenantId, req.RequestId, strings.Repeat("a", 64)).Error
	return resp, err
}
func (b *guardedAttemptBridge) BeginUpstreamAttempt(ctx context.Context, tenant int64, request string, ordinal int, provider uint64) error {
	attempt, err := prepareGuardedAttempt(ctx, b.db, uint64(tenant), request, strings.Repeat("a", 64), ordinal, provider)
	if err != nil {
		return err
	}
	ok, err := beginAttempt(ctx, b.db, attempt.AttemptID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("attempt already started")
	}
	return nil
}

func (b *guardedAttemptBridge) FinishUpstreamAttempt(ctx context.Context, tenant int64, request string, ordinal int, provider uint64) error {
	var row attemptRow
	if err := b.db.WithContext(ctx).Table("eval_route_attempt").Where("tenant_id=? AND request_id=? AND ordinal=? AND provider_id=?", tenant, request, ordinal, provider).Take(&row).Error; err != nil {
		return err
	}
	if os.Getenv("TEST_COMPLETION_EVIDENCE") == "1" {
		b.observed.Store(request, row.AttemptID)
	}
	if os.Getenv("TEST_COORDINATED_FINISH") == "1" {
		_, err := completeAttemptCoordinated(ctx, b.db, uint64(tenant), request, row.AttemptID, true)
		return err
	}
	_, err := completeAttempt(ctx, b.db, row.AttemptID, true)
	return err
}

func TestAttemptGuardBlocksUnknownFailover(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing and gateway overlays")
	}
	gin.SetMode(gin.TestMode)
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	attemptDB := setupAttemptDB(t)
	if err := attemptDB.Exec("CREATE TABLE IF NOT EXISTS eval_attempt_request (tenant_id BIGINT UNSIGNED NOT NULL,request_id VARBINARY(128) NOT NULL,fingerprint VARBINARY(64) NOT NULL,PRIMARY KEY(tenant_id,request_id)) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpcServer := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	rpcServer.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = rpcServer.Serve(listener) }()
	defer rpcServer.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	owner := sha256.Sum256([]byte("synthetic-owner"))
	bridge := &guardedAttemptBridge{gatewayExecutionBridge: &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(owner[:])}, db: attemptDB}
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			id := fmt.Sprintf("gw-failover-%t-%d", stream, time.Now().UnixNano())
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
			var first, second atomic.Int32
			sweepResults := make(chan error, 1)
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				first.Add(1)
				if os.Getenv("TEST_INFLIGHT_SWEEP") == "1" {
					rid := id + "-request"
					if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", rid, store.PhaseReserve).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
						sweepResults <- err
						return
					}
					if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=? AND phase=?", rid, store.PhaseReserve).Error; err != nil {
						sweepResults <- err
						return
					}
					runner := reconciler.New(db, &stateSweepBridge{db: db, conn: conn, s: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
					stats, err := runner.SweepOnce(ctx)
					if err != nil || stats.Scanned < 1 || stats.Reversed != 0 {
						sweepResults <- fmt.Errorf("sweep=%+v err=%v", stats, err)
						return
					}
					var current store.Bill
					if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).Take(&current).Error; err != nil || current.Status != store.StatusPending {
						sweepResults <- fmt.Errorf("bill=%s err=%v", current.Status, err)
						return
					}
					// This suite reuses its isolated database across tests; restore the
					// artificial age so subsequent scans do not count this fixture.
					if err := db.Model(&store.Bill{}).Where("id=?", current.ID).UpdateColumn("created_at", time.Now()).Error; err != nil {
						sweepResults <- err
						return
					}
					sweepResults <- nil
				}
				if os.Getenv("TEST_INFLIGHT_SUCCESS") == "1" {
					if stream {
						_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n")
					} else {
						_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
					}
					return
				}
				h, ok := w.(http.Hijacker)
				if !ok {
					t.Error("hijack unsupported")
					return
				}
				c, _, err := h.Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = c.Close()
			}))
			defer primary.Close()
			secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				second.Add(1)
				if stream {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`)
				}
			}))
			defer secondary.Close()
			p1 := store.Provider{Name: id + "-1", BaseURL: primary.URL, Models: "[]", Weight: 1}
			p2 := store.Provider{Name: id + "-2", BaseURL: secondary.URL, Models: "[]", Weight: 1}
			if err := db.Create(&p1).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&p2).Error; err != nil {
				t.Fatal(err)
			}
			router := &sequentialExecutionRouter{routes: []*gatewayclient.RouteResult{{ProviderID: p1.ID, BaseURL: primary.URL}, {ProviderID: p2.ID, BaseURL: secondary.URL}}}
			engine := gin.New()
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("tenant_id", int64(tenant.ID))
				c.Set("api_key_id", int64(1))
				c.Set("request_id", id+"-request")
			}, handler.Chat(bridge, router, slog.New(slog.DiscardHandler), time.Second, false))
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t}`, id, stream)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if os.Getenv("TEST_INFLIGHT_SWEEP") == "1" {
				if err := <-sweepResults; err != nil {
					t.Fatal(err)
				}
				t.Log("sweep_while_upstream_inflight=true reversed=0")
			}
			success := os.Getenv("TEST_INFLIGHT_SUCCESS") == "1"
			wantStatus := 502
			if success {
				wantStatus = 200
			}
			if resp.StatusCode != wantStatus || first.Load() != 1 || second.Load() != 0 {
				t.Fatalf("status=%d first=%d second=%d", resp.StatusCode, first.Load(), second.Load())
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			if success {
				if bill.Status != store.StatusSettled || bill.UsageReportedAt == nil {
					t.Fatal(bill.Status, bill.UsageReportedAt)
				}
			} else if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
				t.Fatal(bill.Status, bill.UsageReportedAt)
			}
			var attempts []attemptRow
			if err := attemptDB.Table("eval_route_attempt").Where("tenant_id=? AND request_id=?", tenant.ID, id+"-request").Find(&attempts).Error; err != nil || len(attempts) != 1 || attempts[0].ProviderID != p1.ID {
				t.Fatal(attempts, err)
			}
			if success {
				if attempts[0].State != "completed" || attempts[0].Outcome == nil || !*attempts[0].Outcome {
					t.Fatal(attempts[0])
				}
				if os.Getenv("TEST_COMPLETION_EVIDENCE") == "1" {
					var evidence string
					if err := db.Raw("SELECT COALESCE(completion_attempt_id,'') FROM bills WHERE id=?", bill.ID).Scan(&evidence).Error; err != nil || evidence != attempts[0].AttemptID {
						t.Fatalf("completion evidence=%q attempt=%q err=%v", evidence, attempts[0].AttemptID, err)
					}
				}
			} else if attempts[0].State != "sending" {
				t.Fatal(attempts[0])
			}
			if success {
				var settled store.Bill
				if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseSettle).Take(&settled).Error; err != nil || settled.ActualQuota == nil || *settled.ActualQuota != 40 {
					t.Fatalf("actual=%v err=%v", settled.ActualQuota, err)
				}
				var attemptEvents int64
				if err := attemptDB.Table("eval_attempt_outbox").Where("attempt_id=?", attempts[0].AttemptID).Count(&attemptEvents).Error; err != nil || attemptEvents != 1 {
					t.Fatal(attemptEvents, err)
				}
			}
			balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || balance != ledger || (success && balance != 960) {
				t.Fatal(balance, ledger, err)
			}
			for i, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				want := int64(0)
				if i == 0 || (success && (i == 1 || i == 2)) {
					want = 1
				}
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+"-request:"+phase).Count(&n).Error; err != nil || n != want {
					t.Fatal(phase, n, err)
				}
			}
			t.Logf("stream=%v first=%d second=%d status=%d attempt_state=%s bill=%s balance=%d ledger=%d", stream, first.Load(), second.Load(), resp.StatusCode, attempts[0].State, bill.Status, balance, ledger)

		})
	}
}
