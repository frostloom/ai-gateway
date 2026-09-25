package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Router struct {
	baseURL string
	cli     *http.Client
}

type RouteResult struct {
	ProviderID uint64 `json:"provider_id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"` // 上游 API key（真实渠道鉴权；空 = mock 透传租户 key）
}

func NewRouter(baseURL string) *Router {
	// 必须显式给大连接池：默认 transport 的 MaxIdleConnsPerHost=2，高并发下每个 /route
	// 请求都会新建 TCP 连接，TIME_WAIT 堆积打爆 Windows 临时端口（压测 300 并发复现）。
	return &Router{
		baseURL: baseURL,
		cli: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        2048,
				MaxIdleConnsPerHost: 1024,
			},
		},
	}
}

// Route 查该模型可用 provider。返回 provider 或 (nil, nil) 表示无可用。
// exclude 是本请求已尝试失败的 provider id（failover 用），可空。
func (r *Router) Route(ctx context.Context, model string, exclude ...uint64) (*RouteResult, error) {
	u := fmt.Sprintf("%s/route?model=%s", r.baseURL, url.QueryEscape(model))
	if len(exclude) > 0 {
		ids := make([]string, 0, len(exclude))
		for _, id := range exclude {
			ids = append(ids, strconv.FormatUint(id, 10))
		}
		u += "&exclude=" + strings.Join(ids, ",")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, nil // 无可用 provider
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router status %d", resp.StatusCode)
	}
	var rr RouteResult
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, err
	}
	// 读完剩余 body 再返回：未读到 EOF 的连接不会回池（http.Transport 规则），
	// 高并发下每个 /route 都新建 TCP 连接 → TIME_WAIT 堆积（压测复现）。
	io.Copy(io.Discard, resp.Body)
	return &rr, nil
}

// Report 回传一次上游调用成败（驱动 router 熔断/AutoBan）。失败不影响主流程，只记日志。
func (r *Router) Report(ctx context.Context, providerID uint64, ok bool) {
	body, _ := json.Marshal(map[string]any{"provider_id": providerID, "ok": ok})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/report", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.cli.Do(req)
	if err != nil {
		return
	}
	// 关键：必须先读完 Body 再 Close，否则连接不会回池，每个 /report 都新建 TCP 连接。
	// 这是压测 368 req/s 打爆 Windows 临时端口的元凶之一（一次请求一次 report，60s 内
	// 8876 个 TIME_WAIT → Only one usage of each socket address）。
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
