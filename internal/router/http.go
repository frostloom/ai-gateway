package router

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
)

// NewHTTPHandler 暴露选路、熔断回报和观测端点。
// /route 与 /report 需要控制面令牌：前者会返回上游 key，后者能拨动熔断。
func NewHTTPHandler(reg *Registry, token string, log *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		if !controlplane.Valid(r.Header.Get(controlplane.Header), token) {
			http.Error(w, `{"error":"control plane token required"}`, http.StatusUnauthorized)
			return
		}
		model := r.URL.Query().Get("model")
		if model == "" {
			http.Error(w, `{"error":"model required"}`, http.StatusBadRequest)
			return
		}
		var exclude map[uint64]bool
		if q := r.URL.Query().Get("exclude"); q != "" {
			exclude = make(map[uint64]bool)
			for _, s := range strings.Split(q, ",") {
				if id, err := strconv.ParseUint(s, 10, 64); err == nil {
					exclude[id] = true
				}
			}
		}
		prov, err := reg.RouteExclude(model, exclude)
		if err != nil {
			log.Error("route", "model", model, "err", err)
			http.Error(w, `{"error":"route failed"}`, http.StatusInternalServerError)
			return
		}
		if prov == nil {
			http.Error(w, `{"error":"no available provider for model"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider_id": prov.ID,
			"name":        prov.Name,
			"base_url":    prov.BaseURL,
			"api_key":     prov.UpstreamKey,
		})
		log.Info("route", "model", model, "provider", prov.Name, slog.Int64("provider_id", int64(prov.ID)))
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		if !controlplane.Valid(r.Header.Get(controlplane.Header), token) {
			http.Error(w, `{"error":"control plane token required"}`, http.StatusUnauthorized)
			return
		}
		var body struct {
			ProviderID uint64 `json:"provider_id"`
			OK         bool   `json:"ok"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad report"}`, http.StatusBadRequest)
			return
		}
		reg.Report(body.ProviderID, body.OK)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
		log.Info("report", "provider_id", body.ProviderID, "ok", body.OK)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"router"}`))
	})
	mux.HandleFunc("/breakers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.States())
	})
	mux.HandleFunc("/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.Providers())
	})
	return mux
}
