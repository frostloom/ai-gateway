package comparison

import "context"

// Test-only: failure after write may mean the authoritative mutation committed.
type guardedPostCommitCache struct{ guardedAgeCache }

func (c *guardedPostCommitCache) mutate(ctx context.Context, hash string, write func() error) error {
	if err := c.guardedAgeCache.mutate(ctx, hash, write); err != nil {
		return err
	}
	return c.guardedAgeCache.mutate(ctx, hash, func() error { return nil })
}
