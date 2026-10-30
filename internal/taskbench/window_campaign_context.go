package taskbench

import (
	"context"
	"fmt"
)

// Call only while owning campaign.mu, or before the campaign is published.
// A wider caller context cannot erase the operation bound when opening a ledger.
func (c *windowCampaign) checkWindowCampaignContextLocked(ctx context.Context) error {
	if err := windowCampaignSuppliedContext(ctx); err != nil {
		return err
	}
	if c.acquisitionContext != nil {
		return c.acquisitionContext.Err()
	}
	return nil
}
func windowCampaignSuppliedContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("window operation context required")
	}
	return ctx.Err()
}
