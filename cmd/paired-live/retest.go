package main

import (
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
	if campaignID != taskbench.CashReadRetestID && campaignID != taskbench.CashReadRecoveryRetestID {
		return fmt.Errorf("unknown fixed retest scope")
	}

	dir := filepath.Join(root, ".tmp", "paired-live-retests")
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
	raw, _ := json.Marshal(map[string]string{"schema": "paired-live-retest-claim.v1", "campaign_id": campaignID, "execution_revision": revision, "output": out, "status": "consumed_before_execution"})
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
