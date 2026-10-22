package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPiCampaignCLIRejectsMismatchedFlagsBeforeWriting(t *testing.T) {
	raw, err := os.ReadFile("../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	json.Unmarshal(raw, &plan)
	plan["schema"] = "campaign-preflight-plan.v2"
	plan["target"] = map[string]string{"base_url": "https://api.deepseek.com/v1", "model": "deepseek-flash", "protocol": "openai", "jev_model": "jev-1.13.0"}
	raw, _ = json.Marshal(plan)
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"match", "endpoint", "protocol", "model"} {
		t.Run(kind, func(t *testing.T) {
			out := filepath.Join(dir, kind+".json")
			args := campaignCLIArgs(out, path, "https://api.deepseek.com/v1")
			for i := 0; i < len(args); i += 2 {
				if args[i] == "-model-name" {
					args[i+1] = "deepseek-flash"
				}
				if args[i] == "-protocol" {
					args[i+1] = "openai"
				}
			}
			for i := 0; i < len(args); i += 2 {
				if kind == "endpoint" && args[i] == "-model-base" {
					args[i+1] = "https://other.invalid"
				}
				if kind == "protocol" && args[i] == "-protocol" {
					args[i+1] = "anthropic"
				}
				if kind == "model" && args[i] == "-model-name" {
					args[i+1] = "deepseek-v4.1-flash"
				}
			}
			code := run(args)
			if kind != "match" {
				if code == 0 {
					t.Fatal("accepted mismatched target")
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatal("rejected preparation created output")
				}
				return
			}
			if code != 0 {
				t.Fatal("Pi preparation failed")
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			var v struct {
				Schema string `json:"schema"`
				Allow  bool   `json:"allow_paid"`
				Ready  bool   `json:"ready_for_paid_run"`
				Calls  int    `json:"provider_calls"`
			}
			json.Unmarshal(data, &v)
			if v.Schema != "small-pilot-execution-proposal.v2" || v.Allow || v.Ready || v.Calls != 0 {
				t.Fatal("preparation changed execution state")
			}
		})
	}
}
