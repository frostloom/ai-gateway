// Local-only gated fake upstream for a deterministic idempotency experiment.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
)

func main() {
	port := os.Getenv("MOCK_PROVIDER_PORT")
	if port == "" {
		panic("missing port")
	}
	var calls atomic.Int64
	gate := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/count", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"calls": calls.Load()})
	})
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(gate) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		n := calls.Add(1)
		<-gate
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("synthetic-%d", n), "object": "chat.completion", "choices": []any{}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 10, "total_tokens": 60}})
	})
	if e := http.ListenAndServe("127.0.0.1:"+port, mux); e != nil {
		panic(e)
	}
}
