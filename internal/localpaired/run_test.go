package localpaired

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRefusesPaidWork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := Run(context.Background(), Options{
		AllowPaid:     false,
		ModelEndpoint: server.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("err = %v, want text containing %q", err, "not started")
	}
	if calls != 0 {
		t.Fatalf("model requests = %d, want 0", calls)
	}
}
