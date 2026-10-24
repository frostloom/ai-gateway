package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func claimCohort(root string, p taskbench.AuthorizedPilot, out string) error {
	if !p.Ready() || !p.IsCohort() || p.CampaignID() != taskbench.V5SixCohortID {
		return fmt.Errorf("ready fixed cohort required")
	}

	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	for _, prior := range []struct{ id, sha string }{
		{taskbench.RechargeConfirmID, taskbench.RechargePriorClaimSHA}, {taskbench.RechargeDiagnosticRetestID, taskbench.RechargeDiagnosticClaimSHA}, {taskbench.RechargeCancelID, taskbench.RechargeCancelClaimSHA}, {taskbench.RechargeSupplementID, taskbench.RechargeSupplementClaimSHA}, {taskbench.RefundCapID, taskbench.RefundCapClaimSHA},
	} {
		raw, err := boundedFile(filepath.Join(dir, prior.id+".json"))
		if err != nil {
			return fmt.Errorf("exact consumed predecessor claims required")
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != prior.sha {
			return fmt.Errorf("predecessor claim changed")
		}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, taskbench.V5SixCohortID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("retest already claimed or durable claim unavailable")
	}
	record := map[string]any{"schema": "paired-live-cohort-claim.v1", "campaign_id": taskbench.V5SixCohortID, "execution_revision": p.ExecutionRevision(), "output": out, "status": "consumed_before_execution", "budget_ceiling_sha256": taskbench.RechargeBudgetCeilingSHA, "reserved_model_calls": 62, "reserved_model_micro_cny": 2095104, "reserved_jev_calls": 6, "reserved_jev_micro_usd": 16128, "cumulative_reserved_model_micro_cny": 3919872, "cumulative_reserved_jev_micro_usd": 29568, "prior_confirm_claim_sha256": taskbench.RechargePriorClaimSHA, "prior_diagnostic_claim_sha256": taskbench.RechargeDiagnosticClaimSHA, "prior_cancel_claim_sha256": taskbench.RechargeCancelClaimSHA, "prior_supplement_claim_sha256": taskbench.RechargeSupplementClaimSHA, "prior_refund_claim_sha256": taskbench.RefundCapClaimSHA}

	raw, _ := json.Marshal(record)
	_, w := f.Write(append(raw, '\n'))
	s := f.Sync()
	c := f.Close()
	if err := errors.Join(w, s, c); err != nil {
		return err
	}
	for _, parent := range []string{dir, filepath.Dir(dir), root} {
		d, err := os.Open(parent)
		if err != nil {
			return err
		}
		if err := errors.Join(d.Sync(), d.Close()); err != nil {
			return err
		}
	}
	return nil
}
