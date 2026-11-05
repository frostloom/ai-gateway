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
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

func TestNetworkGatewayInterruptedStream(t *testing.T) {
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
	for _, mode := range []string{"empty", "partial"} {
		t.Run(mode, func(t *testing.T) {
			id := fmt.Sprintf("http-stream-%s-%d", mode, time.Now().UnixNano())
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
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Content-Length", "200")
				w.WriteHeader(200)
				if mode == "empty" {
					_, _ = io.WriteString(w, "data: {\"choices\":[")
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
				}
			}))
			defer up.Close()
			provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]", Weight: 1}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("tenant_id", int64(tenant.ID))
				c.Set("api_key_id", int64(1))
				c.Set("request_id", id+"-request")
			}, handler.Chat(bridge, gatewayExecutionRouter{url: up.URL, providerID: provider.ID}, slog.New(slog.DiscardHandler), time.Second, os.Getenv("TEST_GATEWAY_CONSERVATIVE") == "1"))
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":true}`, id)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			response, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			wantStatus := 200
			if mode == "empty" && os.Getenv("TEST_STREAM_SAFE") == "1" {
				wantStatus = 502
			}
			if resp.StatusCode != wantStatus || calls.Load() != 1 || strings.Contains(string(response), "[DONE]") {
				t.Fatal(resp.StatusCode, calls.Load(), string(response))
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id+"-request", store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "empty" {
				validBody := len(response) == 0
				if os.Getenv("TEST_STREAM_SAFE") == "1" {
					validBody = strings.Contains(string(response), "upstream stream incomplete") && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json")
				}
				if !validBody || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
					t.Fatal(string(response), bill.Status)
				}
			} else {
				if os.Getenv("TEST_GATEWAY_CONSERVATIVE") == "1" {
					if !strings.Contains(string(response), "hello") || bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
						t.Fatal(string(response), bill.Status, bill.UsageReportedAt)
					}
				} else if !strings.Contains(string(response), "hello") || bill.Status != store.StatusSettled || bill.CompletionTokens == nil || *bill.CompletionTokens <= 0 {
					t.Fatal(string(response), bill.Status, bill.CompletionTokens)
				}
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != ledger {
				t.Fatal(live, ledger, err)
			}
			for i, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				want := int64(0)
				if i == 0 || (mode == "partial" && os.Getenv("TEST_GATEWAY_CONSERVATIVE") != "1" && (i == 1 || i == 2)) {
					want = 1
				}
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+"-request:"+phase).Count(&n).Error; err != nil || n != want {
					t.Fatal(phase, n, want, err)
				}
			}
			t.Logf("mode=%s http=%d bytes=%d done=false upstream_calls=%d bill=%s balance=%d ledger=%d", mode, resp.StatusCode, len(response), calls.Load(), bill.Status, live, ledger)
		})
	}
}
