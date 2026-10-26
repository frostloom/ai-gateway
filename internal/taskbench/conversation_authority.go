package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Optional independent query evidence. Absence in a historical record is not an
// empty catalogue/bill list. Captured queries are for this tenant and time only.
type ConversationAnswerAuthority struct {
	TenantID            uint64          `json:"tenant_id"`
	CapturedAt          time.Time       `json:"captured_at"`
	CatalogComplete     bool            `json:"catalog_complete"`
	Plans               []store.Plan    `json:"plans"`
	RecentBills         []store.Bill    `json:"recent_bills"`
	BillsLimit          int             `json:"bills_limit"`
	BillsTotal          int64           `json:"bills_total"`
	BillsPhase          string          `json:"bills_phase"`
	BillsStatus         string          `json:"bills_status"`
	Consumption         []store.DayStat `json:"consumption"`
	ConsumptionComplete bool            `json:"consumption_complete"`
	ConsumptionDays     int             `json:"consumption_days"`
	RefreshHours        int             `json:"refresh_hours"`
}

func CaptureConversationAnswerAuthority(ctx context.Context, st *store.Store, tenant uint64, at time.Time) (*ConversationAnswerAuthority, error) {
	if st == nil || tenant == 0 || at.IsZero() {
		return nil, fmt.Errorf("authority needs store, tenant and capture time")
	}
	me, err := st.MeOverview(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return captureAuthorityWithOverview(ctx, st, tenant, at, me)
}

// The overview must come from this tenant's current capture, never a previous State call.
func captureAuthorityWithOverview(ctx context.Context, st *store.Store, tenant uint64, at time.Time, me *store.MeView) (*ConversationAnswerAuthority, error) {
	bills, total, err := st.ListBills(ctx, tenant, "settle", "settled", 10, 0)
	if err != nil {
		return nil, err
	}
	return &ConversationAnswerAuthority{TenantID: tenant, CapturedAt: at, CatalogComplete: true, Plans: me.Plans, RecentBills: bills, BillsLimit: 10, BillsTotal: total, BillsPhase: "settle", BillsStatus: "settled", Consumption: me.Consumption, ConsumptionComplete: true, ConsumptionDays: 7, RefreshHours: me.RefreshHours}, nil
}

func validateAnswerAuthority(s ConversationState, f Fixture) error {
	a := s.AnswerAuthority
	if a == nil {
		return nil
	}
	if a.TenantID != f.TenantID || a.CapturedAt.IsZero() || !a.CapturedAt.Equal(s.CapturedAt) || a.BillsLimit < 1 || a.BillsLimit > 200 || a.BillsTotal < 0 || a.BillsPhase != "settle" || a.BillsStatus != "settled" {
		return fmt.Errorf("answer authority tenant/time/query mismatch")
	}
	count := a.BillsTotal
	if count > int64(a.BillsLimit) {
		count = int64(a.BillsLimit)
	}
	if int64(len(a.RecentBills)) != count {
		return fmt.Errorf("answer authority bill coverage incomplete")
	}
	previous := ^uint64(0)
	for _, b := range a.RecentBills {
		if b.TenantID != f.TenantID || b.Phase != a.BillsPhase || b.Status != a.BillsStatus || b.ID == 0 || b.ID >= previous {
			return fmt.Errorf("answer authority foreign/unordered bill")
		}
		previous = b.ID
	}
	seen := map[uint64]bool{}
	for _, p := range a.Plans {
		if p.ID == 0 || seen[p.ID] || p.PriceMoney < 0 || p.RefreshHours < 0 {
			return fmt.Errorf("invalid authority catalogue")
		}
		seen[p.ID] = true
	}
	if a.ConsumptionComplete && a.ConsumptionDays != 7 {
		return fmt.Errorf("answer authority consumption scope mismatch")
	}
	days := map[string]bool{}
	firstDay := a.CapturedAt.Add(-6 * 24 * time.Hour).Format("2006-01-02")
	lastDay := a.CapturedAt.Format("2006-01-02")
	for _, d := range a.Consumption {
		if _, err := time.Parse("2006-01-02", d.Day); err != nil || days[d.Day] || d.Tokens < 0 || d.Day < firstDay || d.Day > lastDay {
			return fmt.Errorf("invalid authority daily consumption")
		}
		days[d.Day] = true
	}
	return nil
}

var authorityPlanLine = regexp.MustCompile(`(?m)^套餐#([0-9]+) ([^\n]+?)（定期订阅）· 月费 ([¥￥][0-9]+(?:\.[0-9]+)?) · 每 ([0-9]+) 小时刷新额度：([^\n]+)`)
var authorityTier = regexp.MustCompile(`(轻量|文本|推理|旗舰)([0-9]+(?:\.[0-9]+)?万|[0-9]+)`)
var authorityAllowance = regexp.MustCompile(`(轻量|文本|推理|旗舰) ([0-9]+(?:\.[0-9]+)?万|[0-9]+)/([0-9]+(?:\.[0-9]+)?万|[0-9]+)`)
var authorityCycle = regexp.MustCompile(`本期到 ([0-9]{4}-[0-9]{2}-[0-9]{2})`)
var authorityRefresh = regexp.MustCompile(`每 ([0-9]+) 小时滚动刷新`)
var authorityBillCount = regexp.MustCompile(`最近 ([0-9]+) 笔消费：`)
var authorityBillLine = regexp.MustCompile(`(?m)^([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}) · ([^·\n]+) · 用量 ([0-9]+) token · 扣费 ([¥￥][0-9]+(?:\.[0-9]+)?)`)
var authorityBillTotal = regexp.MustCompile(`合计扣费 ([¥￥][0-9]+(?:\.[0-9]+)?)`)
var authorityDay = regexp.MustCompile(`([0-9]{4}-[0-9]{2}-[0-9]{2}) 消耗 ([¥￥][0-9]+(?:\.[0-9]+)?)`)
var authorityDailyTotal = regexp.MustCompile(`合计 ([¥￥][0-9]+(?:\.[0-9]+)?)`)

// Only rounded *display* equality is checked for 万. Never assert that 1.2万
// recovers an exact source quantity of 12000; authority retains the exact value.
func authorityTokenDisplay(n int64) string {
	if n%10000 == 0 {
		return fmt.Sprintf("%d万", n/10000)
	}
	if n > 10000 {
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	}
	return fmt.Sprint(n)
}
func tokenDisplayMatches(raw string, n int64) bool {
	if strings.HasSuffix(raw, "万") {
		return raw == authorityTokenDisplay(n)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	return err == nil && v == n
}

func checkAdditionalConversationFacts(r ConversationResult) []ConversationFactCheck {
	checks := []ConversationFactCheck{}
	for i, obs := range r.Observations {
		text := obs.Turn.Response.Reply
		a := obs.State.AnswerAuthority
		add := func(fact, claim, expected string, status string) {
			checks = append(checks, ConversationFactCheck{i, fact, claim, expected, status})
		}
		check := func(fact, claim, expected string, ok bool) {
			status := "mismatch"
			if ok {
				status = "match"
			}
			add(fact, claim, expected, status)
		}
		money := func(fact, claim string, cent int64) {
			got, ok := claimCent(claim)
			check(fact, claim, fmt.Sprintf("%d cent", cent), ok && got == cent)
		}
		if a != nil {
			if err := validateAnswerAuthority(obs.State, r.Fixture); err != nil {
				add("invalid_answer_authority", "", err.Error(), "mismatch")
				continue
			}
		}
		if strings.Contains(text, "如果") || strings.Contains(text, "假设") || strings.ContainsAny(text, "“”\"") || strings.Contains(text, "你说") {
			continue
		}
		for _, m := range authorityPlanLine.FindAllStringSubmatch(text, -1) {
			if a == nil {
				add("plan_authority_missing", m[0], "independent catalogue", "not_evaluated")
				continue
			}
			id, _ := strconv.ParseUint(m[1], 10, 64)
			var plan *store.Plan
			for j := range a.Plans {
				if a.Plans[j].ID == id && a.Plans[j].Status == 0 {
					plan = &a.Plans[j]
				}
			}
			if plan == nil {
				if a.CatalogComplete {
					check("catalogue_plan", m[1], "active catalogue plan", false)
				} else {
					add("partial_catalogue", m[1], "unknown plan", "not_evaluated")
				}
				continue
			}
			check("plan_name", m[2], plan.Name, m[2] == plan.Name)
			check("plan_type", "定期订阅", store.PlanTypeRecurring, plan.PlanType == store.PlanTypeRecurring)
			if plan.PriceMoney <= 90071992547409 {
				money("plan_monthly_price", m[3], plan.PriceMoney*100)
			}
			check("plan_refresh_hours", m[4], fmt.Sprint(plan.RefreshHours), m[4] == fmt.Sprint(plan.RefreshHours))
			quota := plan.TierQuotaMap()
			for _, tier := range authorityTier.FindAllStringSubmatch(m[5], -1) {
				v, ok := quota[tier[1]]
				check("plan_quota_display", tier[0], fmt.Sprintf("%s=%d tokens (display %s)", tier[1], v, authorityTokenDisplay(v)), ok && tokenDisplayMatches(tier[2], v))
			}
		}
		if subscriptionClaim.MatchString(text) {
			sub := conversationEntitlement(obs.State)
			for _, m := range authorityCycle.FindAllStringSubmatch(text, -1) {
				if sub == nil || sub.CycleEnd == nil {
					add("subscription_cycle", m[1], "missing entitled cycle", "not_evaluated")
				} else {
					check("subscription_cycle", m[1], sub.CycleEnd.Format("2006-01-02"), m[1] == sub.CycleEnd.Format("2006-01-02"))
				}
			}
			for _, m := range authorityAllowance.FindAllStringSubmatch(text, -1) {
				found := false
				for _, tier := range obs.State.Allowance {
					if tier.Tier == m[1] {
						found = true
						check("allowance_display", m[0], fmt.Sprintf("remaining=%d quota=%d tokens", tier.Remaining, tier.Quota), tokenDisplayMatches(m[2], tier.Remaining) && tokenDisplayMatches(m[3], tier.Quota))
					}
				}
				if !found {
					add("allowance_missing", m[0], "missing tier evidence", "not_evaluated")
				}
			}
			for _, m := range authorityRefresh.FindAllStringSubmatch(text, -1) {
				if a == nil {
					add("refresh_authority_missing", m[0], "missing plan refresh evidence", "not_evaluated")
				} else {
					check("subscription_refresh_hours", m[1], fmt.Sprint(a.RefreshHours), m[1] == fmt.Sprint(a.RefreshHours))
				}
			}
		}
		billClaim := authorityBillCount.MatchString(text) || strings.Contains(text, "最近没有消费记录")
		if billClaim {
			if a == nil {
				add("bill_authority_missing", text, "independent query evidence", "not_evaluated")
			} else {
				limit := int64(3)
				traceCount := 0
				validLimit := true
				for _, tr := range obs.Turn.Response.Trace {
					if tr.Tool == "get_recent_bills" && tr.Phase == "read" && tr.Outcome == "success" {
						traceCount++
						var args map[string]any
						raw := tr.ResolvedArgs
						if raw == "" {
							raw = tr.Args
						}
						if raw != "" && json.Unmarshal([]byte(raw), &args) != nil {
							validLimit = false
						}
						if v, ok := args["limit"]; ok {
							limit = conversationInteger(v)
							if limit < 1 || limit > 10 {
								validLimit = false
							}
						}
					}
				}
				// A reply spanning multiple reads cannot be assigned to one
				// limit without separate claim-to-result attribution.
				if traceCount != 1 || !validLimit || limit > int64(a.BillsLimit) {
					add("bill_query_scope_missing", text, "successful bounded read trace", "not_evaluated")
				} else {
					want := a.BillsTotal
					if want > limit {
						want = limit
					}
					rows := authorityBillLine.FindAllStringSubmatch(text, -1)
					for _, m := range authorityBillCount.FindAllStringSubmatch(text, -1) {
						check("recent_bill_count", m[1], fmt.Sprint(want), m[1] == fmt.Sprint(want))
					}
					if strings.Contains(text, "最近没有消费记录") {
						check("recent_bill_empty", "最近没有消费记录", "zero settled bills", want == 0)
					}
					if len(rows) > 0 {
						check("recent_bill_rows", fmt.Sprint(len(rows)), fmt.Sprint(want), int64(len(rows)) == want)
					}
					total := int64(0)
					knownTotal := true
					for j := int64(0); j < want; j++ {
						b := a.RecentBills[j]
						if b.ActualQuota == nil {
							knownTotal = false
						} else {
							total += *b.ActualQuota
						}
						if j >= int64(len(rows)) {
							continue
						}
						m := rows[j]
						model := b.Model
						if model == "" {
							model = "?"
						}
						check("bill_time_model", m[1]+"/"+m[2], b.CreatedAt.Format("2006-01-02 15:04:05")+"/"+model, m[1] == b.CreatedAt.Format("2006-01-02 15:04:05") && m[2] == model)
						if b.PromptTokens != nil && b.CompletionTokens != nil {
							check("bill_tokens", m[3], fmt.Sprint(*b.PromptTokens+*b.CompletionTokens), m[3] == fmt.Sprint(*b.PromptTokens+*b.CompletionTokens))
						} else {
							add("bill_tokens_missing", m[3], "missing usage", "not_evaluated")
						}
						if b.ActualQuota != nil {
							money("bill_amount", m[4], *b.ActualQuota)
						} else {
							add("bill_amount_missing", m[4], "missing amount", "not_evaluated")
						}
					}
					for _, m := range authorityBillTotal.FindAllStringSubmatch(text, -1) {
						if knownTotal {
							money("recent_bill_total", m[1], total)
						} else {
							add("bill_total_missing", m[1], "missing amounts", "not_evaluated")
						}
					}
				}
			}
		}
		if strings.Contains(text, "近 7 天") {
			if a == nil || !a.ConsumptionComplete || a.ConsumptionDays != 7 {
				add("consumption_scope_missing", text, "independent seven-day cash query", "not_evaluated")
			} else {
				total := int64(0)
				days := map[string]int64{}
				for _, d := range a.Consumption {
					total += d.Tokens
					days[d.Day] = d.Tokens
				}
				if strings.Contains(text, "近 7 天没有消耗记录") {
					if total != 0 {
						check("cash_consumption_empty", text, "nonzero cash consumption proves records", false)
					} else {
						// DailyStats pads dates and zero-cost bills also exist. Zero
						// cash cannot establish absence of usage or billing records.
						add("cash_consumption_records_unknown", text, "daily cash sums do not retain record counts", "not_evaluated")
					}
				}
				for _, m := range authorityDay.FindAllStringSubmatch(text, -1) {
					amount, found := days[m[1]]
					if found {
						money("daily_cash_consumption", m[2], amount)
					} else {
						check("daily_cash_consumption", m[1], "date present in query", false)
					}
				}
				for _, m := range authorityDailyTotal.FindAllStringSubmatch(text, -1) {
					money("seven_day_cash_total", m[1], total)
				}
			}
		}
	}
	return checks
}
