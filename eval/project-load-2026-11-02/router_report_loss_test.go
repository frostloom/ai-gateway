package loadtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
)

// Characterizes the actual gateway client's current response handling.
func TestGatewayReportDoesNotRetryHTTP503(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	router := client.NewRouter(server.URL, "test-only-token")
	router.Report(context.Background(), 1, false)
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want exactly one", calls.Load())
	}
}
