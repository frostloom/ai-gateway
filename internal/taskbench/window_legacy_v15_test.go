package taskbench

import (
	"encoding/json"
	"os"
	"testing"
)

// The SHA fields come from immutable original manifests, not the new builder.
// Existing TestWindowV8LegacyFieldInjectionAndBytes covers epochs1..7.
func TestWindowV15PreservesFrozenLegacyScopeBytes(t *testing.T) {
	for _, tc := range []struct{ root, sha string }{
		{"/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v8-freeze-20261030", windowStoppedV8ManifestSHA},
		{windowStoppedV9OriginalRoot, windowStoppedV9ManifestSHA},
		{windowStoppedV10Root, windowStoppedV10ManifestSHA},
		{windowStoppedV11Root, windowStoppedV11ManifestSHA},
		{windowStoppedV12Root, windowStoppedV12ManifestSHA},
		{windowStoppedV13Root, windowStoppedV13ManifestSHA},
		{windowStoppedV14Root, windowStoppedV14ManifestSHA},
	} {
		raw, err := os.ReadFile(tc.root + "/actual-manifest.json")
		if err != nil || conversationHash(raw) != tc.sha {
			t.Fatal("original changed", tc.root, err)
		}
		var m windowActualManifest
		if err = json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(m.Scope)
		if err != nil || conversationHash(wire) != m.ScopeSHA256 {
			t.Fatal("legacy scope wire changed", m.Schema, err)
		}
		c := expectedWindowCompletionDeclarationV15()
		m.Scope.CompletionV15 = &c
		if validateWindowNativeDeclarations(m.Scope) == nil {
			t.Fatal("legacy accepted15 completion", m.Schema)
		}
	}
}
