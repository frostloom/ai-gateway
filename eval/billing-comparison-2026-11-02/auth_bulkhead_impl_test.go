package comparison

import (
	"context"
	"errors"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// Test-only local per-process bulkhead; no cross-instance limit or queuing.
var errAuthBusy = errors.New("authentication fallback overloaded")

type authBulkhead struct {
	permits  chan struct{}
	queued   chan struct{}
	wait     time.Duration
	upstream func(context.Context, string) (*store.APIKey, error)
}

func newAuthBulkhead(n int, fn func(context.Context, string) (*store.APIKey, error)) *authBulkhead {
	if n <= 0 {
		panic("auth bulkhead must be positive")
	}
	return &authBulkhead{permits: make(chan struct{}, n), upstream: fn}
}
func newWaitingAuthBulkhead(n int, wait time.Duration, fn func(context.Context, string) (*store.APIKey, error)) *authBulkhead {
	b := newAuthBulkhead(n, fn)
	b.wait = wait
	return b
}
func newBoundedAuthBulkhead(n, queue int, wait time.Duration, fn func(context.Context, string) (*store.APIKey, error)) *authBulkhead {
	if queue <= 0 || wait <= 0 {
		panic("queue and wait must be positive")
	}
	b := newWaitingAuthBulkhead(n, wait, fn)
	b.queued = make(chan struct{}, queue)
	return b
}
func (b *authBulkhead) validate(ctx context.Context, hash string) (*store.APIKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.wait <= 0 {
		select {
		case b.permits <- struct{}{}:
		default:
			return nil, errAuthBusy
		}
	} else {
		acquired := false
		queued := false
		defer func() {
			if queued {
				<-b.queued
			}
		}()
		if b.queued != nil {
			select {
			case b.permits <- struct{}{}:
				acquired = true
			default:
			}
			if !acquired {
				select {
				case b.queued <- struct{}{}:
					queued = true
				default:
					return nil, errAuthBusy
				}
			}
		}
		if !acquired {
			timer := time.NewTimer(b.wait)
			defer timer.Stop()
			select {
			case b.permits <- struct{}{}:
				if queued {
					<-b.queued
					queued = false
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return nil, errAuthBusy
			}
		}
	}
	defer func() { <-b.permits }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.upstream(ctx, hash)
}
