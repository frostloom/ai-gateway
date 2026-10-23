package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestContinuationClaimPreservesOriginalAndRejectsReplay(t *testing.T) {
	root := t.TempDir()
	plan := []byte(`{"schema":"campaign-preflight-plan.v2"}`)
	first := filepath.Join(root, "first")
	os.Mkdir(first, 0700)
	path, err := claimAuthorization(root, plan, first)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	for _, out := range []string{"second", "different-output"} {
		err := claimContinuation(root, plan, first, filepath.Join(root, out))
		if out == "second" && err != nil {
			t.Fatal(err)
		}
		if out != "second" && err == nil {
			t.Fatal("repeated batch allowed")
		}
	}
	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Fatal("original claim changed")
	}
	if err := claimContinuation(t.TempDir(), plan, first, "out"); err == nil {
		t.Fatal("missing original claim accepted")
	}
}
func TestContinuationPermissionBinding(t *testing.T) {
	permission := map[string]any{"schema": "paired-continuation-authorization.v1", "human_authorized": true, "batch_id": "batch-002", "plan_sha256": "plan", "previous_receipt_sha256": "receipt", "review_sha256": "review", "automatic_paid_retries": false}
	raw, _ := json.Marshal(permission)
	if err := validateContinuationPermission(raw, "plan", "receipt", "review"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"schema", "batch_id", "plan_sha256", "previous_receipt_sha256", "review_sha256", "human_authorized", "automatic_paid_retries"} {
		altered := map[string]any{}
		json.Unmarshal(raw, &altered)
		if field == "human_authorized" {
			altered[field] = false
		} else if field == "automatic_paid_retries" {
			altered[field] = true
		} else {
			altered[field] = "wrong"
		}
		b, _ := json.Marshal(altered)
		if validateContinuationPermission(b, "plan", "receipt", "review") == nil {
			t.Fatalf("accepted wrong %s", field)
		}
	}
}
