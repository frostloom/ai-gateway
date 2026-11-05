package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	b, err := client.NewBilling(os.Getenv("BILLING_ADDR"), os.Getenv("CONTROL_PLANE_TOKEN"))
	if err != nil {
		panic(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = b.Reserve(ctx, &pb.ReserveRequest{RequestId: "eval-v1-forbidden", TenantId: 1, ApiKeyId: 1, Model: "test-model", EstPromptTokens: 10})
	if status.Code(err) != codes.FailedPrecondition {
		panic(fmt.Sprintf("expected FailedPrecondition: %v", err))
	}
	fmt.Println("legacy reserve rejected before write")
}
