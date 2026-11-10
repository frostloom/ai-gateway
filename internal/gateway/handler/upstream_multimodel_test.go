package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type multiModelAuthServer struct {
	billingv1.UnimplementedBillingServiceServer
	keyHash string
}

func (s *multiModelAuthServer) ValidateAPIKey(_ context.Context, req *billingv1.ValidateAPIKeyRequest) (*billingv1.ValidateAPIKeyResponse, error) {
	return &billingv1.ValidateAPIKeyResponse{Valid: req.ApiKeyHash == s.keyHash, TenantId: 42, ApiKeyId: 7}, nil
}

// One tenant credential must survive the normal authentication entry point while
// each routed provider receives its own credentials and configured API prefix.
func TestOneAPIKeyRoutesMultipleModelsAndProtocols(t *testing.T) {
	const userKey = "sk-tenant-single-key"
	keySum := sha256.Sum256([]byte(userKey))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	billingv1.RegisterBillingServiceServer(grpcServer, &multiModelAuthServer{keyHash: hex.EncodeToString(keySum[:])})
	go grpcServer.Serve(listener)
	t.Cleanup(grpcServer.Stop)
	authClient, err := client.NewBilling(listener.Addr().String(), "loopback-control-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authClient.Close() })

	var seenMu sync.Mutex
	var seen []string
	providerServer := func(providerKey, prefix string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+providerKey {
				t.Errorf("upstream Authorization = %q", r.Header.Get("Authorization"))
			}
			if strings.Contains(r.Header.Get("Authorization"), userKey) || r.Header.Get("x-api-key") == userKey || r.Header.Get("api-key") == userKey {
				t.Error("tenant credential leaked to configured provider")
			}
			seenMu.Lock()
			seen = append(seen, r.URL.Path)
			seenMu.Unlock()
			var body struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			switch r.URL.Path {
			case prefix + "/chat/completions":
				if body.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
				}
			case "/api/anthropic/v1/messages":
				if r.Header.Get("x-api-key") != providerKey || r.Header.Get("anthropic-version") != "2023-06-01" {
					t.Errorf("Messages credentials/version not set: %+v", r.Header)
				}
				_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":1}}`)
			case "/custom/responses":
				_, _ = io.WriteString(w, `{"output":[],"usage":{"input_tokens":3,"output_tokens":1}}`)
			default:
				http.NotFound(w, r)
			}
		}))
	}
	first := providerServer("provider-a-secret", "/v1")
	defer first.Close()
	glm := providerServer("provider-glm-secret", "/api/coding/paas/v4")
	defer glm.Close()
	providers := map[string]client.RouteResult{
		"model-a": {ProviderID: 1, BaseURL: first.URL + "/v1", APIKey: "provider-a-secret"},
		"glm-5.3": {ProviderID: 3, BaseURL: glm.URL + "/api/coding/paas/v4", APIKey: "provider-glm-secret", ResponsesBaseURL: glm.URL + "/custom"},
	}
	routerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/report" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		provider, ok := providers[r.URL.Query().Get("model")]
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(provider)
	}))
	defer routerServer.Close()
	routerClient := client.NewRouter(routerServer.URL, "loopback-control-token")
	bill := &fakeBilling{}
	log := slog.New(slog.DiscardHandler)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.APIKey(authClient))
	engine.Use(func(c *gin.Context) { c.Set("request_id", c.GetHeader("X-Request-Id")); c.Next() })
	engine.POST("/v1/chat/completions", Chat(bill, routerClient, log, time.Second))
	engine.POST("/anthropic/v1/messages", Native(bill, routerClient, log, "messages", true))
	engine.POST("/v1/responses", Native(bill, routerClient, log, "responses", true))

	tests := []struct {
		name, path, model, authHeader string
		stream                        bool
	}{
		{"first-model", "/v1/chat/completions", "model-a", "Authorization", false},
		{"glm-chat", "/v1/chat/completions", "glm-5.3", "Authorization", false},
		{"glm-stream", "/v1/chat/completions", "glm-5.3", "Authorization", true},
		{"glm-messages", "/anthropic/v1/messages", "glm-5.3", "x-api-key", false},
		{"glm-responses", "/v1/responses", "glm-5.3", "api-key", false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"input":"hi","max_tokens":8,"stream":%t}`, test.model, test.stream)
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(body))
			credential := userKey
			if test.authHeader == "Authorization" {
				credential = "Bearer " + credential
			}
			req.Header.Set(test.authHeader, credential)
			req.Header.Set("X-Request-Id", fmt.Sprintf("multi-%d", index))
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if len(bill.settles) != index+1 {
				t.Fatalf("settles=%d after %s, body=%s", len(bill.settles), test.name, response.Body.String())
			}
			if len(bill.reserves) != index+1 || bill.reserves[index].TenantId != 42 || bill.reserves[index].ApiKeyId != 7 || bill.reserves[index].Model != test.model {
				t.Fatalf("reserve = %+v", bill.reserves)
			}
		})
	}
	if len(bill.settles) != len(tests) || len(bill.reverses) != 0 {
		t.Fatalf("settled=%d reversed=%d", len(bill.settles), len(bill.reverses))
	}
	bad := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`))
	bad.Header.Set("Authorization", "Bearer wrong-key")
	badResponse := httptest.NewRecorder()
	engine.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusUnauthorized || len(bill.reserves) != len(tests) {
		t.Fatalf("invalid key status=%d reserves=%d", badResponse.Code, len(bill.reserves))
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	want := []string{"/v1/chat/completions", "/api/coding/paas/v4/chat/completions", "/api/coding/paas/v4/chat/completions", "/api/anthropic/v1/messages", "/custom/responses"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("upstream paths = %v, want %v", seen, want)
	}
}

func TestUpstreamRedirectDoesNotForwardProviderCredentials(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, protocol := range []string{"chat", "messages"} {
			for _, redirectStatus := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
				t.Run(fmt.Sprintf("%s/stream=%t/status=%d", protocol, stream, redirectStatus), func(t *testing.T) {
					var destinationMu sync.Mutex
					destinationCalls := 0
					destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						destinationMu.Lock()
						destinationCalls++
						destinationMu.Unlock()
						w.WriteHeader(http.StatusUnauthorized)
					}))
					defer destination.Close()
					redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("Authorization") != "Bearer provider-private-key" {
							t.Errorf("provider credentials missing from initial request")
						}
						if protocol == "messages" && r.Header.Get("x-api-key") != "provider-private-key" {
							t.Errorf("Messages credentials missing from initial request")
						}
						w.Header().Set("Location", destination.URL+"/credential-collector")
						w.WriteHeader(redirectStatus)
					}))
					defer redirector.Close()
					bill := &fakeBilling{}
					router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 3, BaseURL: redirector.URL, APIKey: "provider-private-key"}}}
					c, response := newGinCtx()
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}],"max_tokens":8,"stream":%t}`, stream)))
					c.Set("request_id", "redirect-credential-test")
					log := slog.New(slog.DiscardHandler)
					if protocol == "messages" {
						Native(bill, router, log, "messages", true)(c)
					} else {
						Chat(bill, router, log, time.Second)(c)
					}
					destinationMu.Lock()
					defer destinationMu.Unlock()
					if destinationCalls != 0 {
						t.Fatalf("redirect destination received %d requests, gateway status %d", destinationCalls, response.Code)
					}
					if len(bill.settles) != 0 || len(bill.reverses) != 1 {
						t.Fatalf("redirect settlement=%d reversal=%d", len(bill.settles), len(bill.reverses))
					}
				})
			}
		}
	}
}
