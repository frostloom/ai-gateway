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

func claimRetest(root string, p taskbench.AuthorizedPilot, out string) error {
	if !p.IsRetest() || !p.Ready() || p.CampaignID() == "" {
		return fmt.Errorf("ready independent retest scope required")
	}
	return persistRetestClaim(root, p.CampaignID(), p.ExecutionRevision(), out)
}

// Each reviewed identity is fixed across plans, revisions, output paths and failed runs.
// Never remove a claim automatically or reuse any legacy campaign claim.
func persistRetestClaim(root, campaignID, revision, out string) error {
	if campaignID != taskbench.CashReadRetestID && campaignID != taskbench.CashReadRecoveryRetestID && campaignID != taskbench.RechargeConfirmID && campaignID != taskbench.RechargeDiagnosticRetestID && campaignID != taskbench.RechargeCancelID && campaignID != taskbench.RechargeSupplementID {
		return fmt.Errorf("unknown fixed retest scope")
	}

	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	if campaignID == taskbench.RechargeDiagnosticRetestID {
		prior, err := boundedFile(filepath.Join(dir, taskbench.RechargeConfirmID+".json"))
		if err != nil {
			return fmt.Errorf("exact consumed predecessor claim required")
		}
		sum := sha256.Sum256(prior)
		if hex.EncodeToString(sum[:]) != taskbench.RechargePriorClaimSHA {
			return fmt.Errorf("predecessor claim changed")
		}
	}
	if campaignID == taskbench.RechargeCancelID {
		for _, prior := range []struct{ id, sha string }{
			{taskbench.RechargeConfirmID, taskbench.RechargePriorClaimSHA},
			{taskbench.RechargeDiagnosticRetestID, taskbench.RechargeDiagnosticClaimSHA},
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
	}
	if campaignID == taskbench.RechargeSupplementID {
		for _, prior := range []struct{ id, sha string }{
			{taskbench.RechargeConfirmID, taskbench.RechargePriorClaimSHA},
			{taskbench.RechargeDiagnosticRetestID, taskbench.RechargeDiagnosticClaimSHA},
			{taskbench.RechargeCancelID, taskbench.RechargeCancelClaimSHA},
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
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, campaignID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("retest already claimed or durable claim unavailable")
	}
	record := map[string]any{"schema": "paired-live-retest-claim.v1", "campaign_id": campaignID, "execution_revision": revision, "output": out, "status": "consumed_before_execution"}
	if campaignID == taskbench.RechargeConfirmID || campaignID == taskbench.RechargeDiagnosticRetestID || campaignID == taskbench.RechargeCancelID || campaignID == taskbench.RechargeSupplementID {
		record["budget_ceiling_sha256"] = taskbench.RechargeBudgetCeilingSHA
		record["reserved_model_calls"] = 10
		record["reserved_model_micro_cny"] = 337920
		record["reserved_jev_calls"] = 1
		record["reserved_jev_micro_usd"] = 2688
	}
	if campaignID == taskbench.RechargeDiagnosticRetestID {
		record["prior_claim_sha256"] = taskbench.RechargePriorClaimSHA
		record["diagnostic_source_revision"] = taskbench.RechargeDiagnosticSourceRevision
		record["diagnostic_manifest_sha256"] = taskbench.RechargeDiagnosticManifestSHA
		record["cumulative_reserved_model_micro_cny"] = 675840
		record["cumulative_reserved_jev_micro_usd"] = 5376
	}
	if campaignID == taskbench.RechargeCancelID {
		record["prior_confirm_claim_sha256"] = taskbench.RechargePriorClaimSHA
		record["prior_diagnostic_claim_sha256"] = taskbench.RechargeDiagnosticClaimSHA
		record["diagnostic_source_revision"] = taskbench.RechargeDiagnosticSourceRevision
		record["diagnostic_manifest_sha256"] = taskbench.RechargeDiagnosticManifestSHA
		record["cumulative_reserved_model_micro_cny"] = 1013760
		record["cumulative_reserved_jev_micro_usd"] = 8064
	}
	if campaignID == taskbench.RechargeSupplementID {
		record["reserved_model_calls"] = 12
		record["reserved_model_micro_cny"] = 405504
		record["prior_confirm_claim_sha256"] = taskbench.RechargePriorClaimSHA
		record["prior_diagnostic_claim_sha256"] = taskbench.RechargeDiagnosticClaimSHA
		record["prior_cancel_claim_sha256"] = taskbench.RechargeCancelClaimSHA
		record["diagnostic_source_revision"] = taskbench.RechargeDiagnosticSourceRevision
		record["diagnostic_manifest_sha256"] = taskbench.RechargeDiagnosticManifestSHA
		record["cumulative_reserved_model_micro_cny"] = 1419264
		record["cumulative_reserved_jev_micro_usd"] = 10752
	}

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
