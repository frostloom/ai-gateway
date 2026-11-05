package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
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
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"`)
		chunk := strings.Repeat("x", 8192)
		for i := 0; i < 256; i++ {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "\"}}]}\n\ndata: [DONE]\n\n")
		w.(http.Flusher).Flush()
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		panic(err)
	}
}
