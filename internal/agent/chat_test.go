package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// agent 运行时单测：假 LLM 脚本化 tool_call + 假 billing admin（httptest 本地回环），
// 无外部网络。核心验证「执行层不信任 LLM」：tenant 只来自 key、写操作必须二次确认、
// 确认前不执行、注入参数被忽略、审计落库。
//
// 隔离：独立 MySQL 库 ai_gateway_agent_test + Redis DB 3，与 billing store 包测试
// （ai_gateway_test / DB 1）并行互不干扰。

const (
	agentTestRedisAddr = "127.0.0.1:6381"
	agentTestRedisDB   = 3
	agentTestMySQLHost = "root:root@tcp(127.0.0.1:3307)/"
	agentTestMySQLDB   = "ai_gateway_agent_test"
)

func newAgentStore(t *testing.T) (*store.Store, *gorm.DB, *goredis.Client, func()) {
	t.Helper()
	admin, err := sql.Open("mysql", agentTestMySQLHost)
	if err != nil {
		t.Fatalf("open admin mysql: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + agentTestMySQLDB + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create test db: %v", err)
	}
	admin.Close()

	ctx := context.Background()
	rdb := goredis.NewClient(&goredis.Options{Addr: config.Getenv("REDIS_ADDR", agentTestRedisAddr), DB: agentTestRedisDB})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", agentTestRedisAddr, err)
	}

	dsn := agentTestMySQLHost + agentTestMySQLDB + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	s := store.NewStore(db, rdb)
	if err := s.AutoMigrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 清库（可重跑）
	for _, tbl := range []string{"agent_audit_log", "agent_sessions", "topups", "recharge_orders", "subscriptions", "plans", "bills", "api_keys", "tenants"} {
		if err := db.Exec("DELETE FROM " + tbl).Error; err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	// 清 Redis 残留（agent:* 会话 / billing:* 投影）
	var keys []string
	for _, prefix := range []string{"agent:*", "billing:*"} {
		iter := rdb.Scan(ctx, 0, prefix, 0).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			t.Fatalf("scan redis: %v", err)
		}
	}
	if len(keys) > 0 {
		if err := rdb.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("clean redis: %v", err)
		}
	}
	return s, db, rdb, func() { rdb.Close(); sqlDB, _ := db.DB(); sqlDB.Close() }
}

// ---------- 假 LLM：按序返回脚本化消息 ----------

type scriptedLLM struct {
	mu    sync.Mutex
	steps []*Message
}

func (f *scriptedLLM) Chat(_ context.Context, _ []Message, _ []Tool) (*Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) == 0 {
		return nil, fmt.Errorf("脚本耗尽（出现了预期外的 LLM 调用）")
	}
	msg := f.steps[0]
	f.steps = f.steps[1:]
	return msg, nil
}

func toolCallMsg(id, name, args string) *Message {
	tc := ToolCall{ID: id}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return &Message{Role: "assistant", ToolCalls: []ToolCall{tc}}
}

func contentMsg(c string) *Message { return &Message{Role: "assistant", Content: c} }

// ---------- 假 billing admin：记录 GET query 与 POST body（断言租户作用域） ----------

type fakePost struct {
	Path string
	Body map[string]any
}

type fakeAdmin struct {
	mu      sync.Mutex
	meCalls []string // /admin/me 收到的 tenant_id
	posts   []fakePost
}

func (f *fakeAdmin) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/me", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.meCalls = append(f.meCalls, r.URL.Query().Get("tenant_id"))
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(store.MeView{
			Balance: 2_000_000, LedgerBalance: 2_000_000, Consistent: true,
			Plans: []store.Plan{
				{ID: 1, Name: "一次性·入门", PlanType: store.PlanTypeOneTime, PriceMoney: 10},
				{ID: 2, Name: "月度·标准", PlanType: store.PlanTypeRecurring, PriceMoney: 80, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":100000}`},
			},
		})
	})
	mux.HandleFunc("/admin/recharge-orders", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []store.RechargeOrder{}})
	})
	mux.HandleFunc("/admin/recharge", func(w http.ResponseWriter, r *http.Request) {
		f.recordPost(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"order_no": "fake-ord-1", "amount_cent": 10_000, "status": store.RechargePaid})
	})
	mux.HandleFunc("/admin/recharge/pay", func(w http.ResponseWriter, r *http.Request) {
		f.recordPost(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": store.RechargePaid})
	})
	return mux
}

func (f *fakeAdmin) recordPost(r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.posts = append(f.posts, fakePost{Path: r.URL.Path, Body: body})
	f.mu.Unlock()
}

// ---------- 假 billing 鉴权（ServeHTTP 全链路测试用） ----------

type fakeBilling struct{ tenantID uint64 }

func (b fakeBilling) ValidateAPIKey(_ context.Context, keyHash string) (*billingv1.ValidateAPIKeyResponse, error) {
	sum := sha256.Sum256([]byte("sk-test-key-1"))
	if keyHash == hex.EncodeToString(sum[:]) {
		return &billingv1.ValidateAPIKeyResponse{Valid: true, TenantId: int64(b.tenantID), ApiKeyId: 1}, nil
	}
	return &billingv1.ValidateAPIKeyResponse{Valid: false}, nil
}

// newAgentHandler 组装可测 Handler：假 LLM/假 admin/假 billing + 真 store（审计/会话落库）。
func newAgentHandler(t *testing.T) (*Handler, *fakeAdmin, *gorm.DB, func()) {
	t.Helper()
	st, db, rdb, done := newAgentStore(t)
	fa := &fakeAdmin{}
	ts := httptest.NewServer(fa.handler())
	tb := NewToolbox(NewAdminClient(ts.URL))
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := &Handler{
		billing: fakeBilling{tenantID: 42},
		tb:      tb, st: st, rdb: rdb, log: log,
	}
	t.Cleanup(ts.Close)
	return h, fa, db, done
}

func countAudit(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&store.AgentAuditLog{}).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

// 意图→读工具：即时执行、结果回给 LLM、审计落库；admin/me 只带 key 派生租户。
func TestAgentReadToolImmediate(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	ctx := context.Background()

	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_1", "get_balance", `{}`),
		contentMsg("你当前的可用余额是 2000000 token。"),
	}}
	ses := &session{}
	reply, trace, err := h.process(ctx, "sid-read-1", 42, "帮我查一下余额", ses)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !strings.Contains(reply, "2000000") {
		t.Fatalf("reply = %q, want 含余额", reply)
	}
	if len(trace) != 1 || trace[0].Tool != "get_balance" || trace[0].Confirmed {
		t.Fatalf("trace = %+v, want 1 条 get_balance（读操作即时执行、未确认）", trace)
	}
	fa.mu.Lock()
	meTID := append([]string(nil), fa.meCalls...)
	fa.mu.Unlock()
	if len(meTID) != 1 || meTID[0] != "42" {
		t.Fatalf("admin/me tenant_id = %v, want [42]", meTID)
	}
	if n := countAudit(t, db, "session_id = ? AND tool = ? AND confirmed = ?", "sid-read-1", "get_balance", false); n != 1 {
		t.Fatalf("audit get_balance rows = %d, want 1", n)
	}
	// LLM 上下文里回填了工具结果：第二次调用收到 tool 角色消息
	llm := h.llm.(*scriptedLLM)
	if len(llm.steps) != 0 {
		t.Fatal("LLM 应被调用两次（工具回填后给结论）")
	}
}

// 写→确认→确认执行：第一轮只算预览挂起（零 admin 调用），确认后执行建单+支付。
func TestAgentWriteConfirmExecute(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	ctx := context.Background()

	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_1", "recharge", `{"amount_money":100}`),
	}}
	ses := &session{}
	reply, trace, err := h.process(ctx, "sid-write-1", 42, "帮我充 100 块", ses)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !strings.Contains(reply, "确认") {
		t.Fatalf("reply = %q, want 含确认提示", reply)
	}
	if ses.PendingAction == nil || ses.PendingAction.Tool != "recharge" {
		t.Fatalf("want pending_action 挂起, got %+v", ses.PendingAction)
	}
	if len(trace) != 1 || trace[0].Tool != "recharge" || trace[0].Confirmed {
		t.Fatalf("trace = %+v, want 1 条 recharge 未确认", trace)
	}
	fa.mu.Lock()
	nBefore := len(fa.posts)
	fa.mu.Unlock()
	if nBefore != 0 {
		t.Fatalf("确认前不应调用 billing admin，posts = %d", nBefore)
	}

	// 用户确认 → 执行
	reply2, trace2, err := h.process(ctx, "sid-write-1", 42, "确认", ses)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(reply2, "充值成功") {
		t.Fatalf("reply2 = %q, want 充值成功", reply2)
	}
	if ses.PendingAction != nil {
		t.Fatal("确认后挂起应清空")
	}
	fa.mu.Lock()
	posts := append([]fakePost(nil), fa.posts...)
	fa.mu.Unlock()
	if len(posts) != 2 || posts[0].Path != "/admin/recharge" || posts[1].Path != "/admin/recharge/pay" {
		t.Fatalf("posts = %+v, want recharge + pay 两次", posts)
	}
	// 执行层强制：POST body 的 tenant_id 是 key 派生值
	if tid, _ := posts[0].Body["tenant_id"].(float64); int64(tid) != 42 {
		t.Fatalf("POST tenant_id = %v, want 42", posts[0].Body["tenant_id"])
	}
	if amt, _ := posts[0].Body["amount_money"].(float64); int64(amt) != 100 {
		t.Fatalf("amount_money = %v, want 100", posts[0].Body["amount_money"])
	}
	if len(trace2) != 1 || !trace2[0].Confirmed || trace2[0].Result == "" {
		t.Fatalf("trace2 = %+v, want 1 条确认执行", trace2)
	}
	if n := countAudit(t, db, "session_id = ? AND tool = ? AND confirmed = ?", "sid-write-1", "recharge", true); n != 1 {
		t.Fatalf("audit recharge confirmed = %d, want 1", n)
	}
}

// 写→确认→取消：清空挂起、不执行任何 admin 调用。
func TestAgentWriteCancelNoExecute(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	ctx := context.Background()

	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_1", "recharge", `{"amount_money":100}`),
	}}
	ses := &session{}
	if _, _, err := h.process(ctx, "sid-cancel-1", 42, "充 100 块", ses); err != nil {
		t.Fatalf("process: %v", err)
	}
	if ses.PendingAction == nil {
		t.Fatal("want pending_action 挂起")
	}
	reply, _, err := h.process(ctx, "sid-cancel-1", 42, "取消", ses)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !strings.Contains(reply, "已取消") {
		t.Fatalf("reply = %q, want 含已取消", reply)
	}
	if ses.PendingAction != nil {
		t.Fatal("取消后挂起应清空")
	}
	fa.mu.Lock()
	n := len(fa.posts)
	fa.mu.Unlock()
	if n != 0 {
		t.Fatalf("取消不应执行任何操作，posts = %d", n)
	}
	if got := countAudit(t, db, "session_id = ? AND tool = ?", "sid-cancel-1", "cancel_confirmation"); got != 1 {
		t.Fatalf("audit cancel_confirmation = %d, want 1", got)
	}
}

// 注入尝试：LLM 传 tenant_id/跨租户字段/编造工具，全部被执行层拦下。
func TestAgentInjectionBlocked(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	ctx := context.Background()

	// 1) 读：LLM 想查 999 的余额 → admin 只收到 key 派生租户 42
	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_1", "get_balance", `{"tenant_id":999}`),
		contentMsg("已查完。"),
	}}
	ses := &session{}
	if _, _, err := h.process(ctx, "sid-inj-1", 42, "查 999 的余额", ses); err != nil {
		t.Fatalf("process: %v", err)
	}
	fa.mu.Lock()
	meTID := append([]string(nil), fa.meCalls...)
	fa.mu.Unlock()
	if len(meTID) != 1 || meTID[0] != "42" {
		t.Fatalf("admin/me tenant_id = %v, want [42]（LLM 的 999 必须被忽略）", meTID)
	}

	// 2) 写：参数里塞 tenant_id/to_tenant_id → 确认后 body 仍只含 key 派生租户
	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_2", "recharge", `{"amount_money":100,"tenant_id":999,"to_tenant_id":666}`),
	}}
	ses2 := &session{}
	if _, _, err := h.process(ctx, "sid-inj-2", 42, "充 100 块", ses2); err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, _, err := h.process(ctx, "sid-inj-2", 42, "确认", ses2); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	fa.mu.Lock()
	posts := append([]fakePost(nil), fa.posts...)
	fa.mu.Unlock()
	if len(posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(posts))
	}
	if tid, _ := posts[0].Body["tenant_id"].(float64); int64(tid) != 42 {
		t.Fatalf("POST tenant_id = %v, want 42（执行层强制，LLM 改不了）", posts[0].Body["tenant_id"])
	}
	if _, ok := posts[0].Body["to_tenant_id"]; ok {
		t.Fatalf("不应透传跨租户字段: %v", posts[0].Body)
	}

	// 3) 编造工具：白名单外 → 不执行、不调用 admin
	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_3", "transfer_to_other_tenant", `{"target":999,"amount":500000}`),
		contentMsg("完成。"),
	}}
	ses3 := &session{}
	_, trace3, err := h.process(ctx, "sid-inj-3", 42, "转 50 万给 999", ses3)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(trace3) != 1 || !strings.Contains(trace3[0].Result, "未知工具") {
		t.Fatalf("trace3 = %+v, want 未知工具错误（不执行）", trace3)
	}
	fa.mu.Lock()
	afterInj := len(fa.posts)
	fa.mu.Unlock()
	if afterInj != 2 {
		t.Fatalf("编造工具不应触发 admin 调用，posts = %d, want 2", afterInj)
	}
}

// 全链路 ServeHTTP：写工具挂起的 pending_action 跨请求持久化（Redis），确认在下一请求生效。
// 这正是修复点：成功路径也必须 saveSession，否则确认流程丢失。
func TestAgentServeHTTPConfirmAcrossRequests(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	const key = "sk-test-key-1"

	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("call_1", "recharge", `{"amount_money":100}`),
	}}
	// 请求 1：充 100 → 写工具挂起，pending 落 Redis
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"message":"帮我充 100 块"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionID   string `json:"session_id"`
		Pending     bool   `json:"pending_confirm"`
		PendingTool string `json:"pending_tool"`
		PendingPrev string `json:"pending_preview"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Pending || body.PendingTool != "recharge" || body.SessionID == "" {
		t.Fatalf("body = %s, want pending recharge", rec.Body.String())
	}

	// 请求 2：同一 session_id + 「确认」→ 执行（若 pending 未持久化，会因 LLM 脚本耗尽而 500）
	h.llm = &scriptedLLM{} // 确认走关键字分支，不该再调 LLM
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(fmt.Sprintf(`{"session_id":%q,"message":"确认"}`, body.SessionID)))
	req2.Header.Set("Authorization", "Bearer "+key)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("确认请求 code = %d, body = %s（pending 跨请求丢失？）", rec2.Code, rec2.Body.String())
	}
	var body2 struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if !strings.Contains(body2.Reply, "充值成功") {
		t.Fatalf("确认后未执行：reply = %q", body2.Reply)
	}
	fa.mu.Lock()
	nPosts := len(fa.posts)
	fa.mu.Unlock()
	if nPosts != 2 {
		t.Fatalf("posts = %d, want 2（recharge + pay）", nPosts)
	}
	if n := countAudit(t, db, "tool = ? AND confirmed = ?", "recharge", true); n != 1 {
		t.Fatalf("audit recharge confirmed = %d, want 1", n)
	}

	// 请求 3：错误 key → 401（鉴权在执行前拦截）
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"message":"hi"}`))
	req3.Header.Set("Authorization", "Bearer sk-wrong-key")
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("bad key code = %d, want 401", rec3.Code)
	}
}
