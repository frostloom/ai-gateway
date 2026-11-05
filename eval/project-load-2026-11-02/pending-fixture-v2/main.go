// Synthetic pending billing fixtures for an isolated reconciler experiment.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func main() {
	addr, token := os.Getenv("TEST_BILLING_ADDR"), os.Getenv("CONTROL_PLANE_TOKEN")
	if addr == "" || token == "" {
		panic("isolated billing address and token required")
	}
	b, e := client.NewBilling(addr, token)
	if e != nil {
		panic(e)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	suffix := time.Now().UnixNano()
	usageID := fmt.Sprintf("grace-usage-%d", suffix)
	emptyID := fmt.Sprintf("grace-empty-%d", suffix)
	reserve := func(id string) *billingv1.ReserveResponse {
		r, e := b.Reserve(ctx, &billingv1.ReserveRequest{RequestId: id, TenantId: 1, ApiKeyId: 1, Model: "test-model", EstPromptTokens: 84})
		if e != nil || r == nil || r.Code != billingv1.ReserveCode_RESERVE_OK {
			panic(fmt.Sprintf("reserve %s: %v %v", id, r, e))
		}
		return r
	}
	used := reserve(usageID)
	if r, e := b.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: usageID, BillId: used.BillId, PromptTokens: 50}); e != nil || r == nil || !r.Accepted {
		panic(fmt.Sprintf("report %v %v", r, e))
	}
	empty := reserve(emptyID)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"usage_request_id": usageID, "usage_bill_id": used.BillId, "empty_request_id": emptyID, "empty_bill_id": empty.BillId, "note": "synthetic records; no upstream was called"})
}
