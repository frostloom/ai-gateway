package comparison

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// Tests SQL semantics in disposable MySQL; it does not test production router code.
func TestRouterZeroResetRowContention(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_router_reset (id BIGINT PRIMARY KEY, status INT NOT NULL, consecutive_fail INT NOT NULL) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_router_reset").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO eval_router_reset(id,status,consecutive_fail) VALUES(1,0,0),(2,0,3),(3,2,3)").Error; e != nil {
		t.Fatal(e)
	}
	for _, conditional := range []bool{false, true} {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		var x int
		if e := tx.Raw("SELECT consecutive_fail FROM eval_router_reset WHERE id=1 FOR UPDATE").Scan(&x).Error; e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		q := "UPDATE eval_router_reset SET consecutive_fail=0 WHERE id=1 AND status=0"
		if conditional {
			q += " AND consecutive_fail <> 0"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		start := time.Now()
		e := db.WithContext(ctx).Exec(q).Error
		elapsed := time.Since(start)
		cancel()
		tx.Rollback()
		if !errors.Is(e, context.DeadlineExceeded) {
			t.Fatalf("conditional=%v expected blocked query deadline, got %v elapsed=%s", conditional, e, elapsed)
		}
		t.Logf("conditional=%v blocked_on_zero_locked_row=true elapsed_ms=%d", conditional, elapsed.Milliseconds())
	}
	// The conditional form still clears positive active rows and leaves banned rows alone.
	if e := db.Exec("UPDATE eval_router_reset SET consecutive_fail=0 WHERE status=0 AND consecutive_fail <> 0").Error; e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		ID              int
		Status          int
		ConsecutiveFail int
	}
	if e := db.Raw("SELECT id,status,consecutive_fail FROM eval_router_reset ORDER BY id").Scan(&rows).Error; e != nil {
		t.Fatal(e)
	}
	if len(rows) != 3 || rows[0].ConsecutiveFail != 0 || rows[1].ConsecutiveFail != 0 || rows[2].ConsecutiveFail != 3 {
		t.Fatal(fmt.Sprint(rows))
	}
}
