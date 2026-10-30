package taskbench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWindowV8Authority(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	reply := filepath.Join(stoppedEvidenceRoot(t), "synthetic-human-reply.txt")
	body := []byte("synthetic controlled test reply, no real authorization")
	if err = os.WriteFile(reply, body, 0600); err != nil {
		t.Fatal(err)
	}
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v8", Scope: scope, ScopeSHA256: digest, ExecutionSHA256: digest, BuildAttestationPublicKey: hex.EncodeToString(pub), Sources: map[string]string{"synthetic.go": digest}}
	sources, _ := json.Marshal(m.Sources)
	binding := windowV8ExecutionBinding{Schema: "full-window-execution-authorization.v8", HumanAuthorized: true, RetryPolicyApproved: true, HumanReply: windowActualFile{reply, conversationHash(body)}, ManifestSHA256: digest, SourceSHA256: conversationHash(sources), BinarySHA256: digest, ScopeSHA256: digest, ExecutionSHA256: digest, BudgetDeclarationSHA256: scope.BudgetDeclarationSHA256, RetryPolicySHA256: scope.RetryPolicySHA256, Parent: windowV8Amounts{140000000, 1100000}, OwnerID: "synthetic-test-owner", SingleOwner: true, NoRelaunch: true}
	sign := func(b windowV8ExecutionBinding) []byte {
		canonical, _ := json.Marshal(b)
		raw, _ := json.Marshal(windowV8ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
		return raw
	}
	if out := os.Getenv("WINDOW_V8_EXPORT_SYNTHETIC"); out != "" {
		authorization := sign(binding)
		var a windowV8ExecutionAuthorization
		json.Unmarshal(authorization, &a)
		canonical, _ := json.Marshal(binding)
		vector := map[string]any{"evidence_kind": "synthetic_controlled_fixture_not_authority", "public_key_hex": hex.EncodeToString(pub), "binding": binding, "canonical_binding_utf8": string(canonical), "signature_hex": a.Signature, "authorization": json.RawMessage(authorization)}
		raw, _ := json.MarshalIndent(vector, "", "  ")
		if err = os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(out, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = verifyWindowV8ExecutionAuthorization(sign(binding), m, digest, digest); err != nil {
		t.Fatalf("valid controlled signature rejected: %v", err)
	}
	for _, mode := range []string{"missing", "signature", "reply", "empty_reply_sha", "invalid_reply_sha", "budget", "policy", "source", "binary", "manifest", "scope", "execution", "owner", "relaunch", "human", "retry"} {
		t.Run(mode, func(t *testing.T) {
			b := binding
			switch mode {
			case "empty_reply_sha":
				b.HumanReply.SHA256 = ""
			case "invalid_reply_sha":
				b.HumanReply.SHA256 = "not-a-digest"
			case "reply":
				b.HumanReply.SHA256 = digest
			case "budget":
				b.Parent.Model--
			case "policy":
				b.RetryPolicySHA256 = digest
			case "source":
				b.SourceSHA256 = digest
			case "binary":
				b.BinarySHA256 = strings.Repeat("b", 64)
			case "manifest":
				b.ManifestSHA256 = strings.Repeat("b", 64)
			case "scope":
				b.ScopeSHA256 = strings.Repeat("b", 64)
			case "execution":
				b.ExecutionSHA256 = strings.Repeat("b", 64)
			case "owner":
				b.OwnerID = ""
			case "relaunch":
				b.NoRelaunch = false
			case "human":
				b.HumanAuthorized = false
			case "retry":
				b.RetryPolicyApproved = false
			}
			raw := sign(b)
			if mode == "missing" {
				raw = nil
			}
			if mode == "signature" {
				raw[len(raw)-5] ^= 1
			}
			if _, err := verifyWindowV8ExecutionAuthorization(raw, m, digest, digest); err == nil {
				t.Fatal("accepted forged or mismatched authority")
			}
		})
	}
}
