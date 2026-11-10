package localpaired

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const frozenDefinitionsDir = "../../eval/task-definitions/2026-10-12-full-dev-v1"

func TestCampaignIsOnePassTwoArms(t *testing.T) {
	campaign, err := LoadCampaign(frozenDefinitionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if campaign.Passes != 1 {
		t.Fatalf("Passes = %d, want 1", campaign.Passes)
	}
	if want := []string{"model", "jev_selective"}; !reflect.DeepEqual(campaign.Arms, want) {
		t.Fatalf("Arms = %q, want %q", campaign.Arms, want)
	}
	if len(campaign.Selected) != 1220 {
		t.Fatalf("len(Selected) = %d, want 1220", len(campaign.Selected))
	}
	if !sort.StringsAreSorted(campaign.Selected) {
		t.Fatal("Selected is not sorted")
	}
	seen := make(map[string]bool, len(campaign.Selected))
	prefixCounts := map[string]int{}
	for _, id := range campaign.Selected {
		if seen[id] {
			t.Fatalf("duplicate selected source id %q", id)
		}
		seen[id] = true
		prefix, _, _ := strings.Cut(id, "-")
		prefixCounts[prefix]++
	}
	for _, prefix := range []string{"balance", "consumption", "plans", "subscription", "chitchat"} {
		if prefixCounts[prefix] != 100 {
			t.Errorf("%s selected count = %d, want 100", prefix, prefixCounts[prefix])
		}
	}
	if len(campaign.Source.Cases) != 4291 {
		t.Fatalf("len(Source.Cases) = %d, want 4291", len(campaign.Source.Cases))
	}
	if len(campaign.Added) != 0 {
		t.Fatalf("len(Added) = %d, want 0", len(campaign.Added))
	}
}

func TestFrozenDefinitionsUntouched(t *testing.T) {
	raw, err := os.ReadFile(frozenDefinitionsDir + "/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(raw))
	const want = "afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181"
	if got != want {
		t.Fatalf("manifest SHA256 = %s, want %s", got, want)
	}
}
