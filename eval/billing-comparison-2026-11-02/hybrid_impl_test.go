package comparison

// Experimental bounded-staleness cache; not production-ready.
import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
)

type hybrid struct {
	candidate
	redis *redis.Client
	ttl   time.Duration
}

func (h hybrid) keys(id uint64) (string, string) {
	return fmt.Sprintf("experiment:hybrid:{%d}:generation", id), fmt.Sprintf("experiment:hybrid:{%d}:balance", id)
}
func (h hybrid) generation(ctx context.Context, id uint64) (string, error) {
	g, _ := h.keys(id)
	v, e := h.redis.Get(ctx, g).Result()
	if errors.Is(e, redis.Nil) {
		return "0", nil
	}
	return v, e
}

var invalidateScript = redis.NewScript(`redis.call('INCR',KEYS[1]); redis.call('DEL',KEYS[2]); return 1`)
var fillScript = redis.NewScript(`local g=redis.call('GET',KEYS[1]) or '0'; if g ~= ARGV[1] then return 0 end; redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[3]); return 1`)

func (h hybrid) invalidate(ctx context.Context, id uint64) error {
	g, k := h.keys(id)
	return invalidateScript.Run(ctx, h.redis, []string{g, k}).Err()
}
func (h hybrid) fill(ctx context.Context, id uint64, g string, value int64) (bool, error) {
	key, b := h.keys(id)
	n, e := fillScript.Run(ctx, h.redis, []string{key, b}, g, value, h.ttl.Milliseconds()).Int64()
	return n == 1, e
}
func (h hybrid) read(ctx context.Context, id uint64) (int64, error) {
	_, k := h.keys(id)
	for i := 0; i < 3; i++ {
		v, e := h.redis.Get(ctx, k).Result()
		if e == nil {
			return strconv.ParseInt(v, 10, 64)
		}
		if !errors.Is(e, redis.Nil) {
			return h.candidate.balance(ctx, id)
		}
		g, e := h.generation(ctx, id)
		if e != nil {
			return h.candidate.balance(ctx, id)
		}
		value, e := h.candidate.balance(ctx, id)
		if e != nil {
			return 0, e
		}
		ok, e := h.fill(ctx, id, g, value)
		if e != nil {
			return value, nil
		}
		if ok {
			return value, nil
		}
	}
	return h.candidate.balance(ctx, id)
}
func (h hybrid) reserve(ctx context.Context, r *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	result, e := h.candidate.reserve(ctx, r)
	if e != nil {
		return nil, e
	}
	if e = h.invalidate(ctx, uint64(r.TenantId)); e != nil {
		return nil, fmt.Errorf("reserve may have committed; cache invalidation: %w", e)
	}
	return result, nil
}
func (h hybrid) settle(ctx context.Context, id string, tenant uint64) (bool, error) {
	result, e := h.candidate.settle(ctx, id)
	if e != nil {
		return false, e
	}
	if e = h.invalidate(ctx, tenant); e != nil {
		return false, fmt.Errorf("settlement may have committed; cache invalidation: %w", e)
	}
	return result, nil
}
