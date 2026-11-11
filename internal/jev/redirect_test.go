package jev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientRejectsRedirectWithCredentials(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client := NewClient(redirect.URL, "private-judge-key", "jev-test")
	_, err := client.Evaluate(context.Background(), "test", map[string]Question{"intent": {Type: QChoice, Criteria: map[string]string{"read": "read"}}})
	failure := FailureDetails(err)
	if err == nil || calls.Load() != 0 || failure.Kind != "http" || failure.HTTPStatus != 307 {
		t.Fatalf("redirect not rejected calls=%d failure=%+v", calls.Load(), failure)
	}
}
