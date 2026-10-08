package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExternalCasesDoNotInheritOmittedDefaultFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"id":"one","message":"read","read":true},{"id":"two","message":"change","tool":"change_plan","initial_subscription":true}]`), 0600); err != nil {
		t.Fatal(err)
	}
	cases, err := loadCases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[1].MissingSlot != "" || cases[1].Followup != "" || cases[1].AmountMoney != 0 {
		t.Fatalf("external case inherited unrelated default fields: %+v", cases)
	}
}
