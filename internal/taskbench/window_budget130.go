package taskbench

import (
	"fmt"
	"reflect"
)

func expectedCurrent130WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV5WindowBaseline()
	b.TotalModel = 130000000
	b.RemainingModel = 87038570
	return b
}

// This wrapper preserves the historical 105 parent and projects a separate
// current budget only after re-reading the exact current approval and proof.
type windowCurrent130History struct {
	stopped  windowStoppedV5Baseline
	approval windowActualFile
}

func (h windowCurrent130History) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if h.approval.SHA256 != windowBudget130ApprovalSHA {
		return fullWindowBaseline{}, "", "", fmt.Errorf("current approval binding changed")
	}
	if _, err := readWindowActualFile(h.approval.Path, h.approval.SHA256, 65536); err != nil {
		return fullWindowBaseline{}, "", "", fmt.Errorf("current approval changed")
	}
	base, sha, root, err := h.stopped.revalidateWindowStoppedHistory()
	if err != nil || sha != windowV6BaselineSHA || !reflect.DeepEqual(base, expectedStoppedV5WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("historical proof changed")
	}
	if _, err := readWindowActualFile(h.approval.Path, h.approval.SHA256, 65536); err != nil {
		return fullWindowBaseline{}, "", "", fmt.Errorf("current approval changed")
	}
	base.ClaimSHA256 = cloneWindowHistoricalClaims(base.ClaimSHA256)
	base.TotalModel = 130000000
	base.RemainingModel = 87038570
	return base, sha, root, nil
}
func (h windowCurrent130History) windowStoppedPredecessorRootsV6() [5]string {
	return h.stopped.windowStoppedPredecessorRootsV6()
}
