package loadtest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// 0=rate rejected, 1=admitted, 2=duplicate. Test-only protocol.
func candidateAtomicAdmission(ctx context.Context, rdb *redis.Client, idem, rate, seq, owner string, now int64) (int64, error) {
	const script = `
 if redis.call('EXISTS', KEYS[1]) == 1 then return 2 end
 redis.call('ZREMRANGEBYSCORE', KEYS[2], 0, tonumber(ARGV[1])-1000)
 if redis.call('ZCARD', KEYS[2]) >= 1 then return 0 end
 redis.call('SET', KEYS[1], ARGV[2], 'PX', 300000)
 local seq = redis.call('INCR', KEYS[3])
 redis.call('PEXPIRE', KEYS[3], 1000)
 redis.call('ZADD', KEYS[2], ARGV[1], ARGV[1] .. ':' .. seq)
 redis.call('PEXPIRE', KEYS[2], 1000)
 return 1`
	return rdb.Eval(ctx, script, []string{idem, rate, seq}, now, owner).Int64()
}

func TestAtomicAdmissionConcurrency(t *testing.T) {
	testAtomicAdmissionConcurrency(t, candidateAtomicAdmission)
}

func testAtomicAdmissionConcurrency(t *testing.T, admit func(context.Context, *redis.Client, string, string, string, string, int64) (int64, error)) {
	t.Helper()
	var addr string
	if os.Getenv("TEST_ATOMIC_ADMISSION_REAL") == "1" {
		addr = os.Getenv("TEST_ATOMIC_ADMISSION_ADDR")
		if addr == "" {
			t.Fatal("missing isolated address")
		}
	} else {
		mr := miniredis.RunT(t)
		addr = mr.Addr()
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx := context.Background()
	prefix := fmt.Sprintf("atomic-admit-%d", time.Now().UnixNano())
	rate, seq := prefix+":rate", prefix+":seq"
	const n = 64
	type result struct {
		index int
		code  int64
		err   error
	}
	run := func(same bool) []result {
		start := make(chan struct{})
		out := make(chan result, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				key := fmt.Sprintf("%s:idem:%d", prefix, i)
				if same {
					key = prefix + ":same"
				}
				code, err := admit(ctx, rdb, key, rate, seq, fmt.Sprintf("owner-%d", i), time.Now().UnixMilli())
				out <- result{i, code, err}
			}(i)
		}
		close(start)
		wg.Wait()
		close(out)
		var all []result
		for x := range out {
			all = append(all, x)
		}
		return all
	}
	all := run(false)
	var wins, rejected int
	var winner, retry int
	for _, x := range all {
		if x.err != nil {
			t.Fatal(x.err)
		}
		switch x.code {
		case 1:
			wins++
			winner = x.index
		case 0:
			rejected++
			retry = x.index
		default:
			t.Fatalf("unexpected code %d", x.code)
		}
	}
	if wins != 1 || rejected != n-1 {
		t.Fatalf("wins=%d rejected=%d", wins, rejected)
	}
	for _, x := range all {
		key := fmt.Sprintf("%s:idem:%d", prefix, x.index)
		v, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		if (x.code == 1 && v != 1) || (x.code == 0 && v != 0) {
			t.Fatalf("index=%d code=%d exists=%d", x.index, x.code, v)
		}
	}
	dup, err := admit(ctx, rdb, fmt.Sprintf("%s:idem:%d", prefix, winner), rate, seq, "different-owner", time.Now().UnixMilli())
	if err != nil || dup != 2 {
		t.Fatalf("duplicate=%d err=%v", dup, err)
	}
	if err := rdb.Del(ctx, rate, seq).Err(); err != nil {
		t.Fatal(err)
	}
	code, err := admit(ctx, rdb, fmt.Sprintf("%s:idem:%d", prefix, retry), rate, seq, "retry", time.Now().UnixMilli())
	if err != nil || code != 1 {
		t.Fatalf("retry=%d err=%v", code, err)
	}
	if err := rdb.Del(ctx, rate, seq).Err(); err != nil {
		t.Fatal(err)
	}
	all = run(true)
	wins = 0
	dups := 0
	for _, x := range all {
		if x.err != nil {
			t.Fatal(x.err)
		}
		switch x.code {
		case 1:
			wins++
		case 2:
			dups++
		default:
			t.Fatalf("same-key code=%d", x.code)
		}
	}
	if wins != 1 || dups != n-1 {
		t.Fatalf("same key wins=%d duplicates=%d", wins, dups)
	}
	t.Logf("different_keys=64 admitted=1 rejected=63 no_rejected_keys=true duplicate_first=true retry_after_reset=true same_key_admitted=1 duplicate=63")
}

// Redis scripts are atomic against other clients but do not roll back commands
// already executed before a runtime error. This test characterizes that risk.
func TestAtomicAdmissionScriptErrorLeavesLease(t *testing.T) {
	if os.Getenv("TEST_ATOMIC_ADMISSION_REAL") != "1" {
		t.Skip("isolated real Redis only")
	}
	addr := os.Getenv("TEST_ATOMIC_ADMISSION_ADDR")
	if addr == "" {
		t.Fatal("missing isolated address")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx := context.Background()
	prefix := fmt.Sprintf("atomic-error-%d", time.Now().UnixNano())
	idem, rate, seq := prefix+":idem", prefix+":rate", prefix+":seq"
	if err := rdb.Set(ctx, seq, "not-an-integer", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	code, err := candidateAtomicAdmission(ctx, rdb, idem, rate, seq, "owner", time.Now().UnixMilli())
	if err == nil {
		t.Fatalf("expected INCR error code=%d", code)
	}
	exists, e := rdb.Exists(ctx, idem).Result()
	if e != nil || exists != 1 {
		t.Fatalf("lease=%d err=%v (script error=%v)", exists, e, err)
	}
	count, e := rdb.ZCard(ctx, rate).Result()
	if e != nil || count != 0 {
		t.Fatalf("rate=%d err=%v", count, e)
	}
	t.Logf("script_error=true lease_exists=%d rate_count=%d", exists, count)
}
