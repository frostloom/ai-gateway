package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
)

const discoveryMaxBytes = 2 << 20
const discoveryMaxModels = 1000

type providerDiscoveryRequest struct {
	ProviderID  uint64 `json:"provider_id,omitempty"`
	Name        string `json:"name,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	UpstreamKey string `json:"upstream_key,omitempty"`
}

type discoveredModel struct {
	ModelID         string `json:"model_id"`
	Name            string `json:"name"`
	Vendor          string `json:"vendor"`
	ContextLen      int    `json:"context_len,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	MetadataSource  string `json:"metadata_source"`
	Configured      bool   `json:"configured"`
	Exists          bool   `json:"exists"`
	Status          *int8  `json:"status,omitempty"`
}

type providerDiscoveryResult struct {
	OK        bool              `json:"ok"`
	LatencyMS int64             `json:"latency_ms"`
	Endpoint  string            `json:"endpoint"`
	Models    []discoveredModel `json:"models"`
	Warnings  []string          `json:"warnings,omitempty"`
}

func discoveryClient() *http.Client {
	return &http.Client{Timeout: 9 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Errors intentionally omit response bodies, URLs and transport error text, all of
// which may contain credentials reflected by an untrusted upstream.
func fetchDiscoveredModels(ctx context.Context, client *http.Client, base, key string) (*providerDiscoveryResult, error) {
	endpoint, err := upstream.ModelsURL(base)
	if err != nil {
		return nil, errors.New("invalid upstream base URL")
	}
	if strings.TrimSpace(key) == "" || key == "sk-***" {
		return nil, errors.New("upstream key is required")
	}
	if strings.Contains(endpoint, key) {
		return nil, errors.New("upstream URL must not contain its API key")
	}
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid upstream request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("upstream connection failed or timed out")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream models endpoint returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, discoveryMaxBytes+1))
	if err != nil {
		return nil, errors.New("could not read upstream models response")
	}
	if len(data) > discoveryMaxBytes {
		return nil, errors.New("upstream models response exceeds 2 MiB")
	}
	models, err := parseDiscoveredModels(data)
	if err != nil {
		return nil, err
	}
	// A malicious endpoint can reflect the key in an otherwise valid model field.
	for _, model := range models {
		if strings.Contains(model.ModelID, key) || strings.Contains(model.Name, key) || strings.Contains(model.Vendor, key) {
			return nil, errors.New("upstream returned unsafe model metadata")
		}
	}
	return &providerDiscoveryResult{OK: true, LatencyMS: time.Since(start).Milliseconds(), Endpoint: endpoint, Models: models}, nil
}

func metadataInt(row map[string]any, names ...string) int {
	for _, name := range names {
		value, ok := row[name]
		if !ok {
			continue
		}
		raw := ""
		switch v := value.(type) {
		case json.Number:
			raw = string(v)
		case string:
			raw = v
		}
		n, err := strconv.ParseInt(raw, 10, 32)
		if err == nil && n > 0 {
			return int(n)
		}
	}
	return 0
}

func modelString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := row[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func parseDiscoveredModels(raw []byte) ([]discoveredModel, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, errors.New("invalid upstream models JSON")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid upstream models JSON")
	}
	if obj, ok := root.(map[string]any); ok {
		if data, exists := obj["data"]; exists {
			root = data
		} else {
			root = obj["models"]
		}
	}
	rows, ok := root.([]any)
	if !ok {
		return nil, errors.New("upstream response has no models array")
	}
	if len(rows) > discoveryMaxModels {
		return nil, errors.New("upstream response exceeds 1000 models")
	}
	models := make([]discoveredModel, 0, len(rows))
	seen := map[string]bool{}
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if s, isString := value.(string); isString {
			row = map[string]any{"id": s}
			ok = true
		}
		if !ok {
			return nil, errors.New("invalid upstream model entry")
		}
		id := modelString(row, "id", "model_id", "name")
		if id == "" || len(id) > 512 || strings.ContainsAny(id, "\r\n\x00") {
			return nil, errors.New("invalid upstream model identifier")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		name := modelString(row, "display_name", "name")
		if name == "" {
			name = id
		}
		vendor := modelString(row, "owned_by", "vendor", "provider")
		if vendor == "" {
			vendor = "Unknown"
		}
		m := discoveredModel{ModelID: id, Name: name, Vendor: vendor, MetadataSource: "unknown"}
		m.ContextLen = metadataInt(row, "context_window", "context_length", "context_len", "max_model_len")
		m.MaxOutputTokens = metadataInt(row, "max_output_tokens", "max_completion_tokens")
		if top, ok := row["top_provider"].(map[string]any); ok {
			if m.ContextLen == 0 {
				m.ContextLen = metadataInt(top, "context_length", "context_window")
			}
			if m.MaxOutputTokens == 0 {
				m.MaxOutputTokens = metadataInt(top, "max_completion_tokens", "max_output_tokens")
			}
		}
		if m.ContextLen > 0 || m.MaxOutputTokens > 0 {
			m.MetadataSource = "upstream"
		}
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ModelID < models[j].ModelID })
	return models, nil
}

func discoveryProvider(ctx context.Context, st *store.Store, in providerDiscoveryRequest) (*store.Provider, error) {
	p := &store.Provider{}
	var err error
	if in.ProviderID != 0 {
		p, err = st.GetProviderForDiscovery(ctx, in.ProviderID)
	} else if in.Name != "" {
		var saved *store.Provider
		saved, err = st.ProviderByName(ctx, in.Name)
		if saved != nil {
			p = saved
		}
	}
	if err != nil {
		return nil, errors.New("channel lookup failed")
	}
	if p.Status == store.ProviderDeleted {
		return nil, errors.New("channel was deleted")
	}
	draft := *p
	if in.BaseURL != "" {
		draft.BaseURL = in.BaseURL
	}
	if in.UpstreamKey != "" && in.UpstreamKey != "sk-***" {
		draft.UpstreamKey = in.UpstreamKey
	}
	return &draft, nil
}

func decodeDiscoveryBody(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(out); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if dec.Decode(new(any)) != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}

func registerProviderDiscovery(mux *http.ServeMux, st *store.Store, log *slog.Logger) {
	mux.HandleFunc("/admin/providers/discover", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in providerDiscoveryRequest
		if !decodeDiscoveryBody(w, r, &in) {
			return
		}
		p, err := discoveryProvider(r.Context(), st, in)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := fetchDiscoveredModels(r.Context(), discoveryClient(), p.BaseURL, p.UpstreamKey)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		catalog, err := st.ListModels(r.Context(), "", nil)
		if err != nil {
			log.Error("discovery catalog lookup failed")
			writeErr(w, http.StatusInternalServerError, "catalog lookup failed")
			return
		}
		byID := make(map[string]store.Model, len(catalog))
		for _, m := range catalog {
			byID[m.ModelID] = m
		}
		var configured []string
		_ = json.Unmarshal([]byte(p.Models), &configured)
		configuredIDs := map[string]bool{}
		for _, id := range configured {
			configuredIDs[id] = true
		}
		for i := range result.Models {
			m := &result.Models[i]
			m.Configured = configuredIDs[m.ModelID] || configuredIDs["*"]
			if saved, ok := byID[m.ModelID]; ok {
				m.Exists = true
				status := saved.Status
				m.Status = &status
				if m.ContextLen == 0 && saved.ContextLen > 0 {
					m.ContextLen = saved.ContextLen
					m.MetadataSource = "catalog"
				}
			}
		}
		if len(result.Models) == 0 {
			result.Warnings = []string{"Upstream returned an empty model list"}
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/admin/models/import", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			ProviderID uint64                  `json:"provider_id"`
			Items      []store.ModelImportItem `json:"items"`
		}
		if !decodeDiscoveryBody(w, r, &in) {
			return
		}
		if in.ProviderID == 0 {
			writeErr(w, http.StatusBadRequest, "saved channel is required")
			return
		}
		if err := store.ValidateModelImport(in.Items); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		p, err := st.GetProviderForDiscovery(r.Context(), in.ProviderID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "channel not found")
			return
		}
		if p.Status != 0 {
			writeErr(w, http.StatusBadRequest, "channel must be active to import models")
			return
		}
		allowed := map[string]bool{}
		discovery, discoveryErr := fetchDiscoveredModels(r.Context(), discoveryClient(), p.BaseURL, p.UpstreamKey)
		if discoveryErr == nil {
			for _, m := range discovery.Models {
				allowed[m.ModelID] = true
			}
		}
		// Explicit saved IDs remain usable when a vendor does not expose discovery.
		// The store rechecks this allowlist under the channel row lock.
		result, err := st.ImportProviderModels(r.Context(), *p, in.Items, allowed)
		if err != nil {
			if errors.Is(err, store.ErrProviderChanged) {
				writeErr(w, http.StatusConflict, err.Error())
				return
			}
			log.Error("model import failed")
			writeErr(w, http.StatusBadRequest, "model import failed; verify channel, confirmed model IDs and explicit prices")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
