package agent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

const sessionTTL = 30 * time.Minute

// pendingAction 写操作待确认状态：算好预览后挂起，等用户「确认/取消」。
type pendingAction struct {
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
	Preview  string         `json:"preview"`
	IdemKey  string         `json:"idem_key"` // 幂等锚点：重试同一次确认不重复入账
	TenantID uint64         `json:"tenant_id"`
}

type session struct {
	History       []Message      `json:"history"`
	PendingAction *pendingAction `json:"pending_action,omitempty"`
}

// traceItem 单次工具执行的审计可见结果（回给前端渲染 trace）。
type traceItem struct {
	Tool      string `json:"tool"`
	Args      string `json:"args"`
	Confirmed bool   `json:"confirmed"`
	Preview   string `json:"preview"`
	Result    string `json:"result"`
}

// llmChat LLM 调用面：生产是真实 DeepSeek（LLMClient），单测注入脚本化假 LLM（无网络）。
type llmChat interface {
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Message, error)
}

// billingAuth 鉴权面：生产是 client.Billing（gRPC），单测注入桩。
type billingAuth interface {
	ValidateAPIKey(ctx context.Context, keyHash string) (*billingv1.ValidateAPIKeyResponse, error)
}

// Handler AI 客服运行时：鉴权（key→tenant）→ 会话 → 意图路由（LLM）→ 工具执行 / 确认挂起。
type Handler struct {
	billing billingAuth // 用 ValidateAPIKey 鉴权，tenant 只从这里来
	tb      *Toolbox
	llm     llmChat
	st      *store.Store // 会话落库 + 审计
	rdb     *goredis.Client
	log     *slog.Logger

	/*** P3 Jev 判断引擎（nil = 停用，行为与未接入时一致） ***/
	jev          jevEvaluator
	jevGuardMin  float64 // 写操作守门阈值（默认 0.8）
	jevBlockMin  float64 // 每轮判定：合法性低于此值直接拦截（默认 0.30）
	jevWarnMin   float64 // 每轮判定：合法性低于此值放行但警告+强制确认（默认 0.60）
	jevIntentMin float64 // 意图预判阈值（默认 0.75）

	/*** 管理面板身份（gateway 注入内部密钥头时生效） ***/
	internalToken string // AGENT_INTERNAL_TOKEN：gateway 与 agent 共享，防伪造管理员
	defaultTenant uint64 // ADMIN_DEFAULT_TENANT：管理员未指定租户时的默认作用域
}

// SetAdminAccess 配置管理面板代操作能力（internalToken 为空 = 关闭该路径）。
func (h *Handler) SetAdminAccess(internalToken string, defaultTenant uint64) {
	h.internalToken = internalToken
	h.defaultTenant = defaultTenant
}

func NewHandler(b *client.Billing, tb *Toolbox, llm *LLMClient, st *store.Store, rdb *goredis.Client, log *slog.Logger) *Handler {
	return &Handler{billing: b, tb: tb, llm: llm, st: st, rdb: rdb, log: log,
		jevGuardMin: 0.8, jevBlockMin: 0.30, jevWarnMin: 0.60, jevIntentMin: 0.75}
}

// ServeHTTP POST /chat：body {session_id?, message, tenant_id?}。
//
// 两种身份（都从「执行层」解析，LLM 永远无法覆盖 tenant）：
//  1. 租户自助：Authorization: Bearer <租户 key> → billing ValidateAPIKey 派生 tenant；
//  2. 管理面板：gateway 已用 AdminSession 校验过管理员登录态，转发时带内部共享密钥头
//     X-Agent-Internal + X-Agent-Tenant（管理员以某个租户身份代为操作，默认租户由 gateway 决定）。
//     内部头必须匹配 AGENT_INTERNAL_TOKEN，否则一律按未授权处理——防止直接打 :9105 伪造管理员。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
		TenantID  uint64 `json:"tenant_id"` // 仅管理面板路径生效（管理员切换目标租户）
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json body")
		return
	}

	tenantID, apiKeyID, ok := h.resolveIdentity(w, r, req.TenantID)
	if !ok {
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeErr(w, http.StatusBadRequest, "message 不能为空")
		return
	}
	if req.SessionID == "" {
		req.SessionID = newSessionID()
	}
	if err := h.st.RecordSession(ctx, req.SessionID, tenantID, apiKeyID); err != nil {
		h.log.Error("record session", "err", err)
	}

	ses, err := h.loadSession(ctx, req.SessionID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session 读取失败")
		return
	}

	reply, trace, err := h.process(ctx, req.SessionID, tenantID, req.Message, ses)
	// 成功也必须落盘：写工具挂起的 pending_action、多轮历史都在这。
	// 只在错误时保存会导致确认流程在下一请求丢失（挂起状态没了）。
	if err := h.saveSession(ctx, req.SessionID, ses); err != nil {
		h.log.Error("save session", "err", err)
	}
	if err != nil {
		// 必须记日志：这条路径把内部错误直接透出成 500，
		// 若不记录，排查时只能看到客户端报错，agent 侧毫无线索。
		h.log.Error("process failed",
			"sid", req.SessionID, "tenant", tenantID,
			"msg", truncate(req.Message, 60), "err", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":      req.SessionID,
		"reply":           reply,
		"trace":           trace,
		"pending_confirm": ses.PendingAction != nil,
		"pending_preview": previewOf(ses.PendingAction),
		"pending_tool":    toolOf(ses.PendingAction),
	})
}

// resolveIdentity 解析请求身份，返回 (tenantID, apiKeyID, ok)；不 ok 时已写响应。
//
// 管理面板路径优先判定：必须同时具备内部共享密钥头（gateway 注入）才认，
// 否则回落到租户 key 鉴权 —— 保证「直接打 :9105 带 X-Agent-Tenant」无法越权。
func (h *Handler) resolveIdentity(w http.ResponseWriter, r *http.Request, wantTenant uint64) (uint64, uint64, bool) {
	// ---- 1) 管理面板路径（gateway 已校验管理员登录态，此处只认内部共享密钥） ----
	if h.internalToken != "" && subtleEqual(r.Header.Get("X-Agent-Internal"), h.internalToken) {
		tid := uint64(0)
		if v := strings.TrimSpace(r.Header.Get("X-Agent-Tenant")); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				tid = n
			}
		}
		if wantTenant > 0 { // body 显式指定目标租户（管理员切换视角）
			tid = wantTenant
		}
		if tid == 0 {
			tid = h.defaultTenant
		}
		if tid == 0 {
			writeErr(w, http.StatusBadRequest, "管理员会话缺少目标租户（请选择租户）")
			return 0, 0, false
		}
		return tid, 0, true // apiKeyID=0：管理员代为操作，非某个租户 key
	}

	// ---- 2) 租户 key 路径 ----
	key := extractKey(r)
	if key == "" {
		writeErr(w, http.StatusUnauthorized, "missing api key（请带租户 API key）")
		return 0, 0, false
	}
	sum := sha256.Sum256([]byte(key))
	authCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp, err := h.billing.ValidateAPIKey(authCtx, hex.EncodeToString(sum[:]))
	if err != nil {
		h.log.Error("agent auth validate", "err", err)
		writeErr(w, http.StatusServiceUnavailable, "billing 鉴权服务不可用")
		return 0, 0, false
	}
	if resp == nil || !resp.Valid {
		writeErr(w, http.StatusUnauthorized, "invalid api key")
		return 0, 0, false
	}
	return uint64(resp.TenantId), uint64(resp.ApiKeyId), true
}

// subtleEqual 常数时间比较（避免共享密钥被时序侧信道爆破）。
func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func previewOf(p *pendingAction) string {
	if p == nil {
		return ""
	}
	return p.Preview
}
func toolOf(p *pendingAction) string {
	if p == nil {
		return ""
	}
	return p.Tool
}

// process 处理一条用户消息，返回答复 + 本次工具 trace。
func (h *Handler) process(ctx context.Context, sessionID string, tenantID uint64, userMsg string, ses *session) (string, []traceItem, error) {
	ses.History = append(ses.History, Message{Role: "user", Content: userMsg})

	// 有挂起的写操作 → 先判定确认/取消（不依赖 LLM：关键字精确匹配，避免幻觉误触发）。
	if ses.PendingAction != nil {
		switch classifyConfirmation(userMsg) {
		case -1: // 取消
			pa := ses.PendingAction
			h.audit(sessionID, tenantID, "cancel_confirmation", pa.Args, false, pa.Preview, "cancelled")
			ses.PendingAction = nil
			reply := "好的，已取消本次操作，没有做任何改动。"
			ses.History = append(ses.History, Message{Role: "assistant", Content: reply})
			return reply, nil, nil
		case 1: // 确认 → 执行
			pa := ses.PendingAction
			ses.PendingAction = nil
			result, err := h.tb.Run(ctx, pa.Tool, pa.Args, pa.TenantID, pa.IdemKey)
			reply := result
			if err != nil {
				reply = "执行失败：" + err.Error()
			}
			reply = sanitizeReply(reply)
			h.audit(sessionID, tenantID, pa.Tool, pa.Args, true, pa.Preview, reply)
			ses.History = append(ses.History, Message{Role: "assistant", Content: reply})
			return reply, []traceItem{{Tool: pa.Tool, Args: jsonString(pa.Args), Confirmed: true, Preview: pa.Preview, Result: reply}}, nil
		}
		// other：保留挂起状态，继续正常对话（LLM 会提醒还有待确认操作）
	}

	// P3 每轮统一前置判定：一次 JEV 请求拿到「意图 + 合法性 + 风险」及置信度，
	// 再按分层策略决定后续 —— 高置信高风险直接拦下（不调 LLM、不碰工具），
	// 中等可疑放行但警告+强制确认，正常则注入意图预判提示帮 LLM 一次选对工具。
	// JEV 未接入或调用失败时静默退回原行为，绝不因判断引擎故障阻塞业务。
	v := h.evaluateTurn(ctx, userMsg, ses)
	if v.Block {
		reply := "抱歉，这个请求我不能执行。它未通过安全校验，可能存在越权或异常操作的风险。" +
			"如果你确实需要办理，请联系人工客服核实。"
		h.audit(sessionID, tenantID, "jev_block", map[string]any{"message": userMsg}, false, "", v.Reason,
			guardJSON("jev", "block", v.Reason, map[string]any{
				"legit": round2(v.LegitProb), "risk": round2(v.RiskScore),
				"risk_conf": round2(v.RiskConf), "intent": v.Intent,
			}))
		ses.History = append(ses.History, Message{Role: "assistant", Content: reply})
		return reply, nil, nil
	}

	reply, trace, err := h.llmLoop(ctx, sessionID, tenantID, v, ses)
	if err != nil {
		return "", trace, err
	}
	return sanitizeReply(reply), trace, nil
}

// round2 保留两位小数（审计可读性）。
func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// sanitizeReply 防御性清洗：剔除独占「⚙ 工具名」行、压缩连续空行（M9 输出格式修复的后备）。
// 正常 LLM 遵循 prompt 规则不会产生这些；这里兜底偶发回显工具调用痕迹。
func sanitizeReply(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if isGearToolLine(t) {
			continue
		}
		if t == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue // 压缩连续空行
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isGearToolLine(t string) bool {
	if !strings.HasPrefix(t, "⚙") {
		return false
	}
	for _, n := range []string{"get_balance", "get_consumption", "get_recent_bills", "list_models", "list_plans", "get_my_subscription", "recharge", "refund", "subscribe_plan", "change_plan", "cancel_subscription"} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return false
}

// llmLoop 标准 function-calling 循环：读工具即时执行并回填，写工具算预览挂起等确认。
// 每轮最多 5 次工具迭代，防 LLM 死循环。v 为 JEV 前置判定（含提示行与警告标记）。
func (h *Handler) llmLoop(ctx context.Context, sessionID string, tenantID uint64, v jevVerdict, ses *session) (string, []traceItem, error) {
	var trace []traceItem
	pending := ses.PendingAction

	for iter := 0; iter < 5; iter++ {
		messages := buildMessages(ses, pending, v)
		// 计时：多轮 loop 的耗时排查全靠这个（"总耗时 300s 但上游每次只要 2s"，
		// 没有每轮计时就只能猜）。
		t0 := time.Now()
		msg, err := h.llm.Chat(ctx, messages, h.tb.All())
		elapsed := time.Since(t0)
		if elapsed > 10*time.Second || err != nil {
			h.log.Info("llm turn", "sid", sessionID, "iter", iter,
				"ms", elapsed.Milliseconds(), "msgs", len(messages), "err", err)
		}
		if err != nil {
			return "", trace, err
		}
		if len(msg.ToolCalls) == 0 {
			ses.History = append(ses.History, *msg)
			return msg.Content, trace, nil
		}

		// 只保留**第一个**工具调用，其余丢弃。
		//
		// 为什么必须这样（实测定位，不是优化而是规避上游缺陷）：
		// 当历史里出现「assistant 一次发起 ≥2 个 tool_use + user 回填对应 tool_result」
		// 时，上游 deepseek-v4.1-flash 会陷入超长推理 —— 单请求耗时直接顶到
		// 300s 超时（max_tokens 从 4096 一路加到 32768 都被思考吃光）。
		// 二分验证：
		//   1 个 tool_use 回填 → 1.8~3.6s 正常
		//   2 个 tool_use 回填 → 75s+ 超时（无论 system 长短、工具多少、结果多短）
		// 在 prompt 里加"一次只调一个工具"的约束**无效** —— 触发点是已回填的历史，
		// 不是模型接下来打算怎么做。所以只能在实现层限制。
		//
		// 副作用很小：模型每轮少拿一个结果，下一轮会继续调它需要的工具
		//（loop 上限 5 轮足够覆盖），最终质量不受影响，只是多一次往返。
		//
		// 注意：assistant 消息与 tool 结果必须严格配对（缺配对上游会 400），
		// 所以裁剪 tool_calls 的同时也必须裁掉对应的 tool 消息 —— 这里通过
		// 只遍历首个 tc 自然实现（toolResults 只会有 1 条）。
		if len(msg.ToolCalls) > 1 {
			h.log.Info("trim multi tool_calls", "sid", sessionID, "iter", iter,
				"got", len(msg.ToolCalls), "keep", msg.ToolCalls[0].Function.Name,
				"dropped", len(msg.ToolCalls)-1)
			msg.ToolCalls = msg.ToolCalls[:1]
		}

		// 该轮有工具调用：逐条执行。注意 assistant tool_calls 消息只能整条入历史一次，
		// 且必须与每条 tool_call_id 的响应配对（DeepSeek 校验：tool_calls 后每个 id 都有 tool 消息，
		// 缺失/错位会 400）。所以先把结果收集起来，最后一次性配对写入。
		var toolResults []Message
		pendingReply := ""
		for _, tc := range msg.ToolCalls {
			name := tc.Function.Name
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{}
			}

			if _, read := readTools[name]; read {
				result, err := h.tb.Run(ctx, name, args, tenantID, "")
				if err != nil {
					result = "错误：" + err.Error()
				}
				h.audit(sessionID, tenantID, name, args, false, "", result)
				trace = append(trace, traceItem{Tool: name, Args: tc.Function.Arguments, Confirmed: false, Result: result})
				toolResults = append(toolResults, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
				continue
			}

			// 写操作：先过 Jev 守门（P3，nil 时直接放行）→ 算预览 + 挂起，回确认文案，本轮结束。
			preview, err := h.tb.Preview(ctx, name, args, tenantID)
			if err != nil {
				result := "操作不可执行：" + err.Error()
				h.audit(sessionID, tenantID, name, args, false, "", result)
				trace = append(trace, traceItem{Tool: name, Args: tc.Function.Arguments, Confirmed: false, Preview: preview, Result: result})
				toolResults = append(toolResults, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
				continue
			}
			if h.jev != nil {
				allowed, reason := h.guardWrite(ctx, tenantID, name, args, lastUserMsg(ses))
				if !allowed {
					result := reason
					h.audit(sessionID, tenantID, name, args, false, preview, result,
						fmt.Sprintf(`{"engine":"jev","decision":"block","reason":%q}`, reason))
					trace = append(trace, traceItem{Tool: name, Args: tc.Function.Arguments, Confirmed: false, Preview: preview, Result: result})
					toolResults = append(toolResults, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
					continue
				}
			}
			pa := &pendingAction{
				Tool: name, Args: args, Preview: preview,
				IdemKey:  newIdemKey(name),
				TenantID: tenantID,
			}
			ses.PendingAction = pa
			h.audit(sessionID, tenantID, name, args, false, preview, "pending_confirmation")
			trace = append(trace, traceItem{Tool: name, Args: tc.Function.Arguments, Confirmed: false, Preview: preview, Result: "等待确认"})
			// 写操作挂起：不把含 tool_calls 的 assistant 消息写入历史（否则该消息的
			// tool_call_id 没有完整配对，下一轮 LLM 校验会 400）。等确认时单独处理。
			pendingReply = preview + "\n\n请回复「确认」执行，或「取消」放弃。"
			break
		}

		if pendingReply != "" {
			ses.History = append(ses.History, Message{Role: "assistant", Content: pendingReply})
			return pendingReply, trace, nil
		}
		if len(toolResults) == 0 {
			// 全部 tool_calls 都不可执行（Preview 全报错且无任何结果）。
			return "（无有效操作）", trace, nil
		}
		// 读工具全部执行完：assistant tool_calls 消息一次 + 每条 tool 结果配对，回填后再让 LLM 给结论。
		ses.History = append(ses.History, *msg)
		ses.History = append(ses.History, toolResults...)
	}
	return "（工具循环次数过多，请换一种说法）", trace, nil
}

// buildMessages 组装 LLM 消息：系统提示（含 JEV 判定行 + 挂起确认提醒）+ 会话历史。
//
// JEV 的两类提示行语义不同，都要带进去：
//   - Hint：意图预判（正常请求，帮 LLM 一次选对工具）
//   - Warn：安全提醒（中等可疑，要求谨慎并强制确认）
func buildMessages(ses *session, pending *pendingAction, v jevVerdict) []Message {
	sys := systemPrompt
	if v.Warn {
		sys += "\n\n【安全模式】本轮请求的合理性判定偏低，请严格遵守：" +
			"只做用户明确要求的事，涉及资金/合同的操作必须先给出预览并等用户确认，禁止推测执行。"
	}
	if v.Hint != "" {
		sys += "\n\n" + v.Hint
	}
	if pending != nil {
		sys += "\n\n【待确认操作】用户有一个写操作等待确认：" + pending.Preview +
			"。请只引导用户回复「确认」或「取消」，不要代为执行、不要编造结果。"
	}
	msgs := make([]Message, 0, len(ses.History)+1)
	msgs = append(msgs, Message{Role: "system", Content: sys})
	msgs = append(msgs, trimHistory(ses.History, 24)...)
	return msgs
}

// trimHistory 裁剪会话历史，**保证 tool_use 与 tool_result 配对完整**。
//
// 为什么不能简单地 hist[len-N:]：
// 那会把「assistant 带 tool_calls」和紧随其后的「tool 结果」从中间切开，
// 产生孤儿消息。实测后果很严重 —— 上游遇到不成对的 tool 消息时会陷入
// 超长推理（单请求 130s+，甚至顶到超时）。这在日志里表现为
// "msgs=6 但耗时 132s"，而同样内容配对完整时只要几秒。
//
// 规则：从目标起点向前回退，直到落在一个「安全的边界」——
// 即该条不是 tool 消息，且前一条不是带 tool_calls 的 assistant。
func trimHistory(hist []Message, max int) []Message {
	if len(hist) <= max {
		return hist
	}
	start := len(hist) - max
	// 向前回退，找到不与前面 tool_calls 断裂的位置
	for start > 0 {
		prevIsToolCalls := hist[start-1].Role == "assistant" && len(hist[start-1].ToolCalls) > 0
		if hist[start].Role == "tool" || prevIsToolCalls {
			start--
			continue
		}
		break
	}
	return hist[start:]
}

// audit 写一条审计（读写都记；写操作带确认标记 + 预览）。guard 可空（P3 Jev 决策 JSON）。
func (h *Handler) audit(sessionID string, tenantID uint64, tool string, args map[string]any, confirmed bool, preview, result string, guard ...string) {
	raw, _ := json.Marshal(args)
	guardStr := ""
	if len(guard) > 0 {
		guardStr = guard[0]
	}
	if err := h.st.RecordAudit(context.Background(), sessionID, tenantID, tool, string(raw), confirmed, preview, result, guardStr); err != nil {
		h.log.Error("audit", "tool", tool, "err", err)
	}
}

// ---------- 会话持久（Redis JSON，TTL 30min） ----------

func sessionKey(id string) string { return "agent:session:" + id }

func (h *Handler) loadSession(ctx context.Context, sid string) (*session, error) {
	raw, err := h.rdb.Get(ctx, sessionKey(sid)).Result()
	if err == goredis.Nil {
		return &session{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s session
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		h.log.Error("session corrupt", "sid", sid, "err", err)
		return &session{}, nil
	}
	return &s, nil
}

func (h *Handler) saveSession(ctx context.Context, sid string, s *session) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return h.rdb.Set(ctx, sessionKey(sid), raw, sessionTTL).Err()
}

// ---------- 确认分类（不依赖 LLM：关键字精确匹配） ----------

var (
	confirmKeywords = []string{"确认", "确定", "好的", "可以", "是的", "嗯", "ok", "yes", "yes.", "执行", "没问题"}
	cancelKeywords  = []string{"取消", "算了", "不要", "不用", "放弃", "不了", "no", "stop", "退出"}
)

func classifyConfirmation(text string) int {
	n := strings.ToLower(strings.TrimSpace(text))
	n = strings.TrimRight(n, "。！？!?.,， ")
	for _, k := range cancelKeywords {
		if n == k {
			return -1
		}
	}
	for _, k := range confirmKeywords {
		if n == k {
			return 1
		}
	}
	return 0
}

// readTools 只读工具集（即时执行，无需确认）。
var readTools = map[string]bool{
	"get_balance":         true,
	"get_consumption":     true,
	"get_recent_bills":    true,
	"list_models":         true,
	"list_plans":          true,
	"get_my_subscription": true,
}

// ---------- 小工具 ----------

func extractKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.Header.Get("api-key")
}

func newSessionID() string { return "sid_" + randHex(8) }
func newIdemKey(tool string) string {
	return fmt.Sprintf("agent:%s:%d:%s", tool, time.Now().UnixNano(), randHex(4))
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
