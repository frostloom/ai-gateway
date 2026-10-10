package store

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestReserveLuaReplayTransfersCompensationOwner(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := NewStore(nil, rdb)
	ctx := context.Background()
	mr.Set(BalanceKey(1), "1000")
	keys := []string{BalanceKey(1), ReqGuardKey("request")}
	script := goredis.NewScript(reserveLua)
	for _, tc := range []struct {
		owner string
		code  int64
	}{{"attempt-A", 1}, {"attempt-B", 2}} {
		res, err := script.Run(ctx, rdb, keys, 100, tc.owner).Result()
		if err != nil {
			t.Fatal(err)
		}
		code, bal := parseReserveResult(res)
		if code != tc.code || bal != 900 {
			t.Fatalf("owner=%s code=%d bal=%d wantcode%d bal900", tc.owner, code, bal, tc.code)
		}
	}
	if refunded, err := s.compensateReserve(ctx, 1, "request", 100, "attempt-A"); err != nil || refunded {
		t.Fatalf("stale owner refund=%v err=%v", refunded, err)
	}
	if bal, _ := s.getBalance(ctx, 1); bal != 900 {
		t.Fatalf("stale compensation balance=%d want900", bal)
	}
	if refunded, err := s.compensateReserve(ctx, 1, "request", 100, "attempt-B"); err != nil || !refunded {
		t.Fatalf("current owner refund=%v err=%v", refunded, err)
	}
	if bal, _ := s.getBalance(ctx, 1); bal != 1000 {
		t.Fatalf("current compensation balance=%d want1000", bal)
	}
}

func TestReserveLuaAdoptsLegacyGuardWithoutSecondDebit(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := NewStore(nil, rdb)
	ctx := context.Background()
	mr.Set(BalanceKey(1), "900")
	mr.Set(ReqGuardKey("legacy-request"), "legacy-request")
	res, err := goredis.NewScript(reserveLua).Run(ctx, rdb, []string{BalanceKey(1), ReqGuardKey("legacy-request")}, 100, "retry-owner").Result()
	if err != nil {
		t.Fatal(err)
	}
	code, bal := parseReserveResult(res)
	if code != 2 || bal != 900 {
		t.Fatalf("legacy replay code=%d bal=%d want2,900", code, bal)
	}
	if refunded, err := s.compensateReserve(ctx, 1, "legacy-request", 100, "legacy-request"); err != nil || refunded {
		t.Fatalf("legacy stale refund=%v err=%v", refunded, err)
	}
	if refunded, err := s.compensateReserve(ctx, 1, "legacy-request", 100, "retry-owner"); err != nil || !refunded {
		t.Fatalf("new owner refund=%v err=%v", refunded, err)
	}
	if bal, _ := s.getBalance(ctx, 1); bal != 1000 {
		t.Fatalf("legacy compensation balance=%d want1000", bal)
	}
}
