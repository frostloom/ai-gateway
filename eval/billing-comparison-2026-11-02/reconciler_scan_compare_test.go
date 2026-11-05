package comparison

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

// Compare discovery only. No RPC, balance mutation, or production implementation.
func TestReconcilerScanDiscoveryComparison(t *testing.T) {
	if os.Getenv("TEST_RECON_SCAN_COMPARE") != "1" {
		t.Skip("isolated comparison only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Bill{}); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("scancompare-%d", time.Now().UnixNano())
	old := time.Now().Add(-time.Hour)
	reported := old.Add(time.Minute)
	const n = 20000
	rows := make([]store.Bill, n)
	for i := range rows {
		rows[i] = store.Bill{RequestID: fmt.Sprintf("%s-%d", prefix, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 1, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, CreatedAt: old}
		if i%1000 == 0 {
			rows[i].UsageReportedAt = &reported
		}
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("request_id LIKE ?", prefix+"-%").Delete(&store.Bill{})
	cutoff := time.Now().Add(-time.Minute)
	var upper uint64
	if err := db.Model(&store.Bill{}).Where("phase = ? AND status = ? AND created_at < ?", store.PhaseReserve, store.StatusPending, cutoff).Select("COALESCE(MAX(id),0)").Scan(&upper).Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 5; round++ {
		for _, filtered := range []bool{false, true} {
			start := time.Now()
			var after uint64
			var scanned, known, held int
			for after < upper {
				var page []store.Bill
				q := db.Model(&store.Bill{}).Where("phase = ? AND status = ? AND created_at < ? AND id > ? AND id <= ?", store.PhaseReserve, store.StatusPending, cutoff, after, upper)
				if filtered {
					q = q.Where("usage_reported_at IS NOT NULL")
				}
				if err := q.Select("id", "request_id", "usage_reported_at", "created_at").Order("id ASC").Limit(256).Find(&page).Error; err != nil {
					t.Fatal(err)
				}
				if len(page) == 0 {
					break
				}
				after = page[len(page)-1].ID
				scanned += len(page)
				for _, v := range page {
					if v.UsageReportedAt != nil {
						known++
					} else {
						held++
					}
				}
			}
			if filtered {
				var count int64
				if err := db.Model(&store.Bill{}).Where("phase = ? AND status = ? AND created_at < ? AND id <= ? AND usage_reported_at IS NULL", store.PhaseReserve, store.StatusPending, cutoff, upper).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				held = int(count)
			}
			if known != 20 || held != n-20 {
				t.Fatalf("filtered=%v known=%d held=%d", filtered, known, held)
			}
			t.Logf("round=%d filtered=%t discovered=%d known=%d held=%d elapsed_us=%d", round+1, filtered, scanned, known, held, time.Since(start).Microseconds())
		}
	}
}
