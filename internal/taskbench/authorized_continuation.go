package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

// AuthorizedContinuation is a checked snapshot of trusted local evidence, not
// permission to spend. The CLI separately requires explicit continuation consent.
type AuthorizedContinuation struct {
	dir                   string
	receipt               AuthorizedFirstBatchReceipt
	receiptSHA, reviewSHA string
	review                []byte
}

func (c AuthorizedContinuation) PreviousModelCalls() int { return c.receipt.ModelState.Calls }
func (c AuthorizedContinuation) PreviousJEVCalls() int   { return c.receipt.JEVAttempts }
func (c AuthorizedContinuation) ReceiptSHA() string      { return c.receiptSHA }
func (c AuthorizedContinuation) ReviewSHA() string       { return c.reviewSHA }

func readContinuationFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil || len(raw) > 16<<20 {
		return nil, fmt.Errorf("bounded checkpoint evidence required")
	}
	return raw, nil
}

func CheckAuthorizedContinuation(p AuthorizedPilot, dir string, reviewRaw []byte) (AuthorizedContinuation, error) {
	fail := func() (AuthorizedContinuation, error) {
		return AuthorizedContinuation{}, fmt.Errorf("complete bound first-batch checkpoint and passing review required")
	}
	if !p.valid || p.IsRetest() || dir == "" || len(reviewRaw) > 1<<20 {
		return fail()
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fail()
	}
	raw, err := readContinuationFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		return fail()
	}
	var r AuthorizedFirstBatchReceipt
	if json.Unmarshal(raw, &r) != nil || r.Schema != "authorized-first-batch.v1" || r.Status != "checkpoint_pending" || r.FailedStage != "" || r.AuthorizationSHA != p.authorizationSHA || r.PlanSHA != p.planSHA || r.BillingSHA != p.billingSHA || r.EvidenceSHA != p.evidenceSHA || r.JEVFailurePolicy != p.JEVFailurePolicy() || r.VendorFailureChargesVerified != p.JEVFailureChargesVerified() || r.Report == nil || len(r.Batches) != 6 || r.ModelState.Stopped != "" || r.ModelState.CostMicroCNY == nil || r.ModelState.Calls < 1 || r.ModelState.Calls > 3 || r.JEVAttempts != 1 {
		return fail()
	}
	for i, b := range r.Batches {
		want := "not_started"
		if i == 0 {
			want = "checkpoint_pending"
		}
		if b.ID != fmt.Sprintf("batch-%03d", i+1) || b.Key != p.plan.Batches[i].Key || b.Status != want {
			return fail()
		}
	}
	outcome, err := readContinuationFile(filepath.Join(dir, "entry-outcome.json"))
	if err != nil {
		return fail()
	}
	var o struct {
		Schema  string `json:"schema"`
		Status  string `json:"status"`
		Receipt string `json:"execution_receipt"`
		Success bool   `json:"cleanup_and_receipt_succeeded"`
	}
	if json.Unmarshal(outcome, &o) != nil || o.Schema != "paired-live-entry-outcome.v1" || o.Status != "checkpoint_pending" || o.Receipt != "receipt.json" || !o.Success {
		return fail()
	}
	report, err := ReportPreparedPair(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
	if err != nil || !reflect.DeepEqual(report, *r.Report) || report.PlannedArms != 2 || report.FinishedArms != 2 || len(report.Rows) != 2 {
		return fail()
	}
	var review struct {
		Schema   string            `json:"schema"`
		Revision string            `json:"execution_revision"`
		Journal  string            `json:"journal_sha256"`
		Findings []json.RawMessage `json:"blocking_findings"`
		Rows     []struct {
			Arm          string `json:"arm"`
			Key          string `json:"case_key"`
			Verdict      string `json:"verdict"`
			Requirements []struct {
				ConversationRequirement
				Verdict string `json:"verdict"`
			} `json:"requirements"`
		} `json:"rows"`
	}
	if json.Unmarshal(reviewRaw, &review) != nil || review.Schema != "pi-first-live-independent-review.v1" || review.Revision != r.Revision || review.Journal != report.JournalSHA256 || review.Findings == nil || len(review.Findings) != 0 || len(review.Rows) != 2 {
		return fail()
	}
	seen := map[string]bool{}
	calls := 0
	var cost, reserved int64
	for _, row := range report.Rows {
		if seen[row.Arm] || (row.Arm != "model" && row.Arm != "jev_selective") || row.Key != p.plan.Batches[0].Key || !row.StructuralPass || row.FactErrors != 0 || row.HTTPFailures != 0 || row.ModelAttempts == nil || row.PricedModelMicroCNY == nil || row.ReservedModelMicroCNY == nil || len(row.ReviewRequirements) == 0 {
			return fail()
		}
		seen[row.Arm] = true
		calls += *row.ModelAttempts
		cost += *row.PricedModelMicroCNY
		reserved += *row.ReservedModelMicroCNY
		matches := 0
		for _, rr := range review.Rows {
			if rr.Arm != row.Arm {
				continue
			}
			matches++
			if rr.Key != row.Key || rr.Verdict != "passed" || len(rr.Requirements) != len(row.ReviewRequirements) {
				return fail()
			}
			qs := map[string]bool{}
			for _, q := range rr.Requirements {
				if qs[q.ID] || q.Verdict != "passed" {
					return fail()
				}
				qs[q.ID] = true
				found := false
				for _, want := range row.ReviewRequirements {
					if q.ConversationRequirement == want {
						found = true
					}
				}
				if !found {
					return fail()
				}
			}
		}
		if matches != 1 {
			return fail()
		}
	}
	if calls != r.ModelState.Calls || cost != *r.ModelState.CostMicroCNY || reserved != r.ModelState.ReservedMicroCNY {
		return fail()
	}
	// Check the original permanent reservation and successful completion without
	// opening/creating a ledger. Exactly one batch must exist at this checkpoint.
	ledger, err := readContinuationFile(filepath.Join(dir, "campaign", "campaign.jsonl"))
	if err != nil {
		return fail()
	}
	lines := bytes.Split(bytes.TrimSpace(ledger), []byte("\n"))
	if len(lines) != 3 {
		return fail()
	}
	binding, _ := json.Marshal([]string{r.AuthorizationSHA, r.PlanSHA, r.BillingSHA, r.EvidenceSHA, r.Revision})
	var config, reserve, finish campaignEvent
	if json.Unmarshal(lines[0], &config) != nil || json.Unmarshal(lines[1], &reserve) != nil || json.Unmarshal(lines[2], &finish) != nil || config.Kind != "config" || config.Contract != conversationHash(binding) || config.Limits == nil || *config.Limits != p.plan.Limits || reserve.Kind != "reserve" || reserve.ID != "batch-001" || reserve.ModelCalls != 3 || reserve.ModelMicroCNY != 101376 || reserve.JEVCalls != 1 || reserve.JEVMicroUSD != 2688 || finish.Kind != "finish" || finish.ID != "batch-001" || finish.Failed {
		return fail()
	}
	markerRaw, err := readContinuationFile(filepath.Join(dir, "campaign", "batch-001", "reservation.json"))
	var marker campaignEvent
	if err != nil || json.Unmarshal(markerRaw, &marker) != nil || !reflect.DeepEqual(marker, reserve) {
		return fail()
	}
	entries, err := os.ReadDir(filepath.Join(dir, "campaign"))
	if err != nil {
		return fail()
	}
	for _, entry := range entries {
		if (entry.Name() == "batch-001" && entry.IsDir()) || (entry.Name() == "campaign.jsonl" && !entry.IsDir()) {
			continue
		}
		return fail()
	}
	journal, err := loadPairJournal(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
	if err != nil || reserve.Binding != PairRunContract(journal.cfg) || validateContinuationAccounting(filepath.Join(dir, "campaign"), r, reserve.Binding, p.plan.Batches[0].ModelLimits) != nil {
		return fail()
	}
	return AuthorizedContinuation{dir: dir, receipt: r, receiptSHA: conversationHash(raw), reviewSHA: conversationHash(reviewRaw), review: append([]byte(nil), reviewRaw...)}, nil
}

func RunAuthorizedSecondBatch(ctx context.Context, dir string, o AuthorizedFirstBatchOptions, c AuthorizedContinuation) (AuthorizedFirstBatchReceipt, error) {
	checked, err := CheckAuthorizedContinuation(o.Pilot, c.dir, c.review)
	if err != nil || checked.receiptSHA != c.receiptSHA || checked.reviewSHA != c.reviewSHA {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("checkpoint changed or unavailable")
	}
	return runAuthorizedBatch(ctx, dir, o, &checked)
}

// Replay accounting without opening writers or creating lock files. Successful
// first-batch evidence must contain only complete, settled, bounded attempts.
func validateContinuationAccounting(dir string, r AuthorizedFirstBatchReceipt, binding string, limits PairBudgetLimits) error {
	fail := func() error { return fmt.Errorf("original accounting evidence incomplete or changed") }
	for _, path := range []string{filepath.Join(dir, "campaign.lock"), filepath.Join(dir, "batch-001", "budget.jsonl.lock"), filepath.Join(dir, "batch-001", "run.jsonl.jev.jsonl.lock")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fail()
		}
	}
	raw, err := readContinuationFile(filepath.Join(dir, "batch-001", "budget.jsonl"))
	if err != nil {
		return fail()
	}
	zero := int64(0)
	b := PairBudgetTransport{limits: limits, state: PairBudgetState{CostMicroCNY: &zero}}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) < 3 {
		return fail()
	}
	for i, line := range lines {
		var ev pairBudgetEvent
		if json.Unmarshal(line, &ev) != nil {
			return fail()
		}
		if i == 0 {
			if ev.Kind != "config" || ev.Contract != binding || ev.Limits == nil || *ev.Limits != limits {
				return fail()
			}
		} else if b.apply(ev) != nil {
			return fail()
		}
	}
	if b.inflight || b.state.Stopped != "" || b.state.CostMicroCNY == nil || b.state.Calls != r.ModelState.Calls || b.state.ReservedMicroCNY != r.ModelState.ReservedMicroCNY || *b.state.CostMicroCNY != *r.ModelState.CostMicroCNY || b.state.InputTokens != r.ModelState.InputTokens || b.state.OutputTokens != r.ModelState.OutputTokens {
		return fail()
	}
	raw, err = readContinuationFile(filepath.Join(dir, "batch-001", "run.jsonl.jev.jsonl"))
	if err != nil {
		return fail()
	}
	lines = bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 3 {
		return fail()
	}
	for i, line := range lines {
		var ev pairJEVEvent
		if json.Unmarshal(line, &ev) != nil {
			return fail()
		}
		switch i {
		case 0:
			if ev.Kind != "config" || ev.Contract != binding || ev.Version != AuthorizedPilotJEVModel || ev.Limit != 1 {
				return fail()
			}
		case 1:
			if ev.Kind != "reserve" || ev.ID != 1 {
				return fail()
			}
		case 2:
			if ev.Kind != "settle" || ev.ID != 1 || ev.Failed || ev.Failure != nil {
				return fail()
			}
		}
	}
	return nil
}
