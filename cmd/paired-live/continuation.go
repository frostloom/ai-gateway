package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"os"
	"path/filepath"
)

func validateContinuationPermission(raw []byte, plan, receipt, review string) error {
	var a struct {
		Schema   string `json:"schema"`
		Approved bool   `json:"human_authorized"`
		Batch    string `json:"batch_id"`
		Plan     string `json:"plan_sha256"`
		Receipt  string `json:"previous_receipt_sha256"`
		Review   string `json:"review_sha256"`
		Retry    bool   `json:"automatic_paid_retries"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &a) != nil || a.Schema != "paired-continuation-authorization.v1" || !a.Approved || a.Batch != "batch-002" || a.Plan != plan || a.Receipt != receipt || a.Review != review || a.Retry {
		return fmt.Errorf("explicit bound batch-002 permission required")
	}
	return nil
}

func claimContinuation(root string, plan []byte, previous, out string) error {
	// Locate the existing canonical claim without creating or rewriting it.
	// Use exactly the same typed canonicalization as the first-batch claim.
	path, err := authorizationClaimPath(root, plan)
	if err != nil {
		return err
	}
	raw, err := boundedFile(path)
	if err != nil {
		return err
	}
	var c struct {
		Schema string `json:"schema"`
		Output string `json:"output"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Schema != "paired-live-claim.v1" || c.Status != "consumed_before_execution" {
		return fmt.Errorf("original claim required")
	}
	original, err := filepath.EvalSymlinks(c.Output)
	if err != nil {
		return err
	}
	expected, err := filepath.EvalSymlinks(previous)
	if err != nil {
		return err
	}
	if original != expected {
		return fmt.Errorf("checkpoint differs from original claim")
	}
	f, err := os.OpenFile(path+".batch-002", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	marker, _ := json.Marshal(map[string]string{"schema": "paired-live-continuation-claim.v1", "batch_id": "batch-002", "previous": expected, "output": out})
	_, w := f.Write(append(marker, '\n'))
	s := f.Sync()
	cErr := f.Close()
	if err := errors.Join(w, s, cErr); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func authorizationClaimPath(root string, plan []byte) (string, error) {
	canonical, err := canonicalClaimScope(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return filepath.Join(root, ".tmp", "paired-live-authorizations", hex.EncodeToString(sum[:])+".json"), nil
}

func writeContinuationCheck(out io.Writer, p taskbench.AuthorizedPilot, c taskbench.AuthorizedContinuation) int {
	if json.NewEncoder(out).Encode(map[string]any{"mode": "check", "execution_scope": "batch-002 only, then checkpoint", "checkpoint_validated": true, "ready_for_paid_run": false, "continuation_permission_required": true, "billing_policy_ready": p.Ready(), "blockers": p.Blockers(), "previous_receipt_sha256": c.ReceiptSHA(), "review_sha256": c.ReviewSHA(), "previous_model_calls": c.PreviousModelCalls(), "previous_jev_calls": c.PreviousJEVCalls(), "model_calls": 0, "jev_calls": 0}) != nil {
		return 1
	}
	return 0
}
