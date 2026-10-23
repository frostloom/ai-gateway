package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRechargeEntryPlanStrictDecodeAndOfflinePreparation(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry"
	plan := filepath.Join(base, "plan.json")
	if _, err := readCampaignPlan(plan); err != nil {
		t.Fatalf("new bound identity not supported by offline reader: %v", err)
	}
	raw, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["unexpected_field"] = "must reject"
	changed, _ := json.Marshal(m)
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, changed, 0600)
	if _, err := readCampaignPlan(bad); err == nil {
		t.Fatal("unknown fields relaxed")
	}
	out := filepath.Join(t.TempDir(), "proposal.json")
	args := []string{"-mode", "campaign-preflight", "-campaign-plan", plan, "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", "-model-base", "https://api.deepseek.com/v1", "-model-name", "deepseek-flash", "-protocol", "openai", "-revision", "5bb2d3774f458bb017aea645e93d29de32bbb2cf", "-jev-model", "jev-1.13.0", "-out", out}
	if run(args) != 0 {
		t.Fatal("offline preparation rejected new scope")
	}
	proposal, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	json.Unmarshal(proposal, &p)
	if p["campaign_id"] != "recharge-confirm-20261023" || p["allow_paid"] != false || p["ready_for_paid_run"] != false || p["provider_calls"] != float64(0) {
		t.Fatal("proposal lost identity or enabled execution")
	}
}
