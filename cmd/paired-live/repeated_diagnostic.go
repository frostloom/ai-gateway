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

func claimRepeatedDiagnostic(root string, p taskbench.AuthorizedPilot, out string) error {
	if !p.Ready() || !p.IsRepeatedDiagnostic() || p.CampaignID() != taskbench.V5RepeatedDiagnosticID {
		return fmt.Errorf("ready fixed repeated export diagnostic required")
	}
	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	for _, prior := range []struct{ id, sha string }{
		{taskbench.RechargeConfirmID, taskbench.RechargePriorClaimSHA}, {taskbench.RechargeDiagnosticRetestID, taskbench.RechargeDiagnosticClaimSHA}, {taskbench.RechargeCancelID, taskbench.RechargeCancelClaimSHA}, {taskbench.RechargeSupplementID, taskbench.RechargeSupplementClaimSHA}, {taskbench.RefundCapID, taskbench.RefundCapClaimSHA}, {taskbench.V5SixCohortID, taskbench.V5SixCohortClaimSHA}, {taskbench.V5RepeatedID, taskbench.V5RepeatedClaimSHA},
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
	f, err := os.OpenFile(filepath.Join(dir, taskbench.V5RepeatedDiagnosticID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("retest already claimed or durable claim unavailable")
	}
	record := map[string]any{"schema": "paired-live-repeated-export-diagnostic-claim.v1", "campaign_id": taskbench.V5RepeatedDiagnosticID, "execution_revision": p.ExecutionRevision(), "output": out, "status": "consumed_before_execution", "budget_ceiling_sha256": taskbench.RechargeBudgetCeilingSHA, "reserved_model_calls": 248, "reserved_model_micro_cny": 8380416, "reserved_jev_calls": 24, "reserved_jev_micro_usd": 64512, "cumulative_reserved_model_micro_cny": 20680704, "cumulative_reserved_jev_micro_usd": 158592, "prior_confirm_claim_sha256": taskbench.RechargePriorClaimSHA, "prior_diagnostic_claim_sha256": taskbench.RechargeDiagnosticClaimSHA, "prior_cancel_claim_sha256": taskbench.RechargeCancelClaimSHA, "prior_supplement_claim_sha256": taskbench.RechargeSupplementClaimSHA, "prior_refund_claim_sha256": taskbench.RefundCapClaimSHA, "prior_cohort_claim_sha256": taskbench.V5SixCohortClaimSHA, "prior_repeated_claim_sha256": taskbench.V5RepeatedClaimSHA}
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
