// Package admin billing 的观测 + 自服务 HTTP 端点（Web 面板数据源 / agent 与 portal 的操作面）。
// 与 gRPC 控制面解耦：gRPC 管计费 Saga（Reserve/Settle），这里管自服务领域（充值/订阅）。
// 鉴权由 gateway/agent 统一把关；本端口仅本机/内网可达，租户隔离由调用方强制。
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/frostloom/ai-gateway/internal/billing/store"
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
func Handler(st *store.Store, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// 管理员认证（newapi 式初始登录，HttpOnly cookie 保持会话）。
	registerAuth(mux, st, log)

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
		case http.MethodPost:
			var in struct {
				Name        string `json:"name"`
				BaseURL     string `json:"base_url"`
				UpstreamKey string `json:"upstream_key"`
				Models      string `json:"models"` // JSON 数组串，如 ["deepseek-chat"] 或 ["*"]
				Weight      int    `json:"weight"`
				Status      int8   `json:"status"`
				CostInCent  int64  `json:"cost_in_cent"`  // 上游成本：输入 分/百万 token
				CostOutCent int64  `json:"cost_out_cent"` // 上游成本：输出 分/百万 token
			}
			if !parseBody(w, r, &in) {
				return
			}
			if in.Name == "" || in.BaseURL == "" {
				writeErr(w, http.StatusBadRequest, "name and base_url required")
				return
			}
			p := &store.Provider{Name: in.Name, BaseURL: in.BaseURL, UpstreamKey: in.UpstreamKey,
				Models: in.Models, Weight: in.Weight, Status: in.Status, CostInCent: in.CostInCent, CostOutCent: in.CostOutCent}
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
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
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

	mux.HandleFunc("/admin/recharge", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			Money    int64  `json:"amount_money"` // 模拟元
			IdemKey  string `json:"idem_key"`     // 可选：幂等锚点（agent 执行重试用）
		}
		if !parseBody(w, r, &in) {
			return
		}
		o, err := st.CreateRechargeOrder(r.Context(), in.TenantID, in.Money, in.IdemKey)
		if err != nil {
			log.Error("create recharge", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, o)
	})

	mux.HandleFunc("/admin/recharge/pay", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			OrderNo string `json:"order_no"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		o, err := st.PayRecharge(r.Context(), in.OrderNo)
		if err != nil {
			log.Error("pay recharge", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, o)
	})

	// 退款两种方式：order_no 按订单退（min(订单额度,余额)）；amount_money 按金额退（min(金额,余额)）。
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

	mux.HandleFunc("/admin/subscribe", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			PlanID   uint64 `json:"plan_id"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		sub, err := st.SubscribePlan(r.Context(), in.TenantID, in.PlanID)
		if err != nil {
			log.Error("subscribe", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sub)
	})

	mux.HandleFunc("/admin/change-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			PlanID   uint64 `json:"plan_id"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if err := st.ChangePlan(r.Context(), in.TenantID, in.PlanID); err != nil {
			log.Error("change plan", "err", err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

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
			rows, err := st.ListModels(r.Context(), q.Get("vendor"), status)
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
			InputPriceCent  int64    `json:"input_price_cent"`
			OutputPriceCent int64    `json:"output_price_cent"`
			ContextLen      int      `json:"context_len"`
			Tags            []string `json:"tags"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if in.ModelID == "" || in.Vendor == "" {
			writeErr(w, http.StatusBadRequest, "model_id 与 vendor 必填")
			return
		}
		m := &store.Model{
			Vendor: in.Vendor, ModelID: in.ModelID, Name: in.Name,
			InputPriceCent: in.InputPriceCent, OutputPriceCent: in.OutputPriceCent,
			ContextLen: in.ContextLen,
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
		rows, err := st.ListModels(r.Context(), r.URL.Query().Get("vendor"), &enabledStatus)
		if err != nil {
			log.Error("portal models", "err", err)
			writeErr(w, http.StatusInternalServerError, "portal models failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
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

	return mux
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
