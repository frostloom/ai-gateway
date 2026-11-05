package comparison

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
)

func TestAdminChatRejectsTruncatedResponse(t *testing.T) {
	var mode atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat" || r.Header.Get("X-Agent-Internal") != "synthetic" || r.Header.Get("X-Agent-Tenant") != "42" {
			http.Error(w, "bad forwarding", http.StatusBadRequest)
			return
		}
		switch mode.Load() {
		case 0:
			w.Header().Set("Content-Length", "80")
			_, _ = w.Write([]byte(`{"reply":`))
		case 1:
			_, _ = w.Write([]byte(`{"reply":"ok"}`))
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
		}
	}))
	defer upstream.Close()
	router := gin.New()
	handler.RegisterAdminChat(router.Group("/admin"), upstream.URL, "synthetic", 1, slog.New(slog.DiscardHandler))
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	send := func() (int, string) {
		t.Helper()
		resp, err := http.Post(gateway.URL+"/admin/chat", "application/json", strings.NewReader(`{"tenant_id":42}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body)
	}
	status, body := send()
	if status != http.StatusBadGateway || strings.Contains(body, `{"reply":`) {
		t.Fatalf("truncated: status=%d body=%q", status, body)
	}
	mode.Store(1)
	status, body = send()
	if status != http.StatusOK || body != `{"reply":"ok"}` {
		t.Fatalf("normal: status=%d body=%q", status, body)
	}
	mode.Store(2)
	status, body = send()
	if status != http.StatusServiceUnavailable || body != `{"error":"unavailable"}` {
		t.Fatalf("error: status=%d body=%q", status, body)
	}
}
