package comparison

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestReserveRejectsMalformedContractBeforeStore(t *testing.T) {
	svc := server.NewService(nil, slog.New(slog.DiscardHandler))
	for name, md := range map[string]metadata.MD{
		"missing-digest":      metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2),
		"short":               metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2, controlplane.ReserveContentKey, "short"),
		"uppercase":           metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2, controlplane.ReserveContentKey, strings.Repeat("A", 64)),
		"nonhex":              metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2, controlplane.ReserveContentKey, strings.Repeat("z", 64)),
		"duplicate-digest":    metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2, controlplane.ReserveContentKey, strings.Repeat("a", 64), controlplane.ReserveContentKey, strings.Repeat("a", 64)),
		"unknown-mode":        metadata.Pairs(controlplane.ReserveModeKey, "replay-v3"),
		"duplicate-mode":      metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV2, controlplane.ReserveModeKey, controlplane.ReserveReplayV1),
		"v1-with-digest":      metadata.Pairs(controlplane.ReserveModeKey, controlplane.ReserveReplayV1, controlplane.ReserveContentKey, strings.Repeat("a", 64)),
		"no-mode-with-digest": metadata.Pairs(controlplane.ReserveContentKey, strings.Repeat("a", 64)),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), md)
			_, err := svc.Reserve(ctx, &pb.ReserveRequest{})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
