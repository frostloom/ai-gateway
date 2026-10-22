package jev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluationClientRejectsRedirectWithoutRetry(t *testing.T) {
	calls, destination := 0, 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destination++ }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, err := NewEvaluationClient(source.URL, "test-key", "pinned", http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Evaluate(context.Background(), "state", map[string]Question{"legit": {Type: QNoul}}); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 || destination != 0 {
		t.Fatalf("redirect or retry sent: %d %d", calls, destination)
	}
}
