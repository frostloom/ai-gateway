package taskbench

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSmallPairedArchiveIntegrity(t *testing.T) {
	raw, e := os.ReadFile("../../eval/paired-pilots/2026-10-13-dev-v1/plan.json")
	if e != nil {
		t.Fatal(e)
	}
	if conversationHash(raw) != "c95fac67a08b515f4ae42c1213eb1f2725b1b120df8262ada7303342ce3b1232" {
		t.Fatal("frozen pilot changed")
	}
	var p SmallPairPlan
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	s, e := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateSmallPair(s, p); e != nil {
		t.Fatal(e)
	}
}
