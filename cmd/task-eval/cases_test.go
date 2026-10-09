package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func TestExternalCasesDoNotInheritOmittedDefaultFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"id":"one","message":"read","read":true,"tool":"get_balance"},{"id":"two","message":"change","tool":"change_plan","plan":"flagship","initial_subscription":true}]`), 0600); err != nil {
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

func TestLoadCasesRejectsInvalidDatasets(t *testing.T) {
	// Each mutation removes a pre-run safety or scoring assumption.
	valid := taskbench.Case{ID: "one", Message: "充值100元", Tool: "recharge", AmountMoney: 100}
	mutations := []struct {
		name, want string
		change     func(*taskbench.Case)
	}{
		{"empty id", "id", func(c *taskbench.Case) { c.ID = "  " }},
		{"empty message", "message", func(c *taskbench.Case) { c.Message = "\t" }},
		{"unknown tool", "tool", func(c *taskbench.Case) { c.Tool = "list_plans" }},
		{"write marked read", "read", func(c *taskbench.Case) { c.Read = true }},
		{"cancel and repeat", "cancel", func(c *taskbench.Case) { c.Cancel = true; c.DuplicateConfirm = true }},
		{"missing followup", "followup", func(c *taskbench.Case) { c.MissingSlot = "amount" }},
		{"followup without slot", "missing_slot", func(c *taskbench.Case) { c.Followup = "充值100元" }},
		{"unsupported slot", "missing_slot", func(c *taskbench.Case) { c.MissingSlot = "order"; c.Followup = "订单123" }},
		{"wrong slot", "missing_slot", func(c *taskbench.Case) { c.MissingSlot = "plan"; c.Followup = "标准" }},
		{"zero amount", "amount_money", func(c *taskbench.Case) { c.AmountMoney = 0 }},
		{"negative amount", "amount_money", func(c *taskbench.Case) { c.AmountMoney = -1 }},
		{"overflow amount", "amount_money", func(c *taskbench.Case) { c.AmountMoney = 92233720368547759 }},
		{"irrelevant plan", "plan", func(c *taskbench.Case) { c.Plan = "standard" }},
		{"wrong order tool", "order_refund", func(c *taskbench.Case) { c.OrderRefund = true }},
		{"cancelled without subscription", "initially_cancelled", func(c *taskbench.Case) { c.InitiallyCancelled = true }},
		{"unsupported composite", "require_balance_answer", func(c *taskbench.Case) { c.RequireBalanceAnswer = true }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			c := valid
			tt.change(&c)
			data, _ := json.Marshal([]taskbench.Case{c})
			assertCasesError(t, string(data), tt.want)
		})
	}
	for _, tt := range []struct{ name, data, want string }{
		{"unknown field", `[{"id":"x","message":"余额","tool":"get_balance","read":true,"raed":true}]`, "unknown field"},
		{"trailing JSON", `[{"id":"x","message":"余额","tool":"get_balance","read":true}] []`, "trailing"},
		{"empty list", `[]`, "empty"}, {"null list", `null`, "empty"},
		{"duplicate id", `[{"id":"x","message":"余额","tool":"get_balance","read":true},{"id":"x","message":"余额","tool":"get_balance","read":true}]`, "duplicate"},
		{"read without flag", `[{"id":"x","message":"余额","tool":"get_balance"}]`, "read"},
		{"read cancellation", `[{"id":"x","message":"余额","tool":"get_balance","read":true,"cancel":true}]`, "read"},
		{"read duplicate", `[{"id":"x","message":"余额","tool":"get_balance","read":true,"duplicate_confirm":true}]`, "read"},
		{"missing plan", `[{"id":"x","message":"订购","tool":"subscribe_plan"}]`, "plan"},
		{"unknown plan", `[{"id":"x","message":"订购","tool":"subscribe_plan","plan":"premium"}]`, "plan"},
		{"plan amount", `[{"id":"x","message":"订购","tool":"subscribe_plan","plan":"standard","amount_money":1}]`, "amount_money"},
		{"change no subscription", `[{"id":"x","message":"改套餐","tool":"change_plan","plan":"flagship"}]`, "initial_subscription"},
		{"change cancelled", `[{"id":"x","message":"改套餐","tool":"change_plan","plan":"flagship","initial_subscription":true,"initially_cancelled":true}]`, "initially_cancelled"},
		{"unchanged standard target", `[{"id":"x","message":"改套餐","tool":"change_plan","plan":"standard","initial_subscription":true}]`, "plan"},
		{"subscribe already active", `[{"id":"x","message":"订购","tool":"subscribe_plan","plan":"standard","initial_subscription":true}]`, "initial_subscription"},
		{"cancel absent", `[{"id":"x","message":"退订","tool":"cancel_subscription"}]`, "initial_subscription"},
		{"order plus amount", `[{"id":"x","message":"退订单{order_no}","tool":"refund_recharge","order_refund":true,"amount_money":1}]`, "amount_money"},
		{"order missing template", `[{"id":"x","message":"退订单","tool":"refund_recharge","order_refund":true}]`, "order_no"},
	} {
		t.Run(tt.name, func(t *testing.T) { assertCasesError(t, tt.data, tt.want) })
	}
}

func assertCasesError(t *testing.T, data, want string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCases(path); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}

func TestLoadCasesAcceptsDefaultAndExternalDefaults(t *testing.T) {
	cases, err := loadCases("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "defaults.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCases(path)
	if err != nil || len(loaded) != 24 {
		t.Fatalf("defaults rejected: count=%d error=%v", len(loaded), err)
	}
}

func TestFrozenBoundaryDevelopmentSuite(t *testing.T) {
	dir := filepath.Join("..", "..", "eval", "task-suites", "2026-10-09-boundary-dev-v1")
	cases, err := loadCases(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatalf("want 12 development tasks, got %d", len(cases))
	}
	var manifest struct {
		Files []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 3 {
		t.Fatalf("incomplete suite manifest: %+v", manifest)
	}
	for _, file := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(dir, file.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatalf("frozen hash mismatch: %s", file.Path)
		}
	}
	var sources []struct {
		TaskID       string `json:"task_id"`
		SourceCaseID string `json:"source_case_id"`
		SourcePath   string `json:"source_path"`
		SourceText   string `json:"source_text"`
	}
	data, err = os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != len(cases) {
		t.Fatal("incomplete source mapping")
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if seen[source.TaskID] {
			t.Fatalf("duplicate source task %s", source.TaskID)
		}
		seen[source.TaskID] = true
		var catalog []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		data, err := os.ReadFile(filepath.Join("..", "..", source.SourcePath))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, original := range catalog {
			if original.ID == source.SourceCaseID && original.Text == source.SourceText {
				found = true
			}
		}
		if !found {
			t.Fatalf("source absent or text changed: %s", source.SourceCaseID)
		}
		for _, c := range cases {
			if c.ID == source.TaskID && c.Message != source.SourceText {
				t.Fatalf("unrecorded source transformation: %s", c.ID)
			}
		}
	}
	for _, c := range cases {
		if !seen[c.ID] {
			t.Fatalf("missing source: %s", c.ID)
		}
	}
}
