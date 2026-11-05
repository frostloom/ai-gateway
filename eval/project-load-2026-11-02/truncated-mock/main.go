package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync/atomic"
)

func main() {
	port := os.Getenv("MOCK_PROVIDER_PORT")
	if port == "" {
		panic("missing port")
	}
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/count", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"calls": calls.Load()})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"partial`)
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		panic(err)
	}
}
