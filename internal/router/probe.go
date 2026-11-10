package router

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
	"io"
	"net/http"
	"time"
)

var probeClient = &http.Client{Timeout: 5 * time.Second}

// StartProbe 定时探测已停用的上游。探测返回成功才把 MySQL 改回可用。
// 间隔为 0 或没有数据库时不启动。
func (r *Registry) StartProbe(every time.Duration) {
	if every <= 0 || r.db == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for range ticker.C {
			r.ProbeOnce()
		}
	}()
}

// ProbeOnce 查一次 status=停用 的上游。模型列表返回 2xx 才恢复。
func (r *Registry) ProbeOnce() {
	if r.db == nil {
		return
	}
	type row struct {
		ID          uint64
		BaseURL     string
		UpstreamKey string
	}
	var rows []row
	if err := r.db.Table("providers").Select("id", "base_url", "upstream_key").Where("status = ?", 2).Find(&rows).Error; err != nil {
		r.log.Error("list banned providers", "err", err)
		return
	}
	for _, row := range rows {
		if !probeUpstream(row.BaseURL, row.UpstreamKey) {
			continue
		}
		r.enableProvider(row.ID)
	}
}

func probeUpstream(baseURL, key string) bool {
	endpoint, err := upstream.ModelsURL(baseURL)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// enableProvider 把停用改回可用，并清掉这一分钟里残留的失败次数。
func (r *Registry) enableProvider(id uint64) {
	if r.db != nil {
		res := r.db.Table("providers").Where("id = ? AND status = ?", id, 2).
			Updates(map[string]any{"status": 0, "consecutive_fail": 0})
		if res.Error != nil {
			r.log.Error("enable provider", "provider_id", id, "err", res.Error)
			return
		}
		if res.RowsAffected == 0 {
			return
		}
	}
	if r.failRedis != nil {
		ctx, cancel := r.failCtx()
		if err := r.failRedis.Del(ctx, failureKey(id)).Err(); err != nil {
			r.log.Error("clear provider failures", "provider_id", id, "err", err)
		}
		cancel()
	}
	r.mu.Lock()
	if b, ok := r.bk[id]; ok {
		b.banned = false
		b.state = breakerClosed
		b.consecutiveFail = 0
		b.probeInFlight = false
	}
	r.clearCache()
	r.mu.Unlock()
}
