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
	if campaignID != taskbench.CashReadRetestID && campaignID != taskbench.CashReadRecoveryRetestID && campaignID != taskbench.RechargeConfirmID && campaignID != taskbench.RechargeDiagnosticRetestID {
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
	if campaignID == taskbench.RechargeConfirmID || campaignID == taskbench.RechargeDiagnosticRetestID {
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
