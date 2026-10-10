package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestDeltaLegacySentinelReplay(t *testing.T) {
	for _, delta := range []int64{50, -50, 0} {
		t.Run(time.Duration(delta).String(), func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			s := NewStore(nil, rdb)
			ctx := context.Background()
			mr.Set(BalanceKey(1), "850")
			mr.Set(DeltaGuardKey("legacy"), "1")
			bal, applied, err := s.applyDelta(ctx, 1, "legacy", delta)
			if err != nil || bal != 850 || applied != delta {
				t.Fatalf("bal=%d applied=%d err=%v want unchanged850 applied%d", bal, applied, err, delta)
			}
		})
	}
}

func TestDeltaVersionedOneReplay(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := NewStore(nil, rdb)
	ctx := context.Background()
	mr.Set(BalanceKey(1), "1")
	if bal, applied, err := s.applyDelta(ctx, 1, "one", 50); err != nil || bal != 0 || applied != 1 {
		t.Fatalf("first bal=%d applied=%d err=%v", bal, applied, err)
	}
	if bal, applied, err := s.applyDelta(ctx, 1, "one", 50); err != nil || bal != 0 || applied != 1 {
		t.Fatalf("replay bal=%d applied=%d err=%v", bal, applied, err)
	}
}

func TestDeltaLegacyNumericRefundPreserved(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := NewStore(nil, rdb)
	ctx := context.Background()
	mr.Set(BalanceKey(1), "1000")
	mr.Set(DeltaGuardKey("reversed"), "-100")
	bal, applied, err := s.applyDelta(ctx, 1, "reversed", 50)
	if err != nil || bal != 1000 || applied != -100 {
		t.Fatalf("bal=%d applied=%d err=%v; old actual refund must survive conflict", bal, applied, err)
	}
}
