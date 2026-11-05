package comparison

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type failSecondEval struct{ count atomic.Int32 }

func (*failSecondEval) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*failSecondEval) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *failSecondEval) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "eval" && h.count.Add(1) == 2 {
			return errors.New("injected post-commit failure")
		}
		return next(ctx, cmd)
	}
}

func TestGuardedPostCommitFailureIsAmbiguous(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	hook := &failSecondEval{}
	r.AddHook(hook)
	c := guardedPostCommitCache{guardedAgeCache{r: r, ttl: time.Second, fenceTTL: time.Second}}
	committed := false
	err := c.mutate(context.Background(), "test", func() error { committed = true; return nil })
	if err == nil || !committed || hook.count.Load() != 2 {
		t.Fatalf("error=%v committed=%v calls=%d", err, committed, hook.count.Load())
	}
	t.Log("write callback completed before post-commit invalidation error; caller cannot treat error as no write")
}
