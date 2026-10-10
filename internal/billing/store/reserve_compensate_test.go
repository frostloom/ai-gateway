package store

import (
	"context"
	"errors"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
)

// Lua 扣减成功后插入失败：余额回到原值，guard 被删掉，同一 request 可以重新预扣。
func TestReserveInsertFailureCompensatesRedis(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-compensate", tenantID, 84) // pre=100

	if err := s.db.Callback().Create().Before("gorm:create").Register("test:insert_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" {
			tx.AddError(errors.New("injected insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:insert_failure") })

	if _, err := s.Reserve(ctx, req); err == nil {
		t.Fatal("injected insert failure was ignored")
	}
	if n := countReserveRows(s); n != 0 {
		t.Fatalf("reserve rows = %d, want 0", n)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 1000 {
		t.Fatalf("balance = %d, want 1000 after compensate", bal)
	}
	if n, err := s.rdb.Exists(ctx, ReqGuardKey(req.RequestId)).Result(); err != nil || n != 0 {
		t.Fatalf("guard exists = %d err=%v, want deleted", n, err)
	}

	s.db.Callback().Create().Remove("test:insert_failure")
	resp, err := s.Reserve(ctx, req)
	if err != nil || resp.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("retry reserve: code=%v err=%v", resp.GetCode(), err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 900 {
		t.Fatalf("balance = %d, want 900 after retry", bal)
	}
}

// guard 值不属于本次调用 owner 时，补偿不能改余额。
func TestCompensateReserveIgnoresMismatchedGuard(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	tenantID := seedTenant(t, s, 50)
	if err := s.rdb.Set(ctx, ReqGuardKey("rid-other"), "someone-else", 0).Err(); err != nil {
		t.Fatal(err)
	}
	ok, err := s.compensateReserve(ctx, tenantID, "rid-other", 10, "attempt-other")
	if err != nil || ok {
		t.Fatalf("compensate = %v err=%v, want false", ok, err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 50 {
		t.Fatalf("balance = %d, want 50", bal)
	}

	if err := s.rdb.Set(ctx, ReqGuardKey("rid-mine"), "attempt-mine", 0).Err(); err != nil {
		t.Fatal(err)
	}
	ok, err = s.compensateReserve(ctx, tenantID, "rid-mine", 10, "attempt-mine")
	if err != nil || !ok {
		t.Fatalf("compensate matching guard = %v err=%v", ok, err)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 60 {
		t.Fatalf("balance = %d, want 60", bal)
	}
}
