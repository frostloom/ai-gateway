package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"testing"
)

func TestComparisonRejectsUnpairedRuns(t *testing.T) {
	b := agent.IntentReport{SchemaVersion: 2, JevMode: "off", Model: "model", DatasetSHA256: "hash", Workers: 4}
	e := b
	e.JevMode = "on"
	if err := validateComparison(b, e); err != nil {
		t.Fatal(err)
	}
	e.Model = "other"
	if validateComparison(b, e) == nil {
		t.Fatal("accepted different models")
	}
	e.Model = b.Model
	e.DatasetSHA256 = "other"
	if validateComparison(b, e) == nil {
		t.Fatal("accepted different data")
	}
	e.DatasetSHA256 = b.DatasetSHA256
	b.Preds = []agent.IntentPrediction{{Sample: agent.IntentSample{ID: "1"}, HTTPCode: 200, Telemetry: &agent.TurnTelemetry{}}}
	e.Preds = []agent.IntentPrediction{{Sample: agent.IntentSample{ID: "1"}, HTTPCode: 200, Telemetry: &agent.TurnTelemetry{JevEnabled: true}}}
	if err := validateComparison(b, e); err != nil {
		t.Fatal(err)
	}
	e.Preds[0].Telemetry.JevEnabled = false
	if validateComparison(b, e) == nil {
		t.Fatal("accepted disabled JEV experiment")
	}
	e.Preds[0].Telemetry.JevEnabled = true
	e.Preds[0].Sample.ID = "2"
	if validateComparison(b, e) == nil {
		t.Fatal("accepted different samples")
	}
}
func TestMainComparisonRejectsUnattestedConfiguration(t *testing.T) {
	b := agent.IntentReport{SchemaVersion: 3, JevMode: "off", Model: "m", DatasetSHA256: "d", Workers: 1}
	e := b
	e.JevMode = "on"
	if validateComparison(b, e) == nil {
		t.Fatal("accepted new report without runtime hashes")
	}
}

func TestComparisonV3RequiresSharedRuntime(t *testing.T) {
	runtime := func(mode, prompt string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"model": "m", "protocol": "openai", "endpoint_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("gateway"))), "prompt_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))), "tools_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("tools"))), "contract_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("contract"))), "routing_mode": mode, "thresholds": map[string]float64{"intent_min": .9}, "max_output_tokens": 100, "reply_repair_limit": 1})
		return b
	}
	makeReport := func(mode, jev string) agent.IntentReport {
		var r agent.IntentReport
		raw, _ := json.Marshal(map[string]any{"SchemaVersion": 3, "JevMode": jev, "Model": "m", "Workers": 1, "DatasetSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte("dataset"))), "Runtime": runtime(mode, "prompt"), "EvaluationContractSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte("c"))), "FixtureDefinitionSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte("f")))})
		json.Unmarshal(raw, &r)
		return r
	}
	b, e := makeReport("model", "off"), makeReport("jev_selective", "on")
	if err := validateComparison(b, e); err != nil {
		t.Fatalf("valid shared-runtime comparison rejected: %v", err)
	}
	for _, key := range []string{"endpoint_sha256", "protocol", "prompt_sha256"} {
		t.Run(key, func(t *testing.T) {
			baseline, experiment := makeReport("model", "off"), makeReport("jev_selective", "on")
			var br, er map[string]any
			json.Unmarshal(baseline.Runtime, &br)
			json.Unmarshal(experiment.Runtime, &er)
			switch key {
			case "endpoint_sha256":
				er[key] = fmt.Sprintf("%x", sha256.Sum256([]byte("other-gateway")))
			case "protocol":
				br[key] = "unknown"
				er[key] = "unknown"
			case "prompt_sha256":
				br[key] = "arbitrary-matching-string"
				er[key] = br[key]
			}
			baseline.Runtime, _ = json.Marshal(br)
			experiment.Runtime, _ = json.Marshal(er)
			if validateComparison(baseline, experiment) == nil {
				t.Fatalf("accepted unverified or changed %s", key)
			}
		})
	}

	raw, _ := json.Marshal(e)
	var v map[string]any
	json.Unmarshal(raw, &v)
	v["Runtime"] = runtime("jev_selective", "different")
	raw, _ = json.Marshal(v)
	json.Unmarshal(raw, &e)
	if validateComparison(b, e) == nil {
		t.Fatal("accepted prompt difference")
	}
}
