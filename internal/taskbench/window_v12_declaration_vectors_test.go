package taskbench

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This cross-language fixture pins Go field order and canonical bytes. A Go
// wire change must not silently leave Python consumers on a different digest.
func TestWindowV12DeclarationVectors(t *testing.T) {
	type vector struct {
		Canonical string `json:"canonical"`
		SHA256    string `json:"sha256"`
	}
	makeVector := func(value any) vector {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return vector{string(raw), conversationHash(raw)}
	}
	vectors := struct {
		Schema                 string `json:"schema"`
		DataOnly               bool   `json:"data_only"`
		Budget                 vector `json:"budget"`
		Retry                  vector `json:"retry"`
		Completion             vector `json:"completion"`
		OriginalScheduleSHA256 string `json:"original_schedule_sha256"`
		SuffixWindowsSHA256    string `json:"suffix_windows_sha256"`
		SuffixCapsSHA256       string `json:"suffix_caps_sha256"`
	}{"full-window-v12-declaration-vectors.v1", true, makeVector(expectedWindowV12BudgetDeclaration()), makeVector(expectedWindowV12RetryDeclaration()), makeVector(expectedWindowCompletionDeclarationV12()), windowV10ScheduleSHA, windowV10SuffixWindowsSHA, windowV10SuffixCapsSHA}
	want, e := json.MarshalIndent(vectors, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	want = append(want, '\n')
	name := filepath.Join("..", "..", "eval", "behavior-review", "fixtures", "v12-contracts", "go-declarations.json")
	if os.Getenv("TASKBENCH_UPDATE_V12_DECLARATION_VECTORS") == "1" {
		if e = os.MkdirAll(filepath.Dir(name), 0755); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(name, want, 0644); e != nil {
			t.Fatal(e)
		}
	}
	got, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("V12 cross-language fixture differs from canonical Go declarations")
	}
}
