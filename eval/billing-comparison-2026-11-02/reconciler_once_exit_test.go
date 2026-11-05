package comparison

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestReconcilerOnceSignalsSettlementFailure(t *testing.T) {
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour)
	tenant := store.Tenant{Name: fmt.Sprintf("once-exit-%d", time.Now().UnixNano()), InitialQuota: 100}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	bill := store.Bill{RequestID: tenant.Name, TenantID: tenant.ID, Phase: store.PhaseReserve, Status: store.StatusPending, APIKeyID: 1, Model: "test", Funding: store.FundingBalance, UsageReportedAt: &now, CreatedAt: now}
	if err := db.Create(&bill).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&bill) })
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	binary := filepath.Join(t.TempDir(), "reconciler")
	build := exec.Command("go", "build", "-o", binary, "./cmd/reconciler")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-once", "-no-rebuild", "-grace=0")
	cmd.Env = append(os.Environ(), "MYSQL_DSN="+os.Getenv("COMPARISON_MYSQL_DSN"), "BILLING_ADDR="+address, "CONTROL_PLANE_TOKEN=isolated", "RECONCILER_PORT=0")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("process timeout: %v output=%s", ctx.Err(), output)
	}
	if !strings.Contains(string(output), "recon settle") {
		t.Fatalf("expected settlement failure, err=%v output=%s", err, output)
	}
	if err == nil {
		t.Fatalf("failed settlement returned success: output=%s", output)
	}
	var after store.Bill
	if err := db.First(&after, bill.ID).Error; err != nil || after.Status != store.StatusPending || after.UsageReportedAt == nil {
		t.Fatalf("bill=%+v err=%v", after, err)
	}
	t.Logf("settlement failure returned nonzero and bill remained pending: %v", err)
	if err := db.Model(&bill).UpdateColumn("usage_reported_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		fail    bool
		message string
	}{
		{"unknown_retained", []string{"-once", "-no-rebuild", "-grace=0"}, false, "held=1"},
		{"reverse_failure", []string{"-once", "-no-rebuild", "-grace=0", "-no-auto-reverse=false"}, true, "recon reverse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			childCtx, childCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer childCancel()
			child := exec.CommandContext(childCtx, binary, tc.args...)
			child.Env = cmd.Env
			output, runErr := child.CombinedOutput()
			if childCtx.Err() != nil || (runErr != nil) != tc.fail || !strings.Contains(string(output), tc.message) {
				t.Fatalf("error=%v context=%v output=%s", runErr, childCtx.Err(), output)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		args    []string
		fail    bool
		message string
	}{
		{"rebuild_failure", []string{"-once", "-no-sweep"}, true, "recon rebuild"},
		{"no_operations", []string{"-once", "-no-sweep", "-no-rebuild"}, false, "one-shot done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			childCtx, childCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer childCancel()
			child := exec.CommandContext(childCtx, binary, tc.args...)
			child.Env = cmd.Env
			output, runErr := child.CombinedOutput()
			if childCtx.Err() != nil || (runErr != nil) != tc.fail || !strings.Contains(string(output), tc.message) {
				t.Fatalf("error=%v context=%v output=%s", runErr, childCtx.Err(), output)
			}
		})
	}
}
