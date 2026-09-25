// make_state 构造 M4 对账需要的两种「崩溃残留」状态（模拟 gateway 恰好在两个时间点挂掉）：
//
//	① m4-recon-nomark  Reserve 后（流未跑完，网关死在 ReportUsage 之前）→ 有预扣、无 marker
//	② m4-recon-settle  ReportUsage 后、Settle 前（网关死在结算之前）→ 有预扣、有 marker、无结算
//
// 对账按 marker 分类：
//
//	① → Reverse 全退；② → 补 Settle。
//
// 用法：go run ./scripts/m4/make_state.go （需 billing 已启动）
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/frostloom/ai-gateway/internal/pkg/config"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

const (
	tenantID = 1 // demo 租户（schema.sql 种子）
	apiKeyID = 1
)

func main() {
	addr := config.Getenv("BILLING_ADDR", "127.0.0.1:9101")
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect billing: %v", err)
	}
	defer conn.Close()
	cli := billingv1.NewBillingServiceClient(conn)
	ctx := context.Background()

	// ① 无 marker：只 Reserve，模拟网关死在 ReportUsage 之前
	resp1, err := cli.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "m4-recon-nomark", TenantId: tenantID, ApiKeyId: apiKeyID,
		Model: "mock-model", EstPromptTokens: 84,
	})
	if err != nil {
		log.Fatalf("reserve nomark: %v", err)
	}
	fmt.Printf("① m4-recon-nomark  reserved  bill_id=%d  balance_after=%d\n", resp1.BillId, resp1.BalanceAfter)

	// ② 有 marker、无 settle：Reserve + ReportUsage，模拟网关死在 Settle 之前
	resp2, err := cli.Reserve(ctx, &billingv1.ReserveRequest{
		RequestId: "m4-recon-settle", TenantId: tenantID, ApiKeyId: apiKeyID,
		Model: "mock-model", EstPromptTokens: 84,
	})
	if err != nil {
		log.Fatalf("reserve settle-case: %v", err)
	}
	if _, err := cli.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: "m4-recon-settle", BillId: resp2.BillId, PromptTokens: 50, CompletionTokens: 100,
	}); err != nil {
		log.Fatalf("report usage: %v", err)
	}
	fmt.Printf("② m4-recon-settle  reported(50,100)  bill_id=%d（未 settle，等对账补）\n", resp2.BillId)

	bal, err := cli.GetBalance(ctx, &billingv1.GetBalanceRequest{TenantId: tenantID})
	if err != nil {
		log.Fatalf("get balance: %v", err)
	}
	fmt.Printf("state created, balance=%d\n", bal.Balance)
	time.Sleep(2 * time.Second) // 让 created_at 早于对账扫描的 cutoff（配合 -grace 1s）
}
