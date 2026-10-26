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

func claimReceiverRepeated(root string, p taskbench.AuthorizedPilot, out string) error {
	if !p.Ready() || !p.IsReceiverRepeated() || p.CampaignID() != taskbench.V5ReceiverRepeatedID {
		return fmt.Errorf("ready fixed cohort required")
	}

	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	for _, prior := range []struct{ id, sha string }{
		{taskbench.RechargeConfirmID, taskbench.RechargePriorClaimSHA}, {taskbench.RechargeDiagnosticRetestID, taskbench.RechargeDiagnosticClaimSHA}, {taskbench.RechargeCancelID, taskbench.RechargeCancelClaimSHA}, {taskbench.RechargeSupplementID, taskbench.RechargeSupplementClaimSHA}, {taskbench.RefundCapID, taskbench.RefundCapClaimSHA}, {taskbench.V5SixCohortID, taskbench.V5SixCohortClaimSHA}, {taskbench.V5RepeatedID, taskbench.V5RepeatedClaimSHA}, {taskbench.V5RepeatedDiagnosticID, taskbench.V5ExportDiagnosticClaimSHA},
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
	f, err := os.OpenFile(filepath.Join(dir, taskbench.V5ReceiverRepeatedID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("retest already claimed or durable claim unavailable")
	}
	record := map[string]any{"schema": "paired-live-receiver-repeated-claim.v1", "campaign_id": taskbench.V5ReceiverRepeatedID, "execution_revision": p.ExecutionRevision(), "output": out, "status": "consumed_before_execution", "budget_ceiling_sha256": taskbench.RechargeBudgetCeilingSHA, "reserved_model_calls": 248, "reserved_model_micro_cny": 8380416, "reserved_jev_calls": 24, "reserved_jev_micro_usd": 64512, "cumulative_reserved_model_micro_cny": 29061120, "cumulative_reserved_jev_micro_usd": 223104, "prior_confirm_claim_sha256": taskbench.RechargePriorClaimSHA, "prior_diagnostic_claim_sha256": taskbench.RechargeDiagnosticClaimSHA, "prior_cancel_claim_sha256": taskbench.RechargeCancelClaimSHA, "prior_supplement_claim_sha256": taskbench.RechargeSupplementClaimSHA, "prior_refund_claim_sha256": taskbench.RefundCapClaimSHA, "prior_cohort_claim_sha256": taskbench.V5SixCohortClaimSHA, "prior_repeated_claim_sha256": taskbench.V5RepeatedClaimSHA, "repeated_diagnostic_binding": p.RepeatedDiagnosticBinding(), "receiver_repeated_binding": p.ReceiverRepeatedBinding(), "prior_export_diagnostic_claim_sha256": taskbench.V5ExportDiagnosticClaimSHA}

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
