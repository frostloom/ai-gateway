package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationRejectsOverwriteAndMissingWriteScope(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	c := runConfig{Base: "http://127.0.0.1:9105", Out: out, JevMode: "on", Model: "test", Version: "test", Source: "known", AgentBinary: "bin/agent.exe", AllowWrites: true}
	if err := validateRun(c); err != nil {
		t.Fatalf("fresh local run rejected: %v", err)
	}
	c.AllowWrites = false
	if err := validateRun(c); err == nil || !strings.Contains(err.Error(), "allow-simulated-writes") {
		t.Fatalf("missing explicit simulation scope: %v", err)
	}
	c.AllowWrites = true
	if err := os.WriteFile(out, []byte("frozen"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateRun(c); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("existing report not protected: %v", err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "frozen" {
		t.Fatal("existing evidence changed")
	}
}

func TestValidationDoesNotSendFixtureCredentialsToRemoteAgent(t *testing.T) {
	c := runConfig{Base: "https://example.com", Out: filepath.Join(t.TempDir(), "report.json"), JevMode: "off", Model: "test", Version: "test", Source: "known", AgentBinary: "test.exe", AllowWrites: true}
	if err := validateRun(c); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("unexpected remote agent permission: %v", err)
	}
}
