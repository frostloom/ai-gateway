// Fixed router for isolated performance diagnosis only. No circuit breaker.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
)

func main() {
	port, up, id, token := os.Getenv("ROUTER_PORT"), os.Getenv("TEST_MOCK_PORT"), os.Getenv("TEST_PROVIDER_ID"), os.Getenv("CONTROL_PLANE_TOKEN")
	n, e := strconv.ParseUint(id, 10, 64)
	if e != nil || n == 0 || port == "" || up == "" || token != "isolated-test-token" {
		log.Fatal("isolated parameters required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		if !controlplane.Valid(r.Header.Get(controlplane.Header), token) {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Query().Get("model") != "test-model" {
			http.Error(w, "no provider", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"provider_id": n, "name": "isolated-mock", "base_url": "http://127.0.0.1:" + up, "api_key": ""})
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		if !controlplane.Valid(r.Header.Get(controlplane.Header), token) {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	ln, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if e != nil {
		log.Fatal(e)
	}
	fmt.Println("fixed test-only router ready")
	log.Fatal(http.Serve(ln, mux))
}
