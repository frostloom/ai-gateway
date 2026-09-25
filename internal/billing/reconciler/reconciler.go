// Package reconciler 对账服务：把滞留的 pending 账单收敛掉（M4）。
//
// 背景：网关可能在任意时刻崩溃——预扣已扣但结算没落。这类账谁来收？
// 网关自己猜会误判（流中断的边界太模糊），所以统一由对账按一套规则收敛：
//
//	扫描条件：reserve 行 status=pending 且 created_at 早于「最大流时长 + 宽限期」
//	三分类：
//	  pending + 无 usage_reported_at（无 marker）→ 流从未产出完整结果 → Reverse 全退
//	  pending + 有 usage_reported_at（有 marker）→ 流已跑完但结算丢失 → 补 Settle
//	  其余（settled/reversed/未过期）→ 跳过
//
// 只读 MySQL 扫描，写操作一律走 billing 幂等 RPC（Reverse/Settle/RebuildBalance）。
// 这样写账的入口始终只有 billing 一家，对账只是「发现 + 触发」。
package reconciler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// Stats 一轮对账的结果（供日志/集成测试断言）。
type Stats struct {
	Scanned  int // 扫描到的待处理 pending 行
	Reversed int // 冲正成功（全退）
	Settled  int // 补结算成功
	Rebuilt  int // 重建余额的租户数
}

type Reconciler struct {
	db    *gorm.DB // 只读扫描（账本）
	cli   billingv1.BillingServiceClient
	log   *slog.Logger
	grace time.Duration // 宽限期：created_at 早于 now-grace 才处理

	// 最近一轮结果快照（HTTP /status 观测，mutex 保护）。
	lastMu sync.Mutex
	last   Stats
}

func New(db *gorm.DB, cli billingv1.BillingServiceClient, log *slog.Logger, grace time.Duration) *Reconciler {
	return &Reconciler{db: db, cli: cli, log: log, grace: grace}
}

// RecordStats 记录最近一轮结果（Run 循环调用）。
func (r *Reconciler) RecordStats(s Stats) {
	r.lastMu.Lock()
	r.last = s
	r.lastMu.Unlock()
}

// LastStats 返回最近一轮快照。
func (r *Reconciler) LastStats() Stats {
	r.lastMu.Lock()
	defer r.lastMu.Unlock()
	return r.last
}

// HTTPHandler 观测 HTTP：/healthz（存活）+ /status（最近一轮对账结果）。网关 /admin/health 聚合用。
func (r *Reconciler) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"reconciler"}`))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, req *http.Request) {
		s := r.LastStats()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "svc": "reconciler",
			"last_scanned": s.Scanned, "last_reversed": s.Reversed,
			"last_settled": s.Settled, "last_rebuilt": s.Rebuilt,
		})
	})
	return mux
}

// SweepOnce 跑一轮扫描 + 分类 + 收敛。每行的写操作都是 billing 幂等 RPC，
// 并发重复执行是安全的（重复 sweep 只是 no-op）。
func (r *Reconciler) SweepOnce(ctx context.Context) (Stats, error) {
	var stats Stats
	cutoff := time.Now().Add(-r.grace)
	var rows []store.Bill
	if err := r.db.WithContext(ctx).
		Where("phase = ? AND status = ? AND created_at < ?",
			store.PhaseReserve, store.StatusPending, cutoff).
		Find(&rows).Error; err != nil {
		return stats, err
	}

	stats.Scanned = len(rows)
	for i := range rows {
		row := &rows[i]
		rid := row.RequestID
		if row.UsageReportedAt == nil {
			// 无 marker：流从未产出完整结果 → 全退。Reverse 幂等，重复跑 no-op。
			resp, err := r.cli.Reverse(ctx, &billingv1.ReverseRequest{
				RequestId: rid, BillId: int64(row.ID), Reason: "recon:no_usage",
			})
			if err != nil {
				r.log.Error("recon reverse", "request_id", rid, "err", err)
				continue
			}
			stats.Reversed++
			r.log.Info("recon reversed", "request_id", rid, "refund_quota", resp.RefundQuota,
				"bill_id", row.ID, "created_at", row.CreatedAt.Format(time.RFC3339))
			continue
		}
		// 有 marker：流已跑完，只差结算 → 补 Settle（按记录的 actual，幂等）。
		resp, err := r.cli.Settle(ctx, &billingv1.SettleRequest{RequestId: rid, BillId: int64(row.ID)})
		if err != nil {
			r.log.Error("recon settle", "request_id", rid, "err", err)
			continue
		}
		if resp.Settled {
			stats.Settled++
			r.log.Info("recon settled", "request_id", rid, "delta_quota", resp.DeltaQuota,
				"bill_id", row.ID, "created_at", row.CreatedAt.Format(time.RFC3339))
		} else {
			r.log.Warn("recon settle skipped", "request_id", rid, "reason", "already finalized")
		}
	}
	return stats, nil
}

// RebuildAll 对每个活跃租户从账本重建 Redis 余额投影（对账的兜底保险）。
func (r *Reconciler) RebuildAll(ctx context.Context) (Stats, error) {
	var stats Stats
	var tenants []store.Tenant
	if err := r.db.WithContext(ctx).Where("status = ?", 0).Find(&tenants).Error; err != nil {
		return stats, err
	}
	for _, t := range tenants {
		resp, err := r.cli.RebuildBalance(ctx, &billingv1.RebuildBalanceRequest{TenantId: int64(t.ID)})
		if err != nil {
			r.log.Error("recon rebuild", "tenant_id", t.ID, "err", err)
			continue
		}
		stats.Rebuilt++
		r.log.Info("recon rebuilt balance", "tenant_id", t.ID, "balance", resp.Balance)
	}
	return stats, nil
}

// Run 按 interval 循环：每轮 sweep + rebuild，直到 ctx 取消。
// noRebuild=true 时只跑 sweep（压测期间用）：rebuild 是「重建投影」的维护操作，
// 与在途 Reserve/Settle 竞态会漏掉 pre 预占，只能在流量安静时跑。
func (r *Reconciler) Run(ctx context.Context, interval time.Duration, noRebuild bool) {
	for {
		var s Stats
		if ss, err := r.SweepOnce(ctx); err != nil {
			r.log.Error("sweep", "err", err)
		} else {
			s = ss
			if ss.Scanned > 0 {
				r.log.Info("sweep done", "scanned", ss.Scanned, "reversed", ss.Reversed, "settled", ss.Settled)
			}
		}
		if !noRebuild {
			if ss, err := r.RebuildAll(ctx); err != nil {
				r.log.Error("rebuild", "err", err)
			} else {
				s.Rebuilt = ss.Rebuilt
				if ss.Rebuilt > 0 {
					r.log.Info("rebuild done", "tenants", ss.Rebuilt)
				}
			}
		}
		r.RecordStats(s)

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
