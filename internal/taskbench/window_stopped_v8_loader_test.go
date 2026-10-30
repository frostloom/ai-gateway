package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStoppedV8Exact46Baseline(t *testing.T) {
	b := expectedStoppedV8WindowBaseline()
	if len(b.ClaimSHA256) != 46 || b.TotalModel != 200000000 || b.TotalJEV != 4000000 || b.PermanentModel != 52654504 || b.PermanentJEV != 408701 || b.RemainingModel != 147345496 || b.RemainingJEV != 3591299 || b.ActualModelCalls != 3557 || b.ActualJEVCalls != 918 {
		t.Fatalf("missing exact stopped46: %+v", b)
	}
	for k, v := range expectedStoppedV7WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old44 changed")
		}
	}
	delete(b.ClaimSHA256, "full1220-v8-20261030/window-000002")
	if len(expectedStoppedV8WindowBaseline().ClaimSHA256) != 46 {
		t.Fatal("shared claim map")
	}
}
func TestWindowStoppedV8DescriptorBindings(t *testing.T) {
	root := "/synthetic/stopped-v8"
	e := windowStoppedV8Evidence{Schema: "full-window-stopped-baseline.v8", PreviousBaseline: windowActualFile{Path: "/synthetic/old44.json", SHA256: windowV8BaselineSHA}, OriginalRoot: root, BudgetApproval: windowActualFile{Path: filepath.Join(root, "human-approval-reply.private.json"), SHA256: "22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57"},
		Manifest:           windowActualFile{Path: filepath.Join(root, "actual-manifest.json"), SHA256: "a6e9eee26e6cdd044a05c2b23fb2fdfb4410643f3147970b10dc8139d4c98527"},
		StoppedAudit:       windowActualFile{Path: filepath.Join(root, "score-preparation/stopped-delivery/audit-source/data-audit.json"), SHA256: "27390161e46f4effdefb6e4407c6924f365481992757a9950981a28a766230ca"},
		Inventory:          windowActualFile{Path: filepath.Join(root, "stop-diagnosis/original-inventory.json"), SHA256: "1a6d25b3c97ac8d56e54dbdc24e28db19c491f5a0c94cc0f31d84d2131c58218"},
		SourceConfirmation: windowActualFile{Path: filepath.Join(root, "semantic-review/source-binding-confirmation.json"), SHA256: "fdd9e7096fb09e35d24983d32eb9317e2fc54294e6fb6a9d3690461458e9c3ea"},
		OwnerExit:          windowActualFile{Path: filepath.Join(root, "actual-owner-exit.json"), SHA256: "24454c796b205dbf91ddc534aad4b210911bbb6d5ac37aba9305887366dfa9de"}}
	if err := validateWindowStoppedV8Descriptor(e); err != nil {
		t.Fatal(err)
	}
	for _, mut := range []func(*windowStoppedV8Evidence){func(v *windowStoppedV8Evidence) { v.Manifest.SHA256 = v.PreviousBaseline.SHA256 }, func(v *windowStoppedV8Evidence) { v.Inventory.Path += "x" }, func(v *windowStoppedV8Evidence) { v.SourceConfirmation.SHA256 = "" }, func(v *windowStoppedV8Evidence) { v.OriginalRoot = "relative" }, func(v *windowStoppedV8Evidence) { v.Schema = "full-window-stopped-baseline.v7" }} {
		v := e
		mut(&v)
		if validateWindowStoppedV8Descriptor(v) == nil {
			t.Fatal("descriptor substitution accepted")
		}
	}
}
func TestWindowStoppedV8UntrustedDescriptorAndCancelHaveNoEffects(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	p := filepath.Join(root, "untrusted.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{windowV9BaselineSHA, windowV8BaselineSHA, conversationHash([]byte(`{}`))} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v, err := loadWindowStoppedV8BaselineContext(ctx, p, sha)
		if err == nil || len(v.base.ClaimSHA256) != 0 {
			t.Fatal("untrusted/canceled descriptor yielded baseline")
		}
	}
	names, err := os.ReadDir(root)
	if err != nil || len(names) != 1 {
		t.Fatal("rejection mutated namespace")
	}
	raw, _ := json.Marshal(expectedStoppedV8WindowBaseline())
	if len(raw) == 0 {
		t.Fatal("baseline serialization")
	}
}
