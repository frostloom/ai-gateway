package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
)

type listedModel struct {
	ID      string
	OwnedBy string
}

func OpenAIModels(adminBase, token string) gin.HandlerFunc {
	hc := &http.Client{Timeout: 3 * time.Second}
	return func(c *gin.Context) {
		models, err := listRoutableModels(c.Request.Context(), hc, adminBase, token)
		if err != nil {
			abort(c, http.StatusBadGateway, "model list unavailable")
			return
		}
		if strings.Contains(c.Request.URL.Path, "/anthropic/") {
			data := make([]gin.H, 0, len(models))
			for _, model := range models {
				data = append(data, gin.H{
					"type":         "model",
					"id":           model.ID,
					"display_name": model.ID,
					"created_at":   "1970-01-01T00:00:00Z",
				})
			}
			first, last := "", ""
			if len(models) > 0 {
				first = models[0].ID
				last = models[len(models)-1].ID
			}
			c.JSON(http.StatusOK, gin.H{
				"data":     data,
				"has_more": false,
				"first_id": first,
				"last_id":  last,
			})
			return
		}
		data := make([]gin.H, 0, len(models))
		for _, model := range models {
			data = append(data, gin.H{
				"id":       model.ID,
				"object":   "model",
				"created":  0,
				"owned_by": model.OwnedBy,
			})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	}
}

func listRoutableModels(ctx context.Context, hc *http.Client, adminBase, token string) ([]listedModel, error) {
	raw, err := controlGet(ctx, hc, strings.TrimRight(adminBase, "/")+"/admin/providers", token)
	if err != nil {
		return nil, err
	}
	var providers struct {
		Items []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			Weight  int    `json:"weight"`
			Status  int8   `json:"status"`
			Models  string `json:"models"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, err
	}
	seen := map[string]string{}
	var order []string
	add := func(id, owner string) {
		if id == "" || id == "*" || strings.TrimSpace(id) != id {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = owner
		order = append(order, id)
	}
	for _, item := range providers.Items {
		if item.Status != 0 || item.Weight <= 0 || item.Weight > 1000000 || strings.TrimSpace(item.BaseURL) == "" {
			continue
		}
		var names []string
		if json.Unmarshal([]byte(item.Models), &names) != nil {
			continue
		}
		for _, name := range names {
			add(name, item.Name)
		}
	}
	// A configured route is public only after its model has published pricing.
	catalog, err := controlGet(ctx, hc, strings.TrimRight(adminBase, "/")+"/portal/models", token)
	if err != nil {
		return nil, err
	}
	var rows struct {
		Items []struct {
			ModelID string `json:"model_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(catalog, &rows); err != nil {
		return nil, err
	}
	priced := make(map[string]bool, len(rows.Items))
	for _, row := range rows.Items {
		priced[row.ModelID] = true
	}

	sort.Strings(order)
	out := make([]listedModel, 0, len(order))
	for _, id := range order {
		if !priced[id] {
			continue
		}
		out = append(out, listedModel{ID: id, OwnedBy: seen[id]})
	}
	return out, nil
}

func controlGet(ctx context.Context, hc *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set(controlplane.Header, token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, errStatus(resp.StatusCode)
	}
	return body, nil
}

type errStatus int

func (e errStatus) Error() string { return http.StatusText(int(e)) }
