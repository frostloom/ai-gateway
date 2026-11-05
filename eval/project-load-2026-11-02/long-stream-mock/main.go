package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	port := os.Getenv("MOCK_PROVIDER_PORT")
	if port == "" {
		panic("missing port")
	}
	var calls, sent atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/count", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"calls": calls.Load(), "sent": sent.Load()})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		frame := `data: {"choices":[{"delta":{"content":"` + strings.Repeat("x", 8192) + `"}}]}` + "\n\n"
		for i := 0; i < 512; i++ {
			if _, err := io.WriteString(w, frame); err != nil {
				return
			}
		}
		w.(http.Flusher).Flush()
		sent.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(4 * time.Second):
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
		w.(http.Flusher).Flush()
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		panic(err)
	}
}
