package taskbench

import (
	"context"
	"fmt"
)

func RunAuthorizedRepeated(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if !o.Pilot.Ready() || !o.Pilot.IsRepeated() || o.Pilot.CampaignID() != V5RepeatedID || o.Revision != o.Pilot.ExecutionRevision() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("ready fixed repeated scope and exact revision required")
	}
	return runAuthorizedFixedCohort(ctx, dir, o)
}
