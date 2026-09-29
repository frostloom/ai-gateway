package main

import (
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
