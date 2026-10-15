package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	redis "github.com/redis/go-redis/v9"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Uses a random disposable database and private Redis; another worktree may be
// running the legacy fixed-name agent tests at the same time.
func isolatedObservedHandler(t *testing.T) (*Handler, *fakeAdmin) {
	t.Helper()
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	name := "lf_agent_" + hex.EncodeToString(id[:])
	admin, err := sql.Open("mysql", agentTestMySQLHost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	db, err := gorm.Open(mysql.Open(agentTestMySQLHost+name+"?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { rdb.Close() })
	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	fa := &fakeAdmin{}
	srv := httptest.NewServer(fa.handler())
	t.Cleanup(srv.Close)
	return &Handler{billing: fakeBilling{tenantID: 42}, tb: NewToolbox(NewAdminClient(srv.URL)), st: st, rdb: rdb, log: slog.New(slog.NewTextHandler(io.Discard, nil))}, fa
}

type observedSpans struct {
	mu    sync.Mutex
	spans []*tracepb.Span
	raw   []byte
}

func localObserver(t *testing.T) (*observability.Observer, *observedSpans) {
	t.Helper()
	c := &observedSpans{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var request collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(raw, &request); err != nil {
			t.Error(err)
		}
		c.mu.Lock()
		c.raw = append(c.raw, raw...)
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				c.spans = append(c.spans, scope.Spans...)
			}
		}
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(srv.Close)
	o, err := observability.New(context.Background(), observability.Config{Enabled: true, Endpoint: srv.URL + "/api/public/otel/v1/traces", PublicKey: "pk-local", SecretKey: "sk-local-secret", HashKey: strings.Repeat("a", 32), Environment: "evaluation", Release: "test", SampleRate: 1, ExportTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = o.Shutdown(context.Background()) })
	return o, c
}
func observedAttr(span *tracepb.Span, key string) string {
	for _, a := range span.Attributes {
		if a.Key == key {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func TestObservedJevFastPathWithoutGeneration(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	o, c := localObserver(t)
	h.SetObserver(o)
	enableFastPath(t, h, true, .9)
	h.SetJev(jev.NewFake(fastVerdict("get_balance", .98)))
	h.llm = &scriptedLLM{}
	rec := cancellationRequest(h, "private-sid", "查余额")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if err := o.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	names := map[string]int{}
	for _, span := range c.spans {
		names[span.Name]++
		if observedAttr(span, "langfuse.session.id") == "" {
			t.Fatal("session absent on child span")
		}
		if observedAttr(span, "langfuse.observation.type") == "generation" {
			t.Fatal("invented model attempt")
		}
	}
	if names["agent.turn"] != 1 || names["jev.intent"] != 1 || names["tool.get_balance"] != 1 {
		t.Fatalf("missing real call boundaries: %v", names)
	}
	if bytes.Contains(c.raw, []byte("private-sid")) || bytes.Contains(c.raw, []byte("sk-test-key-1")) || bytes.Contains(c.raw, []byte("查余额")) {
		t.Fatal("metadata-only policy leaked content")
	}
}

func TestObservedPreviewAndConfirmRemainSeparate(t *testing.T) {
	h, admin := isolatedObservedHandler(t)
	o, c := localObserver(t)
	h.SetObserver(o)
	_ = h.SetRoutingMode("model")
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("call", "recharge", `{"amount_money":100}`)}}
	first := cancellationRequest(h, "pending-sid", "充值100元")
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	if len(admin.posts) != 0 {
		t.Fatal("preview mutated account")
	}
	second := cancellationRequest(h, "pending-sid", "确认")
	if second.Code != 200 || len(admin.posts) != 2 {
		t.Fatalf("confirmation changed: %s", second.Body.String())
	}
	if err := o.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	preview, execute, roots := 0, 0, 0
	for _, span := range c.spans {
		switch span.Name {
		case "tool.preview.recharge":
			preview++
		case "tool.recharge":
			execute++
		case "agent.turn":
			roots++
		}
	}
	if preview != 1 || execute != 1 || roots != 2 {
		t.Fatalf("preview=%d execute=%d turns=%d", preview, execute, roots)
	}
}

func TestObservedRealModelAttemptsPreserveUsage(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	o, c := localObserver(t)
	h.SetObserver(o)
	_ = h.SetRoutingMode("model")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-local-model-key" {
			t.Error("model authentication changed")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "你好"}}}, "usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 2}})
	}))
	defer upstream.Close()
	h.llm = NewLLMClient(upstream.URL, "sk-local-model-key", "local-model")
	rec := cancellationRequest(h, "llm-sid", "你好")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if err := o.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	generations := 0
	for _, span := range c.spans {
		if observedAttr(span, "langfuse.observation.type") == "generation" {
			generations++
			if observedAttr(span, "langfuse.observation.prompt.name") != "customer-service/system/"+contentSHA([]byte(systemPrompt)) {
				t.Fatal("actual generation is not linked to its base prompt")
			}
			if observedAttr(span, "langfuse.observation.usage_details") != `{"input":7,"output":2}` {
				t.Fatal("provider usage lost")
			}
		}
	}
	if generations != 1 {
		t.Fatalf("generations=%d", generations)
	}
	if bytes.Contains(c.raw, []byte("sk-local-model-key")) {
		t.Fatal("model key leaked")
	}
}
