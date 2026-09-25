// M1 冒烟脚本：对真实 billing 服务发 gRPC，验证 Reserve 正常/幂等/余额不足 + GetBalance。
// 用法：go run scripts/m1/smoke.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func main() {
	addr := "127.0.0.1:9101"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	c := billingv1.NewBillingServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	check("初始余额", func() { mustBalance(c, ctx, 1, 1_000_000) })

	// 1) 正常预占：840 prompt token → mock-model pre = 840*1.2 = 1008
	resp := mustReserve(c, ctx, "smoke-1", 840)
	check("预占 OK", func() {
		if resp.Code != billingv1.ReserveCode_RESERVE_OK {
			panic(fmt.Sprintf("code=%v", resp.Code))
		}
		fmt.Printf("  bill_id=%d balance_after=%d\n", resp.BillId, resp.BalanceAfter)
	})
	check("余额扣 1008", func() { mustBalance(c, ctx, 1, 1_000_000-1008) })

	// 2) 幂等重放：同一 request_id，不二次扣减
	resp2 := mustReserve(c, ctx, "smoke-1", 840)
	check("幂等重放返回同一 bill", func() {
		if resp2.BillId != resp.BillId {
			panic(fmt.Sprintf("bill_id 变了 %d → %d", resp.BillId, resp2.BillId))
		}
		fmt.Printf("  bill_id=%d balance_after=%d（未二次扣减）\n", resp2.BillId, resp2.BalanceAfter)
	})

	// 3) 余额不足：pre=2400000 > 998992
	resp3 := mustReserve(c, ctx, "smoke-2", 2_000_000)
	check("余额不足返回 INSUFFICIENT", func() {
		if resp3.Code != billingv1.ReserveCode_RESERVE_INSUFFICIENT {
			panic(fmt.Sprintf("code=%v", resp3.Code))
		}
		fmt.Printf("  balance_after=%d\n", resp3.BalanceAfter)
	})

	check("最终余额", func() { mustBalance(c, ctx, 1, 1_000_000-1008) })
	fmt.Println("M1 smoke: PASS")
}

func mustReserve(c billingv1.BillingServiceClient, ctx context.Context, rid string, est int64) *billingv1.ReserveResponse {
	r, err := c.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId:       rid,
		TenantId:        1,
		ApiKeyId:        1,
		Model:           "mock-model",
		EstPromptTokens: est,
	})
	if err != nil {
		panic(fmt.Sprintf("Reserve(%s): %v", rid, err))
	}
	return r
}

func mustBalance(c billingv1.BillingServiceClient, ctx context.Context, tenant int64, want int64) {
	r, err := c.GetBalance(ctx, &billingv1.GetBalanceRequest{TenantId: tenant})
	if err != nil {
		panic(err)
	}
	if r.Balance != want {
		panic(fmt.Sprintf("balance=%d want %d", r.Balance, want))
	}
	fmt.Printf("  balance=%d（期望 %d）\n", r.Balance, want)
}

func check(name string, fn func()) {
	fmt.Println("== " + name)
	fn()
}
