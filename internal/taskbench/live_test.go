package taskbench

import (
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePlansRejectsMissingOrAmbiguousCatalogue(t *testing.T) {
	for _, plans := range [][]store.Plan{nil, {{ID: 1, Name: "月度·标准", PlanType: store.PlanTypeRecurring}}, {{ID: 1, Name: "月度·标准", PlanType: store.PlanTypeRecurring}, {ID: 2, Name: "月度·标准", PlanType: store.PlanTypeRecurring}, {ID: 3, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring}}} {
		if _, _, err := ResolvePlans(plans); err == nil {
			t.Fatal("bad catalogue accepted")
		}
	}
}
func TestReportWriteNeverOverwritesHistoricalEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte("historical"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteReport(path, Report{}); err == nil {
		t.Fatal("existing report overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "historical" {
		t.Fatal("historical evidence destroyed")
	}
}
