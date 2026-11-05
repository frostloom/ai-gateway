package loadtest

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Isolated candidate for exploring ambiguous Redis execution; not production.
const (
	idemCanceled  int64 = -1
	idemDuplicate int64 = 0
	idemAcquired  int64 = 1
)

type idemCandidate struct {
	r                 *redis.Client
	markerTTL, keyTTL time.Duration
}

func (c *idemCandidate) keys(request, owner string) (string, string) {
	return "idem-exp:{" + request + "}", "idem-exp-cancel:{" + request + "}:" + owner
}
func (c *idemCandidate) acquire(ctx context.Context, request, owner string) (int64, error) {
	key, cancel := c.keys(request, owner)
	const script = `if redis.call('EXISTS',KEYS[2])==1 then return -1 end
if redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2],'NX') then return 1 end
return 0`
	return c.r.Eval(ctx, script, []string{key, cancel}, owner, c.keyTTL.Milliseconds()).Int64()
}
func (c *idemCandidate) cancel(ctx context.Context, request, owner string) error {
	key, marker := c.keys(request, owner)
	const script = `redis.call('PSETEX',KEYS[2],ARGV[2],'1')
if redis.call('GET',KEYS[1])==ARGV[1] then redis.call('DEL',KEYS[1]) end
return 1`
	return c.r.Eval(ctx, script, []string{key, marker}, owner, c.markerTTL.Milliseconds()).Err()
}
