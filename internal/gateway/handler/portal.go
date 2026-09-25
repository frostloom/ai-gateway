// Package handler 的 portal.go：用户自助页接口（/portal/*）。
// 与 admin 面板严格分离：这里只服务「登录了自己的租户 key 的用户」，鉴权走 middleware.APIKey，
// tenant_id 一律由 key 派生（执行层强制，前端传的一律覆盖）。写操作转发 billing admin，读操作同样。
// AI 客服走 agent 第 6 服务（/portal/chat 透传 Authorization，agent 自行 ValidateAPIKey）。
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// chatHTTP 客服透传专用 client（区别于 admin 聚合用的 5s adminHTTP）。
//
// 一轮对话在在线模式下要串行经过：JEV 前置判定 → LLM 多轮工具循环（每轮一个
// 上游请求）→ 工具执行。带 thinking 的模型单轮就可能十几秒；agent 内部对
// 「思考撞 max_tokens」还有一次加倍预算重试，最坏情况接近 2×300s。
// 一旦这里提前超时，agent 侧收到 context canceled 并把已做的工作丢掉，
// 网关返回 502「agent 不可达」—— 用户看到"服务挂了"，其实只是慢。
//
// 因此必须 **大于 agent 内部预算**：这里给 10 分钟，是 agent 侧单次 LLM 超时
// （llmHTTPTimeout）的 2 倍，保证不抢在 agent 前面放弃。
var chatHTTP = &http.Client{Timeout: 10 * time.Minute}

// RegisterPortal 注册用户自助路由。挂在已过 middleware.APIKey 的 group 上。
//
//	billingAdminBase = http://127.0.0.1:9103（billing 自服务端点）
//	agentBase        = http://127.0.0.1:9105（AI 客服）
func RegisterPortal(g *gin.RouterGroup, billingAdminBase, agentBase string, log *slog.Logger) {
	// 只读：自己的一揽子数据（余额/订阅/目录/消耗/流水）
	g.GET("/me", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/admin/me?tenant_id="+tenantStr(c), nil, log)
	})
	g.GET("/plans", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/admin/plans", nil, log)
	})
	g.GET("/topups", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/admin/topups?tenant_id="+tenantStr(c), nil, log)
	})
	g.GET("/recharge-orders", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/admin/recharge-orders?tenant_id="+tenantStr(c), nil, log)
	})
	// 商城目录：买家只读，query 透传 vendor（billing 已过滤为只列上架）。
	g.GET("/models", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/portal/models?"+c.Request.URL.RawQuery, nil, log)
	})
	// 我的消费（按模型分组，days 透传）。
	g.GET("/consumption", func(c *gin.Context) {
		proxy(c, http.MethodGet, billingAdminBase+"/admin/consumption?tenant_id="+tenantStr(c)+"&"+c.Request.URL.RawQuery, nil, log)
	})

	// 写操作：tenant_id 强制取自 key（前端传的被覆盖），防止越权改别人的账户
	g.POST("/recharge", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/recharge", log)
	})
	// 模拟支付：pending_payment → paid + 入账（页面「充值」按钮 = 建单+支付两步，与 AI 客服等价）
	g.POST("/recharge/pay", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/recharge/pay", log)
	})
	g.POST("/recharge/refund", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/recharge/refund", log)
	})
	g.POST("/subscribe", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/subscribe", log)
	})
	g.POST("/change-plan", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/change-plan", log)
	})
	g.POST("/cancel-subscription", func(c *gin.Context) {
		portalPOST(c, billingAdminBase+"/admin/cancel-subscription", log)
	})

	// AI 客服：完整透传（body + Authorization），agent 自己鉴权、自己绑定 tenant
	// 注意：不能用面板聚合用的 adminHTTP（5s 短超时）——agent 内部要走真实 DeepSeek 多轮 tool loop，
	// LLM 稍慢（首 token 或重试）就会被 5s 掐断成「agent 不可达」。这里用专用长超时 client。
	g.POST("/chat", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, agentBase+"/chat", bytes.NewReader(body))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "build chat request failed"})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", c.GetHeader("Authorization"))
		resp, err := chatHTTP.Do(req)
		if err != nil {
			log.Error("portal chat", "err", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "agent 不可达"})
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", b)
	})
}

// portalPOST 读 body → 强制 tenant_id = key 派生值 → 转发 billing admin。
func portalPOST(c *gin.Context, target string, log *slog.Logger) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json body"})
		return
	}
	// 执行层强制：tenant 只来自鉴权后的 key，绝不信前端。
	m["tenant_id"] = c.GetInt64("tenant_id")
	raw, _ := json.Marshal(m)
	proxy(c, http.MethodPost, target, raw, log)
}

func tenantStr(c *gin.Context) string {
	return strconv.FormatInt(c.GetInt64("tenant_id"), 10)
}
