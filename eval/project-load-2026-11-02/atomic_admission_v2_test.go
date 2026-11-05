package loadtest

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func candidateAtomicAdmissionV2(ctx context.Context, rdb *redis.Client, idem, rate, seq, owner string, now int64) (int64, error) {
	const script = `
 if redis.call('EXISTS', KEYS[1]) == 1 then return 2 end
 redis.call('ZREMRANGEBYSCORE', KEYS[2], 0, tonumber(ARGV[1])-1000)
 if redis.call('ZCARD', KEYS[2]) >= 1 then return 0 end
 local seq = redis.call('INCR', KEYS[3])
 redis.call('PEXPIRE', KEYS[3], 1000)
 redis.call('ZADD', KEYS[2], ARGV[1], ARGV[1] .. ':' .. seq)
 redis.call('PEXPIRE', KEYS[2], 1000)
 redis.call('SET', KEYS[1], ARGV[2], 'PX', 300000)
 return 1`
	return rdb.Eval(ctx, script, []string{idem, rate, seq}, now, owner).Int64()
}

func TestAtomicAdmissionV2Concurrency(t *testing.T) {
	testAtomicAdmissionConcurrency(t, candidateAtomicAdmissionV2)
}

// Simulate losing a successful reply at the application boundary; this does not
// inject a real network fault. The handler has not run when the reply is lost.
func TestAtomicAdmissionV2LostReply(t *testing.T) {
	if os.Getenv("TEST_ATOMIC_ADMISSION_REAL") != "1" {
		t.Skip("isolated real Redis only")
	}
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("TEST_ATOMIC_ADMISSION_ADDR")})
	defer rdb.Close()
	ctx := context.Background()
	p := fmt.Sprintf("atomic-lost-reply-%d", time.Now().UnixNano())
	idem, rate, seq := p+":idem", p+":rate", p+":seq"
	code, err := candidateAtomicAdmissionV2(ctx, rdb, idem, rate, seq, "owner", time.Now().UnixMilli())
	if err != nil || code != 1 {
		t.Fatalf("initial admission=%d err=%v", code, err)
	}
	// Discard the successful reply and simulate the caller returning an error
	// before starting any business handler.
	lease, err := rdb.Get(ctx, idem).Result()
	if err != nil || lease != "owner" {
		t.Fatalf("lease=%q err=%v", lease, err)
	}
	count, err := rdb.ZCard(ctx, rate).Result()
	if err != nil || count != 1 {
		t.Fatalf("rate=%d err=%v", count, err)
	}
	code, err = candidateAtomicAdmissionV2(ctx, rdb, idem, rate, seq, "retry-owner", time.Now().UnixMilli())
	if err != nil || code != 2 {
		t.Fatalf("retry=%d err=%v", code, err)
	}
	t.Logf("reply_discarded=true handler_started=false lease=%s rate_count=%d retry=duplicate", lease, count)
}
func TestAtomicAdmissionV2WrongSequence(t *testing.T) {
	if os.Getenv("TEST_ATOMIC_ADMISSION_REAL") != "1" {
		t.Skip("isolated real Redis only")
	}
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("TEST_ATOMIC_ADMISSION_ADDR")})
	defer rdb.Close()
	ctx := context.Background()
	p := fmt.Sprintf("atomic-v2-wrong-%d", time.Now().UnixNano())
	idem, rate, seq := p+":idem", p+":rate", p+":seq"
	if err := rdb.Set(ctx, seq, "not-an-integer", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := candidateAtomicAdmissionV2(ctx, rdb, idem, rate, seq, "owner", time.Now().UnixMilli()); err == nil {
		t.Fatal("expected wrong sequence error")
	}
	exists, err := rdb.Exists(ctx, idem).Result()
	if err != nil || exists != 0 {
		t.Fatalf("failed admission left lease=%d err=%v", exists, err)
	}
}

func TestAtomicAdmissionV2SetDeniedLeavesRateCharge(t *testing.T) {
	if os.Getenv("TEST_ATOMIC_ADMISSION_REAL") != "1" {
		t.Skip("isolated real Redis only")
	}
	addr := os.Getenv("TEST_ATOMIC_ADMISSION_ADDR")
	root := redis.NewClient(&redis.Options{Addr: addr})
	defer root.Close()
	ctx := context.Background()
	name := fmt.Sprintf("evaluser%d", time.Now().UnixNano())
	password := "eval-password"
	if err := root.Do(ctx, "ACL", "SETUSER", name, "on", ">"+password, "~*", "+@all", "-set").Err(); err != nil {
		t.Fatal(err)
	}
	defer root.Do(context.Background(), "ACL", "DELUSER", name)
	restricted := redis.NewClient(&redis.Options{Addr: addr, Username: name, Password: password})
	defer restricted.Close()
	p := fmt.Sprintf("atomic-acl-%d", time.Now().UnixNano())
	idem, rate, seq := p+":idem", p+":rate", p+":seq"
	_, err := candidateAtomicAdmissionV2(ctx, restricted, idem, rate, seq, "owner", time.Now().UnixMilli())
	if err == nil {
		t.Fatal("expected SET ACL failure")
	}
	lease, e := root.Exists(ctx, idem).Result()
	if e != nil {
		t.Fatal(e)
	}
	count, e := root.ZCard(ctx, rate).Result()
	if e != nil {
		t.Fatal(e)
	}
	if lease != 0 || count != 1 {
		t.Fatalf("lease=%d rate=%d error=%v", lease, count, err)
	}
	code, retryErr := candidateAtomicAdmissionV2(ctx, root, idem, rate, seq, "owner", time.Now().UnixMilli())
	if retryErr != nil || code != 0 {
		t.Fatalf("retry before window reset=%d err=%v", code, retryErr)
	}
	if err := root.Del(ctx, rate, seq).Err(); err != nil {
		t.Fatal(err)
	}
	code, retryErr = candidateAtomicAdmissionV2(ctx, root, idem, rate, seq, "owner", time.Now().UnixMilli())
	if retryErr != nil || code != 1 {
		t.Fatalf("retry after reset=%d err=%v", code, retryErr)
	}
	t.Logf("set_acl_error=true lease=%d rate_count=%d retry_before_reset=0 retry_after_reset=1", lease, count)
}
