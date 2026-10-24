// Local-only Docker acceptance fixture. Not included in the production runtime target.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billing "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type auth struct {
	billing.UnimplementedBillingServiceServer
}

func (auth) ValidateAPIKey(_ context.Context, r *billing.ValidateAPIKeyRequest) (*billing.ValidateAPIKeyResponse, error) {
	h := sha256.Sum256([]byte("smoke-key"))
	return &billing.ValidateAPIKeyResponse{Valid: r.ApiKeyHash == hex.EncodeToString(h[:]), TenantId: 1, ApiKeyId: 1}, nil
}

var mu sync.Mutex
var counts = map[string]int{}

func die(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func probe(endpoint string) error {
	c := http.Client{Timeout: 20 * time.Second}
	for _, q := range []string{"退订退款规则", "外星天气"} {
		b, _ := json.Marshal(map[string]any{"message": q})
		req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer smoke-key")
		req.Header.Set("Content-Type", "application/json")
		r, e := c.Do(req)
		if e != nil {
			return e
		}
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var out struct {
			Trace []struct{ Tool, Result, Phase string }
			Reply string
		}
		if r.StatusCode != 200 || json.Unmarshal(raw, &out) != nil || len(out.Trace) != 1 || out.Trace[0].Tool != "search_platform_knowledge" || out.Trace[0].Phase != "read" {
			return fmt.Errorf("invalid smoke response status=%d body=%s", r.StatusCode, raw)
		}
		status := "ok"
		if q == "外星天气" {
			status = "no_match"
		}
		if !strings.Contains(out.Trace[0].Result, `"status":"`+status+`"`) {
			return fmt.Errorf("wrong retrieval %s", raw)
		}
	}
	time.Sleep(time.Second)
	r, e := c.Get("http://fixture:9991/stats")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	var stats map[string]int
	json.NewDecoder(r.Body).Decode(&stats)
	for _, name := range []string{"knowledge.retrieval", "knowledge.chunk", "knowledge.request_context", "knowledge.request_outcome"} {
		if stats[name] == 0 {
			return fmt.Errorf("missing observation %s", name)
		}
	}
	fmt.Println("PASS success/no_match, real container model inputs and OTLP spans")
	return nil
}
func main() {
	network := flag.Bool("network-probe", false, "diagnose local fixture transport")
	ready := flag.Bool("ready", false, "local fixture readiness")
	probeURL := flag.String("probe", "", "probe container agent")
	readonly := flag.Bool("check-readonly", false, "assert immutable runtime")
	flag.Parse()
	if *network {
		c, e := net.DialTimeout("tcp", "fixture:9992", 3*time.Second)
		if e != nil {
			die(e)
		}
		c.Close()
		fmt.Println("TCP fixture:9992 reachable")
		fmt.Printf("proxy_configured=%t no_proxy_configured=%t\n", os.Getenv("HTTPS_PROXY") != "" || os.Getenv("HTTP_PROXY") != "", os.Getenv("NO_PROXY") != "")
		b, e := client.NewBilling("fixture:9992", "smoke-control")
		die(e)
		defer b.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h := sha256.Sum256([]byte("smoke-key"))
		_, e = b.ValidateAPIKey(ctx, hex.EncodeToString(h[:]))
		die(e)
		fmt.Println("gRPC reachable")
		return
	}
	if *ready {
		c := http.Client{Timeout: 2 * time.Second}
		r, e := c.Get("http://127.0.0.1:9991/healthz")
		die(e)
		defer r.Body.Close()
		if r.StatusCode != 200 {
			die(fmt.Errorf("fixture not ready"))
		}
		return
	}
	if *readonly {
		if e := os.WriteFile("/app/knowledge/platform/write-probe", []byte("x"), 0600); e == nil {
			die(fmt.Errorf("runtime knowledge writable"))
		}
		if os.Getuid() == 0 {
			die(fmt.Errorf("runtime root"))
		}
		fmt.Println("PASS read-only nonroot knowledge")
		return
	}
	if *probeURL != "" {
		die(probe(*probeURL))
		return
	}
	db, e := gorm.Open(mysql.Open(os.Getenv("MYSQL_DSN")), &gorm.Config{})
	die(e)
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
	die(store.NewStore(db, rdb).AutoMigrate())
	gs := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("smoke-control")))
	billing.RegisterBillingServiceServer(gs, auth{})
	l, e := net.Listen("tcp", ":9992")
	die(e)
	go gs.Serve(l)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(counts)
	})
	mux.HandleFunc("/api/public/otel/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		var req collector.ExportTraceServiceRequest
		if e := proto.Unmarshal(b, &req); e != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					counts[s.Name]++
				}
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []agent.Message }
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		var msg agent.Message
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			msg = agent.Message{Role: "assistant", Content: "退订当期不退款。[KB-006]"}
			if strings.Contains(last.Content, `"status":"no_match"`) {
				msg.Content = "未检索到相关资料，无法核实。"
			}
		} else {
			query := "退订退款"
			if strings.Contains(last.Content, "外星") {
				query = "外星天气"
			}
			tc := agent.ToolCall{ID: "smoke-call", Type: "function"}
			tc.Function.Name = "search_platform_knowledge"
			b, _ := json.Marshal(map[string]any{"query": query})
			tc.Function.Arguments = string(b)
			msg = agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1}})
	})
	die(http.ListenAndServe(":9991", mux))
}
