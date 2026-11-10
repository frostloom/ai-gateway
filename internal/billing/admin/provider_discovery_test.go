package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseDiscoveredModels(t *testing.T) {
	cases := []struct {
		name, body      string
		context, output int
		source          string
	}{
		{"openai", `{"data":[{"id":"test","owned_by":"vendor","context_window":128000,"max_output_tokens":4096}]}`, 128000, 4096, "upstream"},
		{"anthropic", `{"data":[{"id":"test","display_name":"Test","type":"model"}]}`, 0, 0, "unknown"},
		{"strings", `{"models":["test"]}`, 0, 0, "unknown"},
		{"bare", `["test"]`, 0, 0, "unknown"},
		{"nested", `{"data":[{"id":"test","top_provider":{"context_length":200000,"max_completion_tokens":8000}}]}`, 200000, 8000, "upstream"},
		{"string metadata", `{"models":[{"id":"test","max_model_len":"32768"}]}`, 32768, 0, "upstream"},
		{"invalid metadata", `{"models":[{"id":"test","context_len":-1,"max_output_tokens":1.5}]}`, 0, 0, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDiscoveredModels([]byte(tc.body))
			if err != nil || len(got) != 1 {
				t.Fatalf("models=%v err=%v", got, err)
			}
			if got[0].ContextLen != tc.context || got[0].MaxOutputTokens != tc.output || got[0].MetadataSource != tc.source {
				t.Fatalf("metadata=%+v", got[0])
			}
		})
	}
}

func TestParseDiscoveredModelsRejectsMalformed(t *testing.T) {
	for _, body := range []string{`{"error":"no"}`, `{"data":[null]}`, `{"data":[{}]}`, `{"data":[1]}`, `{"data":[]} {}`, `{"data":null}`, `{"data":["bad\nmodel"]}`} {
		if _, err := parseDiscoveredModels([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	rows := make([]string, 1001)
	for i := range rows {
		rows[i] = fmt.Sprint(i)
	}
	body, _ := json.Marshal(rows)
	if _, err := parseDiscoveredModels(body); err == nil {
		t.Fatal("accepted over 1000 models")
	}
}

func TestFetchDiscoveredModels(t *testing.T) {
	const key = "sk-private-discovery-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Errorf("request path/auth mismatch")
		}
		fmt.Fprint(w, `{"data":[{"id":"test","context_length":8192}]}`)
	}))
	defer server.Close()
	got, err := fetchDiscoveredModels(context.Background(), discoveryClient(), server.URL, key)
	if err != nil || len(got.Models) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if got.Endpoint != server.URL+"/v1/models" || got.Models[0].ContextLen != 8192 {
		t.Fatalf("result=%+v", got)
	}
}

func TestFetchDiscoveredModelsSafeFailures(t *testing.T) {
	const key = "sk-private-discovery-key"
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"auth", 401, key}, {"bad json", 200, key}, {"reflected key", 200, `{"data":[{"id":"` + key + `"}]}`}, {"too large", 200, strings.Repeat("x", discoveryMaxBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			_, err := fetchDiscoveredModels(context.Background(), discoveryClient(), s.URL, key)
			if err == nil || strings.Contains(err.Error(), key) {
				t.Fatalf("unsafe error %v", err)
			}
		})
	}
}

func TestFetchDiscoveredModelsRefusesRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	_, err := fetchDiscoveredModels(context.Background(), discoveryClient(), source.URL, "sk-secret")
	if err == nil || called {
		t.Fatalf("redirect followed=%v err=%v", called, err)
	}
}

func TestFetchDiscoveredModelsTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := fetchDiscoveredModels(ctx, discoveryClient(), s.URL, "sk-secret")
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout error=%v duration=%s", err, time.Since(start))
	}
}
