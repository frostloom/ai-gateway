package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// 计价数学：cost = (prompt×in + completion×out) / 1_000_000（分，向下取整）。
func TestCostCent(t *testing.T) {
	cases := []struct {
		p, c, in, out, want int64
	}{
		{0, 0, 100, 200, 0},                 // 零用量零扣费
		{1_000, 1_000, 100, 200, 0},         // (100k+200k)/1M = 0.3 → 0
		{10_000, 10_000, 100, 200, 3},       // (1M+2M)/1M = 3
		{1_000_000, 0, 100, 200, 100},       // 1M prompt × 100 分/M = 100 分
		{500_000, 0, 1_000_000, 0, 500_000}, // 测试模型：estPrompt×1.2 整值的基数
		{0, 100_000, 0, 3_800, 380},         // 100k completion × 3800 分/M
		{1_000, 1_000, 3_800, 7_600, 11},    // (3.8M+7.6M)/1M = 11.4 → 11
	}
	for _, c := range cases {
		if got := quota.CostCent(c.p, c.c, c.in, c.out); got != c.want {
			t.Errorf("CostCent(%d,%d,%d,%d) = %d, want %d", c.p, c.c, c.in, c.out, got, c.want)
		}
	}
	// 预扣 = 成本 × 1.2（向下取整）：3×1.2=3.6 → 3
	if got := quota.ReserveCostCent(10_000, 10_000, 100, 200); got != 3 {
		t.Errorf("ReserveCostCent(10000,10000,100,200) = %d, want 3", got)
	}
	// 精确预占到 0：8334×1.2 = 10000.8 → 10000（TestRefundZeroWhenBalanceSpent 依赖）
	if got := quota.ReserveCostCent(8_334, 0, 1_000_000, 1_000_000); got != 10_000 {
		t.Errorf("ReserveCostCent(8334,0,1M,1M) = %d, want 10000", got)
	}
}

// 未上架/已下架模型：Reserve 拒绝（InvalidArgument → gateway 400），不落行、不动余额。
func TestReserveModelUnavailable(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tid := seedTenant(t, s, 10_000)

	// 不存在于目录
	if _, err := s.Reserve(ctx, reserveReq("no-model-1", tid, 84)); err == nil ||
		!strings.Contains(err.Error(), "model unavailable or not listed") {
		t.Fatalf("want 不存在模型被拒，got %v", err)
	}
	// 已下架（存在但 status=1）
	seedMockModel(t, s)
	var m Model
	if err := s.db.WithContext(ctx).Where("model_id = ?", "mock-model").First(&m).Error; err != nil {
		t.Fatalf("load mock model: %v", err)
	}
	if err := s.UpdateModelStatus(ctx, m.ID, 1); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := s.Reserve(ctx, reserveReq("off-model-1", tid, 84)); err == nil ||
		!strings.Contains(err.Error(), "model unavailable or not listed") {
		t.Fatalf("want 下架模型被拒，got %v", err)
	}
	if n := countReserveRows(s); n != 0 {
		t.Fatalf("reserve rows = %d, want 0（拒绝不应落行）", n)
	}
	if bal, _ := s.getBalance(ctx, tid); bal != 10_000 {
		t.Fatalf("balance = %d, want 10000（拒绝不应动余额）", bal)
	}
}

// 销售统计：settle 行的 actual 按模型/厂商/汇总分组，充值并入 summary 与 by_day。
func TestSalesStats(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	// newTestStore 已 SeedModels：deepseek-v4-flash in=100/out=200，step-2 in=3800/out=7600（分/M）
	t1 := seedTenant(t, s, 5_000) // ¥50
	t2 := seedTenant(t, s, 5_000)
	payOrder(t, s, ctx, t1, 100, "sales-topup-1") // t1 充值 ¥100 → 10,000 分

	// 完整调用链 reserve→report→settle，共 4 单：
	//   deepseek-v4-flash：pre=CostCent(100k,0)×1.2=12，actual=CostCent(100k,50k)=20
	//   step-2：pre=CostCent(100k,0)×1.2=456，actual=CostCent(100k,50k)=760
	cycle := func(rid string, tid uint64, model string) {
		t.Helper()
		if _, err := s.Reserve(ctx, &billingv1.ReserveRequest{
			RequestId: rid, TenantId: int64(tid), ApiKeyId: 1, Model: model, EstPromptTokens: 100_000,
		}); err != nil {
			t.Fatalf("reserve %s: %v", rid, err)
		}
		if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
			RequestId: rid, PromptTokens: 100_000, CompletionTokens: 50_000,
		}); err != nil {
			t.Fatalf("report %s: %v", rid, err)
		}
		if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: rid}); err != nil {
			t.Fatalf("settle %s: %v", rid, err)
		}
	}
	cycle("sales-s1", t1, "deepseek-v4-flash")
	cycle("sales-s2", t1, "deepseek-v4-flash")
	cycle("sales-s3", t1, "step-2")
	cycle("sales-s4", t2, "deepseek-v4-flash")

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	st, err := s.SalesStats(ctx, from, to)
	if err != nil {
		t.Fatalf("sales stats: %v", err)
	}

	// 汇总
	if st.Summary.ConsumedCent != 820 || st.Summary.SettledBills != 4 ||
		st.Summary.ActiveTenants != 2 || st.Summary.TopupCent != 10_000 {
		t.Fatalf("summary = %+v, want 消耗 820 / 4 单 / 2 买家 / 充值 10000", st.Summary)
	}

	// 按模型
	byModel := map[string]SalesModel{}
	for _, m := range st.ByModel {
		byModel[m.ModelID] = m
	}
	if dm := byModel["deepseek-v4-flash"]; dm.Calls != 3 || dm.ConsumedCent != 60 || dm.ActiveTenants != 2 {
		t.Fatalf("deepseek-v4-flash = %+v, want 3 次 / 60 分 / 2 买家", dm)
	}
	if sm := byModel["step-2"]; sm.Calls != 1 || sm.ConsumedCent != 760 || sm.ActiveTenants != 1 {
		t.Fatalf("step-2 = %+v, want 1 次 / 760 分 / 1 买家", sm)
	}

	// 按厂商
	byVendor := map[string]SalesVendor{}
	for _, v := range st.ByVendor {
		byVendor[v.Vendor] = v
	}
	if dv := byVendor["DeepSeek"]; dv.Calls != 3 || dv.ConsumedCent != 60 || dv.ActiveTenants != 2 {
		t.Fatalf("DeepSeek = %+v, want 3 次 / 60 分 / 2 买家", dv)
	}
	if jy := byVendor["阶跃星辰"]; jy.Calls != 1 || jy.ConsumedCent != 760 || jy.ActiveTenants != 1 {
		t.Fatalf("阶跃星辰 = %+v, want 1 次 / 760 分 / 1 买家", jy)
	}

	// 按天：数据的实际发生日应有 消耗 820 + 充值 10000。
	//
	// 查询窗口是 [now-1h, now+1h]，临近午夜时会横跨两个自然日，
	// 首桶可能只有充值或只有消耗，所以取两项合计最大的那天来断言，
	// 而不是硬编码 ByDay[0]。（分桶键口径另见 TestSalesStatsByDayUsesLocalDay）
	var (
		day      *SalesDay
		totalDay int64
	)
	for i := range st.ByDay {
		d := &st.ByDay[i]
		if d.ConsumedCent+d.TopupCent > totalDay {
			day, totalDay = d, d.ConsumedCent+d.TopupCent
		}
	}
	if day == nil {
		t.Fatalf("by_day 为空: %+v", st.ByDay)
	}
	if day.ConsumedCent != 820 || day.TopupCent != 10_000 {
		t.Fatalf("by_day[%s] = %+v, want 消耗 820 / 充值 10000（全部桶: %+v）", day.Day, *day, st.ByDay)
	}

	// 余额一致性：t1 充值 10000 + initial 5000 − 实际消耗（20+20+760）= 14200
	if bal, _ := s.getBalance(ctx, t1); bal != 14_200 {
		t.Fatalf("t1 balance = %d, want 14200（reserve/settle 冲正后等于实际消耗）", bal)
	}
	if ledger, err := LedgerBalance(ctx, s.db, t1); err != nil || ledger != 14_200 {
		t.Fatalf("t1 ledger = %d err=%v, want 14200", ledger, err)
	}
}

// 回归：by_day 的补齐键必须与 MySQL 侧 DATE_FORMAT 的日期口径一致。
//
// 历史缺陷：补齐循环用了 from.Truncate(24*time.Hour)，那是按 UTC 绝对时间截断；
// 在东八区会把「今天 00:30」截到「昨天 00:30」，生成的 key 比 DATE_FORMAT 返回的
// 日期早一天，于是真实桶全部匹配不上、只剩补零占位 —— 前端销售趋势恒为 0，
// 而旧断言只看 ByDay[0]，恰好被这个占位符骗过。
//
// 这里直接构造一个非 UTC 时区的窗口，断言窗口起始那天的桶里能拿到真实数据。
func TestSalesStatsByDayUsesLocalDay(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	tid := seedTenant(t, s, 5_000)
	payOrder(t, s, ctx, tid, 100, "tz-topup-1") // 充值 10,000 分

	if _, err := s.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "tz-1", TenantId: int64(tid), ApiKeyId: 1,
		Model: "deepseek-v4-flash", EstPromptTokens: 100_000,
	}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: "tz-1", PromptTokens: 100_000, CompletionTokens: 50_000,
	}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: "tz-1"}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// 东八区窗口：这会让 UTC 截断与本地自然日错开一天，正是缺陷的触发条件。
	loc := time.FixedZone("CST", 8*3600)
	now := time.Now().In(loc)
	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)

	st, err := s.SalesStats(ctx, from, to)
	if err != nil {
		t.Fatalf("sales stats: %v", err)
	}
	if len(st.ByDay) == 0 {
		t.Fatal("by_day 为空")
	}

	// 窗口起始的本地自然日必须出现在结果里，并且带上真实数据。
	wantKey := from.Format("2006-01-02")
	var got *SalesDay
	var sum int64
	for i := range st.ByDay {
		if st.ByDay[i].Day == wantKey {
			got = &st.ByDay[i]
		}
		sum += st.ByDay[i].ConsumedCent + st.ByDay[i].TopupCent
	}
	if got == nil {
		t.Fatalf("by_day 缺少窗口起始日 %s（桶: %+v）", wantKey, st.ByDay)
	}
	if sum == 0 {
		t.Fatalf("所有桶都是 0，说明真实数据被补零占位覆盖（键口径不一致）: %+v", st.ByDay)
	}
	if got.ConsumedCent != 20 || got.TopupCent != 10_000 {
		t.Fatalf("by_day[%s] = %+v, want 消耗 20 / 充值 10000", wantKey, *got)
	}
}
