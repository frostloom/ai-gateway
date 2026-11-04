package taskbench

import (
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/observability"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This exports data-only wire bytes. It creates no key and signs nothing.
func TestWindowV12AuthorityWireVector(t *testing.T) {
	raw, e := os.ReadFile("../../eval/behavior-review/fixtures/v9-contracts/go-delegated-authority.json")
	if e != nil {
		t.Fatal(e)
	}
	var prior struct {
		Authorization windowV12ExecutionAuthorization `json:"authorization"`
	}
	if e = json.Unmarshal(raw, &prior); e != nil {
		t.Fatal(e)
	}
	a := prior.Authorization
	b := &a.windowV12ExecutionBinding
	b.Schema = "full-window-delegated-execution-authorization.v12"
	b.Parent = windowV8Amounts{195000000, 3000000}
	b.PrivateSettingsSHA256 = strings.Repeat("a", 64)
	b.SpoolRoot = "/controlled/spool"
	b.BudgetDeclarationSHA256 = "047ba0eb6c92ced4c2aeaee842c0e03a6afcdb246277dd6a4bff3149526cedb0"
	b.RetryPolicySHA256 = "26fef0e90b466ed651b2e8bc11790d336d12d75554f14e7fd06a8d18009cf088"
	b.CompletionSHA256 = "54fbf1a840485510a35356f3b33abf9b78fdd9b827b13a8ad0f3c2d0d373fd7f"
	b.ExecutorEquivalenceSHA256 = strings.Repeat("f", 64)
	b.Observer = observability.EvaluationIdentity{Enabled: true, CaptureContent: true, SampleRate: 1, Environment: "evaluation", Release: strings.Repeat("c", 40), ExportTimeoutMS: 10000, QueueSize: 4096, EndpointSHA256: strings.Repeat("d", 64), ProjectSHA256: strings.Repeat("e", 64), ObservationReceiptBoundary: "durable_local", SpoolRootSHA256: conversationHash([]byte(b.SpoolRoot)), SpoolRunID: strings.Repeat("b", 64), SpoolMaxBytes: 4 << 30}
	a.Signature = strings.Repeat("0", 128)
	canonical, _ := json.Marshal(b)
	authority, _ := json.Marshal(a)
	out := struct {
		DataOnly      bool            `json:"data_only"`
		Authorization json.RawMessage `json:"authorization"`
		SigningBytes  string          `json:"signing_bytes"`
		SigningSHA    string          `json:"signing_sha256"`
	}{true, authority, string(canonical), conversationHash(canonical)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	encoded = append(encoded, '\n')
	path := filepath.Join("../../eval/behavior-review/fixtures/v12-contracts", "go-authority-data.json")
	if os.Getenv("TASKBENCH_UPDATE_V12_AUTHORITY_VECTOR") == "1" {
		if e = os.WriteFile(path, encoded, 0600); e != nil {
			t.Fatal(e)
		}
	}
	got, e := os.ReadFile(path)
	if e != nil || string(got) != string(encoded) {
		t.Fatal("canonical authority DATA vector differs", e)
	}
}
