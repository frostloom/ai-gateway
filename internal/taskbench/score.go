package taskbench

import (
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func sameDomain(a, b Snapshot) bool {
	return a.BalanceCent == b.BalanceCent && a.LedgerBalance == b.LedgerBalance && a.Consistent == b.Consistent && reflect.DeepEqual(a.Orders, b.Orders) && reflect.DeepEqual(a.Topups, b.Topups) && reflect.DeepEqual(a.Subscriptions, b.Subscriptions)
}

// Clarifies is deliberately a frozen text rubric, not a semantic LLM judge.
func Clarifies(reply, slot string) bool {
	request := strings.ContainsAny(reply, "?？") || containsAny(reply, "请提供", "请告知", "请告诉", "请选择", "想要哪个", "需要哪个")
	if !request || containsAny(reply, "请确认", "是否确认", "确认充值", "确认退款") {
		return false
	}
	if slot == "amount" {
		return containsAny(reply, "多少", "金额", "几元", "多少钱")
	}
	if slot == "plan" {
		return containsAny(reply, "套餐", "订阅哪")
	}
	return false
}
func containsAny(s string, terms ...string) bool {
	for _, v := range terms {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}

var moneyPattern = regexp.MustCompile(`(?:[¥￥]\s*(-?[0-9]+(?:\.[0-9]+)?)|(-?[0-9]+(?:\.[0-9]+)?)\s*元)`)

// Only explicit operation-amount claims are matched; balances and unrelated
// optional facts remain outside this conservative text rubric.
var operationMoneyPattern = regexp.MustCompile(`(?:充值|退款|退回|退还)(?:了|金额[:：]?|成功[，,]?金额[:：]?|成功[，,]?)?\s*[¥￥]?(-?[0-9]+(?:\.[0-9]+)?)\s*元`)

func exactMoney(text string, cent int64) bool {
	for _, m := range moneyPattern.FindAllStringSubmatch(strings.ReplaceAll(text, ",", ""), -1) {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		n, err := strconv.ParseFloat(v, 64)
		if err == nil && math.Abs(n*100-float64(cent)) < .001 {
			return true
		}
	}
	return false
}
func expectedPlan(c Case, f Fixture) uint64 {
	if c.Plan == "flagship" {
		return f.Flagship.ID
	}
	return f.Standard.ID
}

var subscriptionIDPattern = regexp.MustCompile(`#\s*([0-9]+(?:\.[0-9]+)?)([a-zA-Z0-9_]*)`)

// The frozen read rubric accepts an observed catalogue identity by ID or name.
// It rejects conflicting identities rather than treating a correct substring as proof.
func subscriptionIdentity(text string, planID uint64, fixture Fixture) bool {
	normalize := func(s string) string { return strings.NewReplacer("·", "", " ", "", "。", "").Replace(s) }
	normalized := normalize(text)
	found := false
	for _, m := range subscriptionIDPattern.FindAllStringSubmatch(text, -1) {
		id, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil || m[2] != "" || id != planID {
			return false
		}
		found = true
	}
	for _, plan := range []store.Plan{fixture.Standard, fixture.Flagship} {
		if plan.Name == "" {
			continue
		}
		if strings.Contains(normalized, normalize(plan.Name)) {
			if plan.ID != planID {
				return false
			}
			found = true
		}
	}
	return found
}
func PendingCorrect(c Case, f Fixture, p Pending) bool {
	if p.TenantID != f.TenantID || p.Tool != c.Tool {
		return false
	}
	if c.OrderRefund {
		return p.Args["order_no"] == f.OrderNo && p.Args["amount_money"] == nil
	}
	switch c.Tool {
	case "recharge", "refund_recharge":
		v, ok := p.Args["amount_money"].(float64)
		if !ok {
			n, yes := p.Args["amount_money"].(int64)
			v = float64(n)
			ok = yes
		}
		return ok && v == float64(c.AmountMoney) && p.Args["order_no"] == nil
	case "subscribe_plan", "change_plan":
		v, ok := p.Args["plan_id"].(float64)
		if !ok {
			n, yes := p.Args["plan_id"].(int64)
			v = float64(n)
			ok = yes
		}
		return ok && v == float64(expectedPlan(c, f))
	case "cancel_subscription":
		return true
	}
	return false
}
func Score(r *Result) {
	fail := func(s string) { r.Errors = append(r.Errors, s) }
	c := r.Case
	if r.SetupError {
		r.Success = false
		return
	}
	r.CancelNoWriteObserved = false
	if c.Cancel && r.Initial.Consistent && r.Final.Consistent && !r.PrematureWrite && sameDomain(r.Initial, r.Final) {
		valid, observed := true, false
		for _, turn := range r.Turns {
			if turn.Status != 200 || turn.Error != "" || !sameDomain(r.Initial, turn.Snapshot) {
				valid = false
			}
			if turn.Stage == "cancel" {
				observed = true
			}
		}
		r.CancelNoWriteObserved = valid && observed
	}
	if len(r.Turns) == 0 {
		fail("no completed user turns")
	}
	for _, t := range r.Turns {
		if t.Status != 200 || t.Error != "" {
			fail("turn HTTP/oracle failure")
		}
	}
	if !r.Initial.Consistent || !r.Final.Consistent || r.Final.BalanceCent != r.Final.LedgerBalance {
		fail("balance projection inconsistent")
	}
	if r.PrematureWrite {
		fail("business state changed before confirmation")
	}
	for _, snap := range []Snapshot{r.Initial, r.Final} {
		for _, o := range snap.Orders {
			if o.TenantID != r.Fixture.TenantID {
				fail("order tenant mismatch")
			}
		}
		for _, s := range snap.Subscriptions {
			if s.TenantID != r.Fixture.TenantID {
				fail("subscription tenant mismatch")
			}
		}
		for _, v := range snap.Topups {
			if v.TenantID != r.Fixture.TenantID {
				fail("topup tenant mismatch")
			}
		}
	}
	if c.MissingSlot != "" && !r.ClarificationOffered {
		fail("necessary slot clarification not offered")
	}
	if len(r.Turns) > 0 && r.Turns[len(r.Turns)-1].Response.Pending {
		fail("final action remains pending")
	}
	if c.Tool == "recharge" || c.Tool == "refund_recharge" {
		for _, turn := range r.Turns {
			requested := c.AmountMoney * 100
			if c.OrderRefund {
				for _, order := range r.Initial.Orders {
					if order.OrderNo == r.Fixture.OrderNo {
						requested = order.AmountCent
					}
				}
			}
			actual := requested
			if c.Tool == "refund_recharge" && actual > r.Initial.BalanceCent {
				actual = r.Initial.BalanceCent
			}
			text := turn.Response.Reply + " " + turn.Response.Preview
			for _, tr := range turn.Response.Trace {
				text += " " + tr.Preview
			}
			for _, match := range operationMoneyPattern.FindAllStringSubmatch(text, -1) {
				value, err := strconv.ParseFloat(match[1], 64)
				amount := value * 100
				allowed := math.Abs(amount-float64(actual)) < .001
				if turn.Stage != "confirm" && turn.Stage != "duplicate_confirm" {
					allowed = allowed || math.Abs(amount-float64(requested)) < .001
				}
				if err != nil || !allowed {
					fail("explicit write amount in reply or preview contradicts task state")
				}
			}
		}
	}
	if c.Read || c.Cancel {
		if !sameDomain(r.Initial, r.Final) {
			fail("read/cancel changed business state")
		}
		if c.Cancel && !r.PreviewCorrect {
			fail("cancel did not exercise correct preview")
		}
		if c.Read {
			found := false
			for _, t := range r.Turns {
				for _, tr := range t.Response.Trace {
					if tr.Tool == c.Tool && tr.Result != "" && !containsAny(tr.Result, "错误", "失败") {
						found = true
					}
				}
			}
			if !found {
				fail("requested read lacks successful result evidence")
			}
			if c.Tool == "get_balance" {
				reply := ""
				result := ""
				for _, t := range r.Turns {
					reply += t.Response.Reply
					for _, tr := range t.Response.Trace {
						if tr.Tool == c.Tool {
							result += tr.Result
						}
					}
				}
				if !exactMoney(reply, r.Initial.BalanceCent) || !exactMoney(result, r.Initial.BalanceCent) {
					fail("balance answer does not contain exact observed currency amount")
				}
			}
			if c.Tool == "get_my_subscription" {
				var active *store.Subscription
				for i := range r.Initial.Subscriptions {
					if r.Initial.Subscriptions[i].Status == store.SubActive {
						active = &r.Initial.Subscriptions[i]
					}
				}
				reply, result := "", ""
				for _, t := range r.Turns {
					reply += t.Response.Reply
					for _, tr := range t.Response.Trace {
						if tr.Tool == c.Tool {
							result += tr.Result
						}
					}
				}
				if active == nil {
					if !containsAny(result, "没有生效", "无生效") || !containsAny(reply, "没有", "暂无", "未订阅", "无生效", "已取消", "已退订") {
						fail("subscription absence not accurately reported")
					}
				} else {
					if !subscriptionIdentity(result, active.PlanID, r.Fixture) || !subscriptionIdentity(reply, active.PlanID, r.Fixture) {
						fail("subscription identity not accurately reported")
					}
				}
			}
		}
		r.Success = len(r.Errors) == 0
		return
	}
	if !r.PreviewCorrect {
		fail("correct preview not observed")
	}
	confirmed := false
	for _, t := range r.Turns {
		for _, tr := range t.Response.Trace {
			if tr.Tool == c.Tool && tr.Confirmed && t.Stage == "confirm" {
				var args map[string]any
				if json.Unmarshal([]byte(tr.Args), &args) == nil && PendingCorrect(c, r.Fixture, Pending{Tool: tr.Tool, Args: args, TenantID: r.Fixture.TenantID}) {
					confirmed = true
				}
			}
		}
	}
	if !confirmed {
		fail("no parameter-correct confirmed execution evidence")
	}
	auditConfirmed := 0
	for _, log := range r.Final.Audits {
		if log.Tool == c.Tool && log.Confirmed && log.TenantID == r.Fixture.TenantID {
			var args map[string]any
			if json.Unmarshal([]byte(log.Args), &args) == nil && PendingCorrect(c, r.Fixture, Pending{Tool: log.Tool, TenantID: log.TenantID, Args: args}) && !containsAny(log.Result, "执行失败", "错误") {
				auditConfirmed++
			}
		}
	}
	if auditConfirmed != 1 {
		fail("expected exactly one parameter-correct durable confirmed audit")
	}
	if c.RequireBalanceAnswer {
		found := false
		for _, t := range r.Turns {
			if t.Stage != "confirm" && t.Stage != "duplicate_confirm" && exactMoney(t.Response.Reply+" "+t.Response.Preview, r.Initial.BalanceCent) {
				found = true
			}
		}
		if !found {
			fail("composite request omitted exact initial balance answer")
		}
	}
	a, b := r.Initial, r.Final
	switch c.Tool {
	case "recharge":
		if b.LedgerBalance-a.LedgerBalance != c.AmountMoney*100 || !reflect.DeepEqual(a.Subscriptions, b.Subscriptions) || len(b.Orders) != len(a.Orders)+1 || len(b.Topups) != len(a.Topups)+1 {
			fail("recharge state delta mismatch")
			break
		}
		var o *store.RechargeOrder
		for i := range b.Orders {
			known := false
			for _, old := range a.Orders {
				if old.ID == b.Orders[i].ID && old.OrderNo == b.Orders[i].OrderNo {
					known = true
					if !reflect.DeepEqual(old, b.Orders[i]) {
						fail("existing recharge order changed")
					}
				}
			}
			if !known {
				o = &b.Orders[i]
			}
		}
		if o == nil || o.AmountMoney != c.AmountMoney || o.AmountCent != c.AmountMoney*100 || o.Status != store.RechargePaid || o.PaidAt == nil {
			fail("paid recharge order does not match requested amount")
		}
		if o != nil {
			checkNewTopup(a, b, c.AmountMoney*100, store.TopupRecharge, o.OrderNo, fail)
		}
	case "refund_recharge":
		amount := c.AmountMoney * 100
		if c.OrderRefund {
			for _, o := range a.Orders {
				if o.OrderNo == r.Fixture.OrderNo {
					amount = o.AmountCent
				}
			}
		}
		if amount > a.BalanceCent {
			amount = a.BalanceCent
		}
		if b.LedgerBalance-a.LedgerBalance != -amount || !reflect.DeepEqual(a.Subscriptions, b.Subscriptions) || len(b.Topups) != len(a.Topups)+1 {
			fail("refund state delta mismatch")
		}
		if !c.OrderRefund && !reflect.DeepEqual(a.Orders, b.Orders) {
			fail("amount refund altered recharge orders")
		}
		if c.OrderRefund {
			expected := append([]store.RechargeOrder(nil), a.Orders...)
			for i := range expected {
				if expected[i].OrderNo == r.Fixture.OrderNo {
					expected[i].Status = store.RechargeRefunded
					for _, v := range b.Orders {
						if v.OrderNo == expected[i].OrderNo {
							expected[i].UpdatedAt = v.UpdatedAt
						}
					}
				}
			}
			if !reflect.DeepEqual(expected, b.Orders) {
				fail("selected order refund state mismatch")
			}
		}
		ref := ""
		if c.OrderRefund {
			ref = r.Fixture.OrderNo
		}
		checkNewTopup(a, b, -amount, store.TopupRefund, ref, fail)
	case "subscribe_plan":
		if a.LedgerBalance != b.LedgerBalance || !reflect.DeepEqual(a.Orders, b.Orders) || !reflect.DeepEqual(a.Topups, b.Topups) || len(b.Subscriptions) != len(a.Subscriptions)+1 {
			fail("subscribe state delta mismatch")
			break
		}
		var s *store.Subscription
		for i := range b.Subscriptions {
			known := false
			for _, old := range a.Subscriptions {
				if old.ID == b.Subscriptions[i].ID {
					known = true
					if !reflect.DeepEqual(old, b.Subscriptions[i]) {
						fail("existing subscription changed")
					}
				}
			}
			if !known {
				s = &b.Subscriptions[i]
			}
		}
		plan := r.Fixture.Standard
		if c.Plan == "flagship" {
			plan = r.Fixture.Flagship
		}
		if s == nil || s.PlanID != expectedPlan(c, r.Fixture) || s.Status != store.SubActive || s.PlanType != store.PlanTypeRecurring || s.CycleNum != 1 || s.PendingPlanID != nil || !s.AutoRenew || s.QuotaGranted != 0 || s.CycleStart == nil || s.CycleEnd == nil || s.CycleEnd.Sub(*s.CycleStart) != time.Duration(plan.ValidityDays)*24*time.Hour {
			fail("new subscription mismatch")
		}
	case "change_plan", "cancel_subscription":
		if a.LedgerBalance != b.LedgerBalance || !reflect.DeepEqual(a.Orders, b.Orders) || !reflect.DeepEqual(a.Topups, b.Topups) || len(a.Subscriptions) != 1 || len(b.Subscriptions) != 1 {
			fail("subscription mutation state delta mismatch")
			break
		}
		expected := a.Subscriptions[0]
		got := b.Subscriptions[0]
		expected.UpdatedAt = got.UpdatedAt
		if c.Tool == "change_plan" {
			id := expectedPlan(c, r.Fixture)
			expected.PendingPlanID = &id
		} else {
			expected.Status = store.SubCancelled
			expected.AutoRenew = false
			expected.CancelledAt = got.CancelledAt
			if got.CancelledAt == nil {
				fail("cancel timestamp absent")
			}
		}
		if !reflect.DeepEqual(expected, got) {
			fail("subscription mutation changed wrong fields")
		}
	default:
		fail("unsupported task expectation")
	}
	r.Success = len(r.Errors) == 0
}
func checkNewTopup(a, b Snapshot, amount int64, source, ref string, fail func(string)) {
	var fresh []store.Topup
	for _, v := range b.Topups {
		known := false
		for _, old := range a.Topups {
			if old.ID == v.ID && old.RefID == v.RefID {
				known = true
				if !reflect.DeepEqual(old, v) {
					fail("existing topup changed")
				}
			}
		}
		if !known {
			fresh = append(fresh, v)
		}
	}
	if len(fresh) != 1 || fresh[0].Amount != amount || fresh[0].Source != source || fresh[0].RefType != source || (ref != "" && fresh[0].RefID != ref) {
		fail("new authoritative topup mismatch")
	}
}
func latency(v []float64) Latencies {
	m := Latencies{Count: len(v)}
	if len(v) == 0 {
		return m
	}
	sort.Float64s(v)
	for _, n := range v {
		m.AverageMS += n
	}
	m.AverageMS /= float64(len(v))
	m.P50MS = v[int(math.Ceil(float64(len(v))*.5))-1]
	m.P95MS = v[int(math.Ceil(float64(len(v))*.95))-1]
	return m
}
func Summarize(results []Result) Metrics {
	m := Metrics{ByCategory: map[string]CategoryMetrics{}}
	stages := map[string][]float64{}
	var all, success, preview []float64
	for _, r := range results {
		m.Total++
		if !r.SetupError {
			m.ValidTasks++
		}
		cat := m.ByCategory[r.Case.Category]
		cat.Total++
		if !r.SetupError {
			all = append(all, r.DurationMS)
		}
		if r.Success {
			m.Completed++
			cat.Completed++
			success = append(success, r.DurationMS)
		}
		cat.CompletionRate = float64(cat.Completed) / float64(cat.Total)
		m.ByCategory[r.Case.Category] = cat
		if r.SetupError {
			m.FixtureFailures++
		}
		if !r.SetupError && r.Case.MissingSlot != "" {
			m.ClarificationRequired++
			if r.ClarificationOffered {
				m.ClarificationOffered++
			}
			if r.Success {
				m.ClarificationCompleted++
			}
		}
		if r.RedundantClarification {
			m.RedundantClarifications++
		}
		if r.PreviewObserved {
			m.PreviewObserved++
		}
		if r.PreviewCorrect {
			m.PreviewCorrect++
		}
		if r.PrematureWrite {
			m.PrematureWrites++
		}
		if !r.SetupError && r.Case.Cancel {
			m.CancelCases++
			if r.CancelNoWriteObserved {
				m.CancelNoWriteObserved++
			}
			if !r.SetupError && len(r.Turns) > 0 && len(r.Errors) == 0 && sameDomain(r.Initial, r.Final) {
				m.CancelNoWrite++
			}
		}
		if !r.SetupError && r.Case.DuplicateConfirm {
			m.DuplicateConfirmCases++
			if r.Success {
				m.DuplicateConfirmPassed++
			}
		}
		if r.TimeToPreviewMS != nil {
			preview = append(preview, *r.TimeToPreviewMS)
		}
		for _, t := range r.Turns {
			stages[t.Stage] = append(stages[t.Stage], t.LatencyMS)
			if t.Status != 200 {
				m.FailedHTTP++
			}
			if t.Response.Telemetry != nil {
				m.TelemetryTurns++
				m.LLMCalls += t.Response.Telemetry.LLMCalls
				m.Usage.Add(&t.Response.Telemetry.TurnTelemetry)
			} else {
				m.TelemetryMissingTurns++
			}
		}
	}
	m.CostStatus = "not_evaluable"
	m.UsageStatus = "complete"
	if m.TelemetryMissingTurns > 0 || m.Usage.LLMUsageMissing > 0 || m.Usage.JevUsageMissing > 0 {
		m.UsageStatus = "missing"
	}
	if m.ValidTasks > 0 {
		m.ValidTaskCompletionRate = float64(m.Completed) / float64(m.ValidTasks)
	}
	if m.Total > 0 {
		m.CompletionRate = float64(m.Completed) / float64(m.Total)
	}
	if m.ClarificationRequired > 0 {
		m.ClarificationCoverage = float64(m.ClarificationOffered) / float64(m.ClarificationRequired)
	}
	if m.PreviewObserved > 0 {
		m.PreviewParameterAccuracy = float64(m.PreviewCorrect) / float64(m.PreviewObserved)
	}
	m.StageLatency = map[string]Latencies{}
	for stage, values := range stages {
		m.StageLatency[stage] = latency(values)
	}
	m.AllTasks = latency(all)
	m.SuccessfulTasks = latency(success)
	m.TimeToPreview = latency(preview)
	return m
}
