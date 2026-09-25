package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// ---------- billing admin HTTP 客户端（agent 的工具操作面） ----------

type AdminClient struct {
	baseURL string
	cli     *http.Client
}

func NewAdminClient(baseURL string) *AdminClient {
	return &AdminClient{
		baseURL: baseURL,
		cli:     &http.Client{Timeout: 5 * time.Second},
	}
}

// get GET billing admin（query 携带 tenant_id，租户作用域）。
func (c *AdminClient) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// post POST billing admin（body 带 tenant_id，由调用方显式传）。
func (c *AdminClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *AdminClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error != "" {
			return nil, fmt.Errorf("%s", e.Error)
		}
		return nil, fmt.Errorf("billing HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

// ---------- 工具定义 ----------

// execTool 一个工具：元数据（给 LLM）+ 预览（写操作确认文案）+ 执行（只对自己租户）。
// confirm=true 的写操作：执行前必须预览 + 用户确认；confirm=false 的读操作即时执行。
type execTool struct {
	name        string
	description string
	parameters  map[string]any
	confirm     bool
	preview     func(ctx context.Context, args map[string]any, tenantID uint64) (string, error)
	run         func(ctx context.Context, args map[string]any, tenantID uint64, idemKey string) (string, error)
}

// Toolbox 工具集：LLM 只能从这里挑工具，参数一律在此层重新校验（执行层不信任 LLM）。
type Toolbox struct {
	admin *AdminClient
	tools map[string]*execTool
}

func NewToolbox(admin *AdminClient) *Toolbox {
	tb := &Toolbox{admin: admin, tools: map[string]*execTool{}}
	for _, t := range []*execTool{
		tb.readBalance(), tb.readConsumption(), tb.readRecentBills(), tb.listModels(), tb.listPlans(), tb.readMySubscription(),
		tb.rechargeTool(), tb.refundRechargeTool(), tb.subscribePlanTool(), tb.changePlanTool(), tb.cancelSubscriptionTool(),
	} {
		tb.tools[t.name] = t
	}
	return tb
}

func (tb *Toolbox) All() []Tool {
	names := make([]string, 0, len(tb.tools))
	for n := range tb.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		t := tb.tools[n]
		var fn Tool
		fn.Type = "function"
		fn.Function.Name = t.name
		fn.Function.Description = t.description
		fn.Function.Parameters = t.parameters
		out = append(out, fn)
	}
	return out
}

// Preview 写工具执行前算确认文案；读工具无预览。
func (tb *Toolbox) Preview(ctx context.Context, name string, args map[string]any, tenantID uint64) (string, error) {
	t, ok := tb.tools[name]
	if !ok {
		return "", fmt.Errorf("未知工具: %s", name)
	}
	if !t.confirm {
		return "", fmt.Errorf("工具 %s 是只读操作，无需确认", name)
	}
	if t.preview == nil {
		return "", fmt.Errorf("工具 %s 缺少预览实现", name)
	}
	return t.preview(ctx, args, tenantID)
}

// Run 执行工具（读即时 / 写由确认状态机调用）。返回给用户/LLM 的说明文本。
func (tb *Toolbox) Run(ctx context.Context, name string, args map[string]any, tenantID uint64, idemKey string) (string, error) {
	t, ok := tb.tools[name]
	if !ok {
		return "", fmt.Errorf("未知工具: %s", name)
	}
	if t.run == nil {
		return "", fmt.Errorf("工具 %s 缺少执行实现", name)
	}
	return t.run(ctx, args, tenantID, idemKey)
}

// ---------- 参数校验 helpers ----------

func argInt(args map[string]any, key string) (int64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("缺少参数 %s", key)
	}
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	}
	return 0, fmt.Errorf("参数 %s 必须为数字", key)
}

func argStr(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("缺少参数 %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("参数 %s 必须为非空字符串", key)
	}
	return s, nil
}

// ---------- 读工具（即时执行） ----------

func (tb *Toolbox) readBalance() *execTool {
	return &execTool{
		name:        "get_balance",
		description: "查询当前账户可用余额（元 ¥，附 token 数）。",
		parameters:  obj(nil, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			v, err := tb.me(ctx, tenantID)
			if err != nil {
				return "", err
			}
			consistent := "一致"
			if !v.Consistent {
				consistent = "不一致（对账收敛中）"
			}
			return fmt.Sprintf("当前可用余额 ¥%.2f；账本口径 ¥%.2f，两者%s。",
				yuan(v.Balance), yuan(v.LedgerBalance), consistent), nil
		},
	}
}

func (tb *Toolbox) readConsumption() *execTool {
	return &execTool{
		name:        "get_consumption",
		description: "查询近 7 天每日消耗量（token，并附约合金额 ¥）。",
		parameters:  obj(nil, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			v, err := tb.me(ctx, tenantID)
			if err != nil {
				return "", err
			}
			if len(v.Consumption) == 0 {
				return "近 7 天没有消耗记录。", nil
			}
			var total int64
			lines := make([]string, 0, len(v.Consumption))
			for _, d := range v.Consumption {
				lines = append(lines, fmt.Sprintf("%s 消耗 ¥%.2f", d.Day, yuan(d.Tokens)))
				total += d.Tokens
			}
			return fmt.Sprintf("近 7 天每日消耗（元）：%s；合计 ¥%.2f。",
				joinLines(lines), yuan(total)), nil
		},
	}
}

// readRecentBills 最近 N 笔消费明细（读工具，即时执行）：答「最近消费几笔、什么时间、花了多少」。
func (tb *Toolbox) readRecentBills() *execTool {
	return &execTool{
		name:        "get_recent_bills",
		description: "查询最近几笔消费明细（默认最近 3 笔，含发生时间/模型/token/约合金额 ¥）。",
		parameters: obj(map[string]any{
			"limit": map[string]any{"type": "number", "description": "查询笔数，默认 3"},
		}, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			limit := int64(3)
			if _, ok := args["limit"]; ok {
				if n, err := argInt(args, "limit"); err == nil {
					limit = n
				}
			}
			if limit < 1 {
				limit = 1
			}
			if limit > 10 {
				limit = 10
			}
			raw, err := tb.admin.get(ctx, "/admin/bills", url.Values{
				"tenant_id": {fmt.Sprint(tenantID)}, "phase": {"settle"}, "status": {"settled"}, "limit": {fmt.Sprint(limit)},
			})
			if err != nil {
				return "", err
			}
			var res struct {
				Items []struct {
					Model            string `json:"Model"`
					PromptTokens     int64  `json:"PromptTokens"`
					CompletionTokens int64  `json:"CompletionTokens"`
					ActualQuota      int64  `json:"ActualQuota"` // 扣费金额（分）
					CreatedAt        string `json:"CreatedAt"`
				} `json:"items"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return "", fmt.Errorf("账单查询失败: %v", err)
			}
			if len(res.Items) == 0 {
				return "最近没有消费记录。", nil
			}
			lines := make([]string, 0, len(res.Items))
			var total int64
			for _, b := range res.Items {
				tk := b.PromptTokens + b.CompletionTokens
				total += b.ActualQuota
				t := strings.Replace(b.CreatedAt, "T", " ", 1)
				if len(t) > 19 {
					t = t[:19]
				}
				model := b.Model
				if model == "" {
					model = "?"
				}
				lines = append(lines, fmt.Sprintf("%s · %s · 用量 %d token · 扣费 ¥%.2f", t, model, tk, yuan(b.ActualQuota)))
			}
			return fmt.Sprintf("最近 %d 笔消费：\n%s\n合计扣费 ¥%.2f。",
				len(res.Items), joinLines(lines), yuan(total)), nil
		},
	}
}

func (tb *Toolbox) listPlans() *execTool {
	return &execTool{
		name:        "list_plans",
		description: "查询套餐目录（一次性/定期订阅，含额度与价格）。",
		parameters:  obj(nil, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			v, err := tb.me(ctx, tenantID)
			if err != nil {
				return "", err
			}
			if len(v.Plans) == 0 {
				return "暂无上架套餐。", nil
			}
			lines := make([]string, 0, len(v.Plans))
			for _, p := range v.Plans {
				if p.Status != 0 {
					continue
				}
				// M9 GPT Plus 式：月费 + 每 N 小时滚动的各档次 token 额度（不「入账钱」）。
				lines = append(lines, fmt.Sprintf("套餐#%d %s（定期订阅）· 月费 ¥%d · 每 %d 小时刷新额度：%s",
					p.ID, p.Name, p.PriceMoney, p.RefreshHours, planQuotaLine(&p)))
			}
			return "可选套餐：\n" + joinLines(lines), nil
		},
	}
}

func (tb *Toolbox) readMySubscription() *execTool {
	return &execTool{
		name:        "get_my_subscription",
		description: "查询我当前已订购的套餐（一次性/定期，含到期与下期生效信息）。",
		parameters:  obj(nil, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			v, err := tb.me(ctx, tenantID)
			if err != nil {
				return "", err
			}
			if v.Subscription == nil {
				return "当前没有生效中的订阅。", nil
			}
			s := v.Subscription
			line := fmt.Sprintf("当前订阅：套餐#%d（定期订阅）", s.PlanID)
			if s.CycleEnd != nil {
				line += fmt.Sprintf("，本期到 %s", s.CycleEnd.Format("2006-01-02"))
				if s.AutoRenew {
					line += "，到期自动续费"
				}
			}
			if s.PendingPlanID != nil {
				line += fmt.Sprintf("，下期将切换为套餐#%d", *s.PendingPlanID)
			}
			// M9：各档次每窗口剩余额度（token），不再显示「累计已授予 ¥」。
			if len(v.Allowance) > 0 {
				parts := make([]string, 0, len(v.Allowance))
				for _, a := range v.Allowance {
					parts = append(parts, fmt.Sprintf("%s %s/%s", a.Tier, tokensCN(a.Remaining), tokensCN(a.Quota)))
				}
				line += "，剩余额度：" + strings.Join(parts, " · ")
				if v.RefreshHours > 0 {
					line += fmt.Sprintf("（每 %d 小时滚动刷新，未用不累积）", v.RefreshHours)
				}
			}
			return line, nil
		},
	}
}

// listModels 商品目录（只读）：答「kimi-k2 多少钱」「有哪些模型」。
func (tb *Toolbox) listModels() *execTool {
	return &execTool{
		name:        "list_models",
		description: "查询模型商城商品目录：厂商/模型/输入输出单价（元，每百万 token）/上下文/标签。",
		parameters: obj(map[string]any{
			"vendor": strProp("厂商筛选，如 DeepSeek/通义千问/Kimi；空=全部"),
		}, nil),
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			q := url.Values{}
			if v, err := argStr(args, "vendor"); err == nil {
				q.Set("vendor", v)
			}
			raw, err := tb.admin.get(ctx, "/portal/models", q)
			if err != nil {
				return "", err
			}
			var res struct {
				Items []store.Model `json:"items"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return "", fmt.Errorf("目录查询失败: %v", err)
			}
			if len(res.Items) == 0 {
				return "暂无上架模型。", nil
			}
			lines := make([]string, 0, len(res.Items))
			for _, m := range res.Items {
				price := "免费"
				if m.InputPriceCent > 0 || m.OutputPriceCent > 0 {
					price = fmt.Sprintf("输入 ¥%.2f / 输出 ¥%.2f（每百万 token）",
						yuan(m.InputPriceCent), yuan(m.OutputPriceCent))
				}
				lines = append(lines, fmt.Sprintf("%s · %s · 上下文 %dK · %s",
					m.Vendor, m.ModelID, m.ContextLen/1000, price))
			}
			return joinLines(lines), nil
		},
	}
}

// ---------- 写工具（必须二次确认） ----------

func (tb *Toolbox) rechargeTool() *execTool {
	return &execTool{
		name:        "recharge",
		description: "充值：给账户充值（模拟支付，1 元 = 100 分）。需要用户确认。",
		confirm:     true,
		parameters: obj(map[string]any{
			"amount_money": numProp("充值金额（元），整数，大于 0"),
		}, []string{"amount_money"}),
		preview: func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
			money, err := argInt(args, "amount_money")
			if err != nil {
				return "", err
			}
			if money <= 0 {
				return "", fmt.Errorf("充值金额必须大于 0")
			}
			return fmt.Sprintf("将模拟充值 ¥%d（模拟支付，不产生真实扣款）。确认后到账。", money), nil
		},
		run: func(ctx context.Context, args map[string]any, tenantID uint64, idemKey string) (string, error) {
			money, err := argInt(args, "amount_money")
			if err != nil {
				return "", err
			}
			var o store.RechargeOrder
			if err := tb.postJSON(ctx, "/admin/recharge",
				map[string]any{"tenant_id": tenantID, "amount_money": money, "idem_key": idemKey}, &o); err != nil {
				return "", err
			}
			// 建单成功后模拟支付（order_no 唯一 → 幂等）。
			if err := tb.postJSON(ctx, "/admin/recharge/pay", map[string]any{"order_no": o.OrderNo}, nil); err != nil {
				return "", err
			}
			return fmt.Sprintf("充值成功：订单 %s 已支付，到账 ¥%d（%d 分）。", o.OrderNo, o.AmountMoney, o.AmountCent), nil
		},
	}
}

func (tb *Toolbox) refundRechargeTool() *execTool {
	return &execTool{
		name:        "refund_recharge",
		description: "退款：按订单号退一笔已支付的充值订单，或按金额退（退款量=min(订单/金额, 当前余额)，最多退到当前余额，绝不退负）。需要用户确认。",
		confirm:     true,
		parameters: obj(map[string]any{
			"order_no":     strProp("要退款的充值订单号（按订单退款，与 amount_money 二选一）"),
			"amount_money": numProp("退款金额（元），最多退到当前可用余额（与 order_no 二选一）"),
		}, nil),
		preview: func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
			// 先取实时余额：确认文案里的「当前余额/最多可退」必须是此刻的值，不能是对话里的旧数字。
			bal, err := tb.balance(ctx, tenantID)
			if err != nil {
				return "", err
			}
			if orderNo, err := argStr(args, "order_no"); err == nil {
				o, err := tb.findPaidOrder(ctx, tenantID, orderNo)
				if err != nil {
					return "", err
				}
				refund := minI64(o.AmountCent, bal)
				if refund <= 0 {
					return "", fmt.Errorf("当前余额为 0，订单 %s 无可退额度", orderNo)
				}
				return fmt.Sprintf("将退款充值订单 %s：订单额度 ¥%.2f，当前可用余额 ¥%.2f，实际退款 ¥%.2f（最多退到当前余额）。确认后执行。",
					orderNo, yuan(o.AmountCent), yuan(bal), yuan(refund)), nil
			}
			amount, err := argInt(args, "amount_money")
			if err != nil {
				return "", fmt.Errorf("请提供要退的订单号或退款金额（元）")
			}
			if amount <= 0 {
				return "", fmt.Errorf("退款金额必须大于 0")
			}
			target := amount * 100 // 元 → 分
			refund := minI64(target, bal)
			if refund <= 0 {
				return "", fmt.Errorf("当前余额为 0，无可退额度")
			}
			return fmt.Sprintf("将退款 ¥%d：当前可用余额 ¥%.2f，实际退款 ¥%.2f（最多退到当前余额）。确认后执行。",
				amount, yuan(bal), yuan(refund)), nil
		},
		run: func(ctx context.Context, args map[string]any, tenantID uint64, idemKey string) (string, error) {
			// 按订单退
			if orderNo, err := argStr(args, "order_no"); err == nil {
				var r store.RefundResult
				if err := tb.postJSON(ctx, "/admin/recharge/refund",
					map[string]any{"tenant_id": tenantID, "order_no": orderNo}, &r); err != nil {
					return "", err
				}
				return fmt.Sprintf("退款成功：订单 %s 退回 ¥%.2f，当前可用余额 ¥%.2f。",
					r.OrderNo, yuan(r.RefundCent), yuan(r.BalanceAfter)), nil
			}
			// 按金额退（元），幂等锚点用确认状态的 idemKey
			amount, err := argInt(args, "amount_money")
			if err != nil {
				return "", fmt.Errorf("请提供要退的订单号或退款金额（元）")
			}
			var r store.RefundResult
			if err := tb.postJSON(ctx, "/admin/recharge/refund",
				map[string]any{"tenant_id": tenantID, "amount_money": amount, "idem_key": idemKey}, &r); err != nil {
				return "", err
			}
			return fmt.Sprintf("退款成功：退回 ¥%.2f，当前可用余额 ¥%.2f。",
				yuan(r.RefundCent), yuan(r.BalanceAfter)), nil
		},
	}
}

func (tb *Toolbox) subscribePlanTool() *execTool {
	return &execTool{
		name:        "subscribe_plan",
		description: "订购定期订阅套餐：月费 ¥X 换每 N 小时滚动的各档次 token 额度（GPT Plus 式，不入账钱）。需要用户确认。",
		confirm:     true,
		parameters: obj(map[string]any{
			"plan_id": numProp("套餐 ID（用 list_plans 查询）"),
		}, []string{"plan_id"}),
		preview: func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
			plan, err := tb.findPlan(ctx, tenantID, args)
			if err != nil {
				return "", err
			}
			// M9：只有定期订阅在售，月费换额度资格（不入账钱）。
			return fmt.Sprintf("将订阅「%s」：月费 ¥%d，每 %d 小时刷新额度：%s。确认后开通。",
				plan.Name, plan.PriceMoney, plan.RefreshHours, planQuotaLine(plan)), nil
		},
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			planID, err := argInt(args, "plan_id")
			if err != nil {
				return "", err
			}
			if err := tb.postJSON(ctx, "/admin/subscribe", map[string]any{"tenant_id": tenantID, "plan_id": planID}, nil); err != nil {
				return "", err
			}
			name := fmt.Sprintf("#%d", planID)
			if plan, err := tb.findPlan(ctx, tenantID, args); err == nil {
				name = plan.Name
			}
			return fmt.Sprintf("订阅成功：已开通套餐「%s」并到账本期额度。", name), nil
		},
	}
}

func (tb *Toolbox) changePlanTool() *execTool {
	return &execTool{
		name:        "change_plan",
		description: "改套餐：仅定期订阅之间，下期生效（当期额度不变）。需要用户确认。",
		confirm:     true,
		parameters: obj(map[string]any{
			"plan_id": numProp("目标套餐 ID（用 list_plans 查询，须为定期套餐）"),
		}, []string{"plan_id"}),
		preview: func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
			plan, err := tb.findPlan(ctx, tenantID, args)
			if err != nil {
				return "", err
			}
			if plan.PlanType != store.PlanTypeRecurring {
				return "", fmt.Errorf("只能改成定期订阅套餐（%s 是一次性套餐）", plan.Name)
			}
			return fmt.Sprintf("将把当前订阅改为定期套餐「%s」：下期续费生效，当期已订购额度与到期日不变。确认后生效。", plan.Name), nil
		},
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			planID, err := argInt(args, "plan_id")
			if err != nil {
				return "", err
			}
			if err := tb.postJSON(ctx, "/admin/change-plan", map[string]any{"tenant_id": tenantID, "plan_id": planID}, nil); err != nil {
				return "", err
			}
			return fmt.Sprintf("已设置：下期续费将切换为套餐 #%d。", planID), nil
		},
	}
}

func (tb *Toolbox) cancelSubscriptionTool() *execTool {
	return &execTool{
		name:        "cancel_subscription",
		description: "退订当前定期订阅：当期不退、只取消下期续费（话费口径）。一次性套餐不可退订。需要用户确认。",
		confirm:     true,
		parameters:  obj(nil, nil),
		preview: func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
			v, err := tb.me(ctx, tenantID)
			if err != nil {
				return "", err
			}
			if v.Subscription == nil {
				return "", fmt.Errorf("当前没有生效中的订阅，无需退订")
			}
			s := v.Subscription
			end := "无到期日"
			if s.CycleEnd != nil {
				end = s.CycleEnd.Format("2006-01-02")
			}
			return fmt.Sprintf("将退订当前套餐（#%d）：当期不退、只取消下期续费，本期用到 %s 为止。确认后退订。", s.PlanID, end), nil
		},
		run: func(ctx context.Context, args map[string]any, tenantID uint64, _ string) (string, error) {
			var c store.CancelResult
			if err := tb.postJSON(ctx, "/admin/cancel-subscription", map[string]any{"tenant_id": tenantID}, &c); err != nil {
				return "", err
			}
			end := "无到期日"
			if c.CurrentCycleEnd != nil {
				end = c.CurrentCycleEnd.Format("2006-01-02")
			}
			return fmt.Sprintf("已退订套餐「%s」：无退款（当期不退），本期用到 %s 为止，下期不再续费。", c.PlanName, end), nil
		},
	}
}

// ---------- Toolbox 内部 helper（billing admin 访问 + 租户作用域校验） ----------

func (tb *Toolbox) me(ctx context.Context, tenantID uint64) (*store.MeView, error) {
	raw, err := tb.admin.get(ctx, "/admin/me", url.Values{"tenant_id": {fmt.Sprint(tenantID)}})
	if err != nil {
		return nil, err
	}
	var v store.MeView
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("me 响应解析失败: %v", err)
	}
	return &v, nil
}

func (tb *Toolbox) balance(ctx context.Context, tenantID uint64) (int64, error) {
	v, err := tb.me(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	return v.Balance, nil
}

// findPaidOrder 校验 order_no 属于该租户且已支付（防幻觉/越权：LLM 不能指定别人的单）。
func (tb *Toolbox) findPaidOrder(ctx context.Context, tenantID uint64, orderNo string) (*store.RechargeOrder, error) {
	raw, err := tb.admin.get(ctx, "/admin/recharge-orders", url.Values{"tenant_id": {fmt.Sprint(tenantID)}})
	if err != nil {
		return nil, err
	}
	var res struct {
		Items []store.RechargeOrder `json:"items"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("订单查询失败: %v", err)
	}
	for i := range res.Items {
		if res.Items[i].OrderNo == orderNo {
			if res.Items[i].Status != store.RechargePaid {
				return nil, fmt.Errorf("订单 %s 尚未支付，无法退款", orderNo)
			}
			return &res.Items[i], nil
		}
	}
	return nil, fmt.Errorf("未找到你名下的订单 %s", orderNo)
}

// findPlan 从目录里取套餐并校验存在且上架。
func (tb *Toolbox) findPlan(ctx context.Context, tenantID uint64, args map[string]any) (*store.Plan, error) {
	planID, err := argInt(args, "plan_id")
	if err != nil {
		return nil, err
	}
	v, err := tb.me(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for i := range v.Plans {
		if v.Plans[i].ID == uint64(planID) {
			if v.Plans[i].Status != 0 {
				return nil, fmt.Errorf("套餐 #%d 已下架", planID)
			}
			return &v.Plans[i], nil
		}
	}
	return nil, fmt.Errorf("套餐 #%d 不存在", planID)
}

func (tb *Toolbox) postJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := tb.admin.post(ctx, path, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("响应解析失败: %v", err)
	}
	return nil
}

// ---------- 小工具 ----------

func obj(props map[string]any, required []string) map[string]any {
	// 空参数工具的 schema 也必须给空对象/空数组：DeepSeek 会校验整个 tools 数组，
	// `properties`/`required` 为 null 会直接 400（Invalid schema: null is not of type "array"）。
	if props == nil {
		props = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func minI64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// yuan 分 → 元（内部金额单位 = 分，1 元 = 100 分；token 只作用量单位出现，不再换算金额）。
func yuan(cent int64) float64 {
	return float64(cent) / 100
}

// planQuotaLine 套餐各档次每窗口额度展示（按贵贱顺序）：「轻量200万/文本100万/推理50万/旗舰20万」
func planQuotaLine(p *store.Plan) string {
	qm := p.TierQuotaMap()
	parts := make([]string, 0, len(qm))
	for _, t := range store.TierNames() {
		if q, ok := qm[t]; ok {
			parts = append(parts, t+tokensCN(q))
		}
	}
	return strings.Join(parts, "/")
}

// tokensCN token 数转中文可读形式：2000000→200万，1200000→120万，800000→80万，12345→1.2万，500→500。
func tokensCN(n int64) string {
	if n%10000 == 0 {
		return fmt.Sprintf("%d万", n/10000)
	}
	if n > 10000 {
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	}
	return fmt.Sprintf("%d", n)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
