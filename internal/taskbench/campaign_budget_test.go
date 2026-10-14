package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCampaignSeparateCurrencyLimitsAndDefaultDisabled(t *testing.T) {
	if err := RunCampaignBatch(context.Background(), nil, AgentPairOptions{}); err == nil {
		t.Fatal("default campaign execution enabled")
	}
	for _, currency := range []string{"CNY", "USD"} {
		t.Run(currency, func(t *testing.T) {
			limits := pairedLimits()
			estimate, _ := EstimateDeclaredPairBudget(limits)
			l := CampaignLimits{MaxBatches: 3, MaxModelCalls: 20, MaxModelMicroCNY: 1000000, MaxJEVCalls: 20, MaxJEVMicroUSD: 100000, JEVMicroUSDPerAttempt: 2688}
			if currency == "CNY" {
				l.MaxModelMicroCNY = estimate.AllAttemptsMicroCNY*2 - 1
			} else {
				l.MaxJEVMicroUSD = 2688*2 - 1
			}
			c, err := OpenCampaignBudget(filepath.Join(t.TempDir(), "campaign"), strings.Repeat("a", 64), l)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			lease, err := c.ReserveBatch("batch-001", "binding", limits, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err = lease.Finish(false); err != nil {
				t.Fatal(err)
			}
			if _, err = c.ReserveBatch("batch-002", "binding", limits, 1); err == nil {
				t.Fatal("aggregate currency limit bypassed")
			}
		})
	}
}

func TestCampaignBudgetReservesAcrossBatchesAndRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "campaign")
	l := CampaignLimits{MaxBatches: 3, MaxModelCalls: 4, MaxModelMicroCNY: 100000, MaxJEVCalls: 3, MaxJEVMicroUSD: 9000, JEVMicroUSDPerAttempt: 2688}
	c, err := OpenCampaignBudget(dir, strings.Repeat("a", 64), l)
	if err != nil {
		t.Fatal(err)
	}
	limits := pairedLimits()
	lease, err := c.ReserveBatch("batch-001", "batch-contract", limits, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReserveBatch("batch-002", "batch-contract", limits, 1); err == nil {
		t.Fatal("allowed concurrent/unresolved batch")
	}
	if err = lease.Finish(false); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCampaignBudget(dir, strings.Repeat("a", 64), l)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.ReserveBatch("batch-001", "batch-contract", limits, 1); err == nil {
		t.Fatal("reused batch reservation")
	}
	lease, err = c.ReserveBatch("batch-002", "batch-contract", limits, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Finish(false); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReserveBatch("batch-003", "batch-contract", limits, 1); err == nil {
		t.Fatal("reset aggregate calls on resume")
	}
}

func TestCampaignBudgetRejectsTruncationChangedLimitsAndFailure(t *testing.T) {
	for _, mutation := range []string{"truncate", "limits", "failure", "pending"} {
		t.Run(mutation, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "campaign")
			l := CampaignLimits{MaxBatches: 3, MaxModelCalls: 20, MaxModelMicroCNY: 1000000, MaxJEVCalls: 4, MaxJEVMicroUSD: 12000, JEVMicroUSDPerAttempt: 2688}
			c, err := OpenCampaignBudget(dir, strings.Repeat("a", 64), l)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := c.ReserveBatch("batch-001", "batch-contract", pairedLimits(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if mutation != "pending" {
				if err = lease.Finish(mutation == "failure"); err != nil {
					t.Fatal(err)
				}
			}
			c.Close()
			if mutation == "truncate" {
				p := filepath.Join(dir, "campaign.jsonl")
				raw, _ := os.ReadFile(p)
				os.WriteFile(p, append([]byte(strings.Split(string(raw), "\n")[0]), '\n'), 0600)
			}
			if mutation == "limits" {
				l.MaxModelCalls++
			}
			c, err = OpenCampaignBudget(dir, strings.Repeat("a", 64), l)
			if mutation == "limits" || mutation == "truncate" {
				if err == nil {
					c.Close()
					t.Fatal("changed/truncated global budget accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if _, err = c.ReserveBatch("batch-002", "batch-contract", pairedLimits(), 1); err == nil {
				t.Fatal("continued failed/unfinished campaign")
			}
		})
	}
}

func TestCampaignRunningLeaseCannotBeFinishedByCaller(t *testing.T) {
	l := CampaignLimits{MaxBatches: 3, MaxModelCalls: 20, MaxModelMicroCNY: 1000000, MaxJEVCalls: 4, MaxJEVMicroUSD: 12000, JEVMicroUSDPerAttempt: 2688}
	c, err := OpenCampaignBudget(filepath.Join(t.TempDir(), "campaign"), strings.Repeat("a", 64), l)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	lease, err := c.ReserveBatch("batch-001", "binding", pairedLimits(), 1)
	if err != nil {
		t.Fatal(err)
	}
	lease.running = true
	if err = lease.Finish(false); err == nil {
		t.Fatal("external finish released running batch")
	}
	if _, err = c.ReserveBatch("batch-002", "binding", pairedLimits(), 1); err == nil {
		t.Fatal("running batch permitted next batch")
	}
	if err = lease.finish(false, true); err != nil {
		t.Fatal(err)
	}
}
