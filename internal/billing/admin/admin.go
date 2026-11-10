// Package admin billing 的观测 + 自服务 HTTP 端点（Web 面板数据源 / agent 与 portal 的操作面）。
// 与 gRPC 控制面解耦：gRPC 管计费 Saga（Reserve/Settle），这里管自服务领域（充值/订阅）。
// 除 /healthz、/metrics 外，请求必须带控制面令牌。租户隔离仍由调用方强制。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
)

// maskKey 渠道 key 脱敏：列表/响应只标记「已配置」，不回显明文。
// demo 明文入库，但任何对外响应都不应吐出 key；生产接 KMS 后这里可返回引用标识。
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	return "sk-***"
}

// Handler 返回 billing 观测 + 自服务端点的 http.Handler。
// token 是控制面共享令牌。除 /healthz 和 /metrics 外，请求必须带 X-Control-Token。
func Handler(st *store.Store, log *slog.Logger, token string) http.Handler {
	mux := http.NewServeMux()
	registerVouchers(mux, st, log)
	registerProviderDiscovery(mux, st, log)

	// 管理员认证（newapi 式初始登录，HttpOnly cookie 保持会话）。
	registerAuth(mux, st, log)
	registerPortalAuth(mux, st, log)
	registerUnifiedLogin(mux, st)

	// Prometheus 抓取（outbox relay produced / 消费 lag 等计费指标）。
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/admin/overview", func(w http.ResponseWriter, r *http.Request) {
		o, err := st.OverviewStats(r.Context())
		if err != nil {
			log.Error("overview", "err", err)
			writeErr(w, http.StatusInternalServerError, "overview failed")
			return
		}
		writeJSON(w, http.StatusOK, o)
	})

	mux.HandleFunc("/admin/pending-summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		summary, err := st.PendingReserveSummary(ctx)
		if err != nil {
			log.Error("pending summary", "err", err)
			writeErr(w, http.StatusInternalServerError, "pending summary failed")
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})

	mux.HandleFunc("/admin/bills", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		tenantID, _ := strconv.ParseUint(q.Get("tenant_id"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		rows, total, err := st.ListBills(r.Context(), tenantID, q.Get("phase"), q.Get("status"), limit, offset)
		if err != nil {
			log.Error("list bills", "err", err)
			writeErr(w, http.StatusInternalServerError, "list bills failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": rows})
	})

	// ---------- M7 自服务：只读查询 ----------

	// /admin/me 用户视角一次拉取（portal 页 + agent 读工具共用）。
	mux.HandleFunc("/admin/me", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		v, err := st.MeOverview(r.Context(), tenantID)
		if err != nil {
			log.Error("me", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "me failed")
			return
		}
		writeJSON(w, http.StatusOK, v)
	})

	mux.HandleFunc("/admin/plans", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var plan store.Plan
			if !parseBody(w, r, &plan) {
				return
			}
			if err := st.SaveConsolePlan(r.Context(), &plan); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, plan)
			return
		}
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET or POST required")
			return
		}
		rows, err := st.ListPlans(r.Context())
		if err != nil {
			log.Error("list plans", "err", err)
			writeErr(w, http.StatusInternalServerError, "list plans failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
	})

	// ---------- M10 渠道管理（聚合 API 式：上游 base_url + key + 成本/毛利） ----------

	// GET /admin/providers 渠道列表 + 每渠道累计售价/成本/毛利；
	// POST /admin/providers upsert 渠道（按 name 幂等，重复名更新转发参数）。
	mux.HandleFunc("/admin/providers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			id, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
			if err != nil || id == 0 {
				writeErr(w, http.StatusBadRequest, "invalid channel id")
				return
			}
			if err := st.DeleteProvider(r.Context(), id); err != nil {
				if errors.Is(err, store.ErrProviderNotFound) {
					writeErr(w, http.StatusNotFound, "channel not found")
				} else {
					log.Error("delete channel", "err", err)
					writeErr(w, http.StatusInternalServerError, "delete channel failed")
				}
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		case http.MethodPost:
			var in struct {
				Name             string  `json:"name"`
				BaseURL          string  `json:"base_url"`
				MessagesBaseURL  *string `json:"messages_base_url"`
				ResponsesBaseURL *string `json:"responses_base_url"`
				UpstreamKey      string  `json:"upstream_key"`
				Models           string  `json:"models"` // JSON 数组串，如 ["deepseek-chat"] 或 ["*"]
				Weight           int     `json:"weight"`
				Status           int8    `json:"status"`
				CostInCent       int64   `json:"cost_in_cent"`  // 上游成本：输入 分/百万 token
				CostOutCent      int64   `json:"cost_out_cent"` // 上游成本：输出 分/百万 token
			}
			if !parseBody(w, r, &in) {
				return
			}
			if in.Name == "" || in.BaseURL == "" {
				writeErr(w, http.StatusBadRequest, "name and base_url required")
				return
			}
			u, urlErr := url.Parse(in.BaseURL)
			var modelNames []string
			if urlErr != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || in.Weight < 1 || in.Weight > 1000000 || in.CostInCent < 0 || in.CostOutCent < 0 || in.Status < 0 || in.Status > 2 || json.Unmarshal([]byte(in.Models), &modelNames) != nil || len(modelNames) == 0 {
				writeErr(w, http.StatusBadRequest, "请检查上游 HTTP(S) 地址、模型数组、正整数权重、状态与非负成本")
				return
			}
			for _, name := range modelNames {
				if strings.TrimSpace(name) == "" {
					writeErr(w, http.StatusBadRequest, "模型名称不能为空")
					return
				}
			}
			var existing *store.Provider
			if in.UpstreamKey == "" || in.MessagesBaseURL == nil || in.ResponsesBaseURL == nil {
				var err error
				existing, err = st.ProviderByName(r.Context(), in.Name)
				if err != nil {
					writeErr(w, http.StatusInternalServerError, "读取渠道失败")
					return
				}
			}
			if in.UpstreamKey == "" && existing != nil {
				in.UpstreamKey = existing.UpstreamKey
			}
			messagesBase, responsesBase := "", ""
			if existing != nil {
				messagesBase = existing.MessagesBaseURL
				responsesBase = existing.ResponsesBaseURL
			}
			if in.MessagesBaseURL != nil {
				messagesBase = strings.TrimSpace(*in.MessagesBaseURL)
			}
			if in.ResponsesBaseURL != nil {
				responsesBase = strings.TrimSpace(*in.ResponsesBaseURL)
			}
			for _, base := range []string{messagesBase, responsesBase} {
				if base != "" {
					if _, err := upstream.ParseBase(base); err != nil || len(base) > 512 {
						writeErr(w, http.StatusBadRequest, "协议 Base URL 无效")
						return
					}
				}
			}
			p := &store.Provider{Name: in.Name, BaseURL: in.BaseURL, UpstreamKey: in.UpstreamKey, Models: in.Models, Weight: in.Weight, Status: in.Status, CostInCent: in.CostInCent, CostOutCent: in.CostOutCent, MessagesBaseURL: messagesBase, ResponsesBaseURL: responsesBase}

			if err := st.UpsertProvider(r.Context(), p); err != nil {
				log.Error("upsert provider", "name", in.Name, "err", err)
				writeErr(w, http.StatusInternalServerError, "upsert provider failed")
				return
			}
			log.Info("channel upserted", "provider_id", p.ID, "name", in.Name)
			p.UpstreamKey = maskKey(p.UpstreamKey) // 响应不回显明文 key
			writeJSON(w, http.StatusOK, p)
		case http.MethodGet:
			rows, err := st.ListProvidersWithMargin(r.Context())
			if err != nil {
				log.Error("list providers", "err", err)
				writeErr(w, http.StatusInternalServerError, "list providers failed")
				return
			}
			for i := range rows {
				rows[i].UpstreamKey = maskKey(rows[i].UpstreamKey)
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": rows})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/admin/subscription", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		sub, err := st.GetActiveSubscription(r.Context(), tenantID)
		if err != nil {
			log.Error("subscription", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "subscription failed")
			return
		}
		if sub == nil {
			writeJSON(w, http.StatusOK, map[string]any{"subscription": nil})
			return
		}
		// M9：附带各档次每窗口剩余额度（活跃订阅才非空）。
		allowance, err := st.SubscriptionAllowance(r.Context(), sub)
		if err != nil {
			log.Error("subscription allowance", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "subscription allowance failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"subscription": sub, "allowance": allowance})
	})

	mux.HandleFunc("/admin/consumption", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		rows, err := st.ConsumptionStats(r.Context(), tenantID, days)
		if err != nil {
			log.Error("consumption", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "consumption failed")
			return
		}
		byModel, err := st.ConsumptionByModel(r.Context(), tenantID, days)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "consumption models failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "by_model": byModel})
	})

	mux.HandleFunc("/admin/topups", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		rows, err := st.ListTopups(r.Context(), tenantID, 50)
		if err != nil {
			log.Error("topups", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "topups failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
	})

	mux.HandleFunc("/admin/recharge-orders", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		rows, err := st.ListRechargeOrders(r.Context(), tenantID, 50)
		if err != nil {
			log.Error("recharge orders", "tenant", tenantID, "err", err)
			writeErr(w, http.StatusInternalServerError, "recharge orders failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
	})

	// ---------- M7 自服务：写操作 ----------

	// Payment callbacks are not configured. Keep both public and internal
	// recharge entry points closed, regardless of demo environment settings.
	for _, path := range []string{"/admin/recharge", "/admin/recharge/pay"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusForbidden, "充值入口暂时关闭，请使用余额兑换码")
		})
	}

	mux.HandleFunc("/admin/recharge/refund", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID    uint64 `json:"tenant_id"`
			OrderNo     string `json:"order_no"`     // 二选一：按订单退
			AmountMoney int64  `json:"amount_money"` // 二选一：按金额退（元）
			IdemKey     string `json:"idem_key"`     // 按金额退的幂等锚点（可空，服务端自生成）
		}
		if !parseBody(w, r, &in) {
			return
		}
		var res *store.RefundResult
		var err error
		switch {
		case in.OrderNo != "":
			res, err = st.RefundRecharge(r.Context(), in.TenantID, in.OrderNo)
		case in.AmountMoney > 0:
			res, err = st.RefundAmount(r.Context(), in.TenantID, in.AmountMoney, in.IdemKey)
		default:
			writeErr(w, http.StatusBadRequest, "请提供 order_no（按订单退）或 amount_money（按金额退）")
			return
		}
		if err != nil {
			log.Error("refund recharge", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// Plan activation waits for verified payment callbacks. Cancellation remains
	// available so existing subscribers can stop their next renewal.
	for _, path := range []string{"/admin/subscribe", "/admin/change-plan"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusForbidden, "月卡开通与套餐变更尚未开放，真实支付接入后开放")
		})
	}

	mux.HandleFunc("/admin/cancel-subscription", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		res, err := st.CancelSubscription(r.Context(), in.TenantID)
		if err != nil {
			log.Error("cancel subscription", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// ---------- 商品目录（卖方管理 + 买家浏览） ----------

	// GET /admin/models?vendor=&status= 全量（卖方管理）；POST /admin/models 新增 SKU。
	mux.HandleFunc("/admin/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			q := r.URL.Query()
			var status *int8
			if s := q.Get("status"); s != "" {
				if v, err := strconv.ParseInt(s, 10, 8); err == nil {
					st8 := int8(v)
					status = &st8
				}
			}
			rows, err := st.ListConfiguredModels(r.Context(), q.Get("vendor"), status)
			if err != nil {
				log.Error("list models", "err", err)
				writeErr(w, http.StatusInternalServerError, "list models failed")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": rows})
			return
		}
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			Vendor          string   `json:"vendor"`
			ModelID         string   `json:"model_id"`
			Name            string   `json:"name"`
			InputPriceCent  *int64   `json:"input_price_cent"`
			OutputPriceCent *int64   `json:"output_price_cent"`
			ContextLen      int      `json:"context_len"`
			Tags            []string `json:"tags"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		in.ModelID = strings.TrimSpace(in.ModelID)
		in.Vendor = strings.TrimSpace(in.Vendor)
		in.Name = strings.TrimSpace(in.Name)
		if in.ModelID == "" || in.Vendor == "" {
			writeErr(w, http.StatusBadRequest, "model_id 与 vendor 必填")
			return
		}
		if in.InputPriceCent == nil || in.OutputPriceCent == nil || *in.InputPriceCent < 0 || *in.OutputPriceCent < 0 || in.ContextLen < 1 {
			writeErr(w, http.StatusBadRequest, "单价须为非负整数，上下文长度须为正整数")
			return
		}
		configured, err := st.ModelConfigured(r.Context(), in.ModelID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取上游模型失败")
			return
		}
		if !configured {
			writeErr(w, http.StatusBadRequest, "请先在启用的上游渠道中配置此模型编号")
			return
		}
		m := &store.Model{
			Vendor: in.Vendor, ModelID: in.ModelID, Name: in.Name,
			InputPriceCent: *in.InputPriceCent, OutputPriceCent: *in.OutputPriceCent,
			ContextLen: in.ContextLen, Tags: "[]",
		}
		if len(in.Tags) > 0 {
			b, _ := json.Marshal(in.Tags)
			m.Tags = string(b)
		}
		if err := st.CreateModel(r.Context(), m); err != nil {
			log.Error("create model", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m)
	})

	// GET /portal/models?vendor= 买家目录：只列上架。
	mux.HandleFunc("/portal/models", func(w http.ResponseWriter, r *http.Request) {
		rows, err := st.ListSupportedModels(r.Context(), r.URL.Query().Get("vendor"))
		if err != nil {
			log.Error("portal models", "err", err)
			writeErr(w, http.StatusInternalServerError, "portal models failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
	})

	// 下游调用用的钥匙。明文只在创建响应里出现一次。
	mux.HandleFunc("/portal/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tenantID := tenantIDFromQuery(r)
			if tenantID == 0 {
				writeErr(w, http.StatusBadRequest, "缺少账户")
				return
			}
			rows, err := st.ListDownstreamAPIKeys(r.Context(), tenantID)
			if err != nil {
				log.Error("list downstream keys", "err", err)
				writeErr(w, http.StatusInternalServerError, "读取密钥失败")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": rows})
			return
		}
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			Name     string `json:"name"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		plain, row, err := st.CreateDownstreamAPIKey(r.Context(), in.TenantID, in.Name)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": plain, "item": row})
	})
	mux.HandleFunc("/portal/api-keys/delete", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			ID       uint64 `json:"id"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if err := st.DeleteDownstreamAPIKey(r.Context(), in.TenantID, in.ID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/portal/api-keys/status", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			ID       uint64 `json:"id"`
			Status   int8   `json:"status"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if err := st.SetDownstreamAPIKeyStatus(r.Context(), in.TenantID, in.ID, in.Status); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// POST /admin/api-keys/status 停用或启用密钥 {id, status}。停用会立刻清掉缓存。
	mux.HandleFunc("/admin/api-keys/status", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			ID     uint64 `json:"id"`
			Status int8   `json:"status"` // 0=启用 1=停用
		}
		if !parseBody(w, r, &in) {
			return
		}
		if err := st.SetAPIKeyStatus(r.Context(), in.ID, in.Status); err != nil {
			log.Error("update api key status", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// POST /admin/models/status 启停 {id, status}。
	mux.HandleFunc("/admin/models/status", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			ID     uint64 `json:"id"`
			Status int8   `json:"status"` // 0=上架 1=下架
		}
		if !parseBody(w, r, &in) {
			return
		}
		if in.ID == 0 || (in.Status != 0 && in.Status != 1) {
			writeErr(w, http.StatusBadRequest, "模型编号或状态无效")
			return
		}
		if in.Status == 0 {
			models, err := st.ListConfiguredModels(r.Context(), "", nil)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "读取上游模型失败")
				return
			}
			configured := false
			for _, model := range models {
				if model.ID == in.ID {
					configured = true
					break
				}
			}
			if !configured {
				writeErr(w, http.StatusBadRequest, "请先在启用的上游渠道中配置此模型")
				return
			}
		}
		if err := st.UpdateModelStatus(r.Context(), in.ID, in.Status); err != nil {
			log.Error("update model status", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// POST /admin/models/price 改价 {id, input_price_cent, output_price_cent}。
	mux.HandleFunc("/admin/models/price", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			ID              uint64 `json:"id"`
			InputPriceCent  int64  `json:"input_price_cent"`
			OutputPriceCent int64  `json:"output_price_cent"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if in.InputPriceCent < 0 || in.OutputPriceCent < 0 {
			writeErr(w, http.StatusBadRequest, "价格不能为负")
			return
		}
		if err := st.UpdateModelPrice(r.Context(), in.ID, in.InputPriceCent, in.OutputPriceCent); err != nil {
			log.Error("update model price", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// ---------- 销售统计 / 对账 / 审计 / 健康 ----------

	// GET /admin/sales?from=YYYY-MM-DD&to=YYYY-MM-DD 卖方销售统计。
	mux.HandleFunc("/admin/sales", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from := parseDay(q.Get("from"))
		to := parseDay(q.Get("to"))
		if !to.IsZero() {
			to = to.Add(24*time.Hour - time.Second) // 含当天
		}
		stats, err := st.SalesStats(r.Context(), from, to)
		if err != nil {
			log.Error("sales stats", "err", err)
			writeErr(w, http.StatusInternalServerError, "sales stats failed")
			return
		}
		writeJSON(w, http.StatusOK, stats)
	})

	// GET /admin/reconcile-check 账本(MySQL) vs 投影(Redis) 一致性。
	mux.HandleFunc("/admin/reconcile-check", func(w http.ResponseWriter, r *http.Request) {
		o, err := st.OverviewStats(r.Context())
		if err != nil {
			log.Error("reconcile-check", "err", err)
			writeErr(w, http.StatusInternalServerError, "reconcile-check failed")
			return
		}
		type bad struct {
			ID           uint64 `json:"id"`
			Name         string `json:"name"`
			Ledger       int64  `json:"ledger_cent"`
			RedisBalance int64  `json:"redis_cent"`
		}
		var inconsistent []bad
		for _, t := range o.Tenants {
			if !t.Consistent {
				inconsistent = append(inconsistent, bad{t.ID, t.Name, t.Balance, t.RedisBalance})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"checked":      len(o.Tenants),
			"inconsistent": inconsistent,
			"ok":           len(inconsistent) == 0,
		})
	})

	// GET /admin/audit?limit= 客服审计轨迹（tenant_id=0 全部）。
	mux.HandleFunc("/admin/audit", func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromQuery(r)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		rows, err := st.ListAuditLogs(r.Context(), tenantID, limit)
		if err != nil {
			log.Error("audit logs", "err", err)
			writeErr(w, http.StatusInternalServerError, "audit failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
	})

	// GET /healthz 存活探针（服务编排/负载均衡用）。
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "svc": "billing"})
	})

	return controlplane.WrapHTTP(token, map[string]struct{}{
		"/healthz": {},
		"/metrics": {},
	}, mux)
}

// enabledStatus 上架状态（status=0），portal 买家目录只列上架。
var enabledStatus int8

// parseDay 解析 YYYY-MM-DD 为当天零点；空/非法返回零值。
func parseDay(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseBody 解析 JSON body（限 64KB）。失败时已写 400 响应，返回 false。
func parseBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json body: "+err.Error())
		return false
	}
	return true
}

// requirePOST 非 POST 返回 405。
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	return true
}

func tenantIDFromQuery(r *http.Request) uint64 {
	id, _ := strconv.ParseUint(r.URL.Query().Get("tenant_id"), 10, 64)
	return id
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
