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
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type sequentialExecutionRouter struct {
	routes []*gatewayclient.RouteResult
	next   int
}

func (r *sequentialExecutionRouter) Route(context.Context, string, ...uint64) (*gatewayclient.RouteResult, error) {
	if r.next >= len(r.routes) {
		return nil, nil
	}
	out := r.routes[r.next]
	r.next++
	return out, nil
}
func (r *sequentialExecutionRouter) Report(context.Context, uint64, bool) {}

// Characterizes the remaining bug: one request-level permission does not gate
// each upstream attempt. It intentionally expects two simulated calls.
func TestExecutionPermissionDoesNotGuardFailover(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires experimental billing and gateway overlays")
	}
	gin.SetMode(gin.TestMode)
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
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
	bridge := &gatewayExecutionBridge{store: s, atomicReserve: candidate, conn: conn, ownerHash: hex.EncodeToString(owner[:])}
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
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				first.Add(1)
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
			if resp.StatusCode != 200 || first.Load() != 1 || second.Load() != 1 {
				t.Fatal(resp.StatusCode, first.Load(), second.Load())
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled || bill.ProviderID == nil || *bill.ProviderID != p2.ID {
				t.Fatal(bill.Status, bill.ProviderID, err)
			}
			var execution string
			if err := db.Raw("SELECT execution_state FROM bills WHERE id=?", bill.ID).Scan(&execution).Error; err != nil || execution != "forwarding" {
				t.Fatal(execution, err)
			}
			balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || balance != 960 || ledger != balance {
				t.Fatal(balance, ledger, err)
			}
			for i, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				want := int64(1)
				if i == 3 {
					want = 0
				}
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+"-request:"+phase).Count(&n).Error; err != nil || n != want {
					t.Fatal(phase, n, err)
				}
			}
			t.Logf("stream=%v first=%d second=%d status=%d state=%s bill=%s balance=%d ledger=%d", stream, first.Load(), second.Load(), resp.StatusCode, execution, bill.Status, balance, ledger)
		})
	}
}
