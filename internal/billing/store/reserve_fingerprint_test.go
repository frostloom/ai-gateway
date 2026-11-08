package store

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"strings"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestReserveReplayFingerprint(t *testing.T) {
	original := &billingv1.ReserveRequest{RequestId: "r", TenantId: 1, ApiKeyId: 2, ProviderId: 3, Model: "m", EstPromptTokens: 4, MaxCompletionTokens: 5}
	fingerprint := reserveFingerprint(original)
	row := &Bill{ReserveFingerprint: &fingerprint, Status: StatusPending}
	if err := verifyReserveReplay(row, original); err != nil {
		t.Fatal(err)
	}
	changes := []func(*billingv1.ReserveRequest){func(r *billingv1.ReserveRequest) { r.TenantId++ }, func(r *billingv1.ReserveRequest) { r.ApiKeyId++ }, func(r *billingv1.ReserveRequest) { r.ProviderId++ }, func(r *billingv1.ReserveRequest) { r.Model += "x" }, func(r *billingv1.ReserveRequest) { r.EstPromptTokens++ }, func(r *billingv1.ReserveRequest) { r.MaxCompletionTokens++ }}
	for i, change := range changes {
		copy := proto.Clone(original).(*billingv1.ReserveRequest)
		change(copy)
		if status.Code(verifyReserveReplay(row, copy)) != codes.FailedPrecondition {
			t.Errorf("change %d accepted", i)
		}
	}
	if status.Code(verifyReserveReplay(&Bill{}, original)) != codes.FailedPrecondition {
		t.Error("legacy replay accepted")
	}
}

func TestReserveReplayRejectsFinalizedOrReported(t *testing.T) {
	req := &billingv1.ReserveRequest{RequestId: "r", TenantId: 1, ApiKeyId: 2, Model: "m"}
	fp := reserveFingerprint(req)
	now := time.Now()
	cases := []struct {
		name, status string
		reported     *time.Time
		want         codes.Code
	}{
		{"pending", "pending", nil, codes.OK},
		{"pending with usage", "pending", &now, codes.FailedPrecondition},
		{"reversed", "reversed", nil, codes.FailedPrecondition},
		{"settled", "settled", &now, codes.FailedPrecondition},
		{"unknown", "unexpected", nil, codes.FailedPrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := &Bill{ReserveFingerprint: &fp, Status: tc.status, UsageReportedAt: tc.reported}
			if got := status.Code(verifyReserveReplay(row, req)); got != tc.want {
				t.Fatalf("status %s want %s", got, tc.want)
			}
		})
	}
}

func TestReserveContentFingerprintVersioning(t *testing.T) {
	req := &billingv1.ReserveRequest{RequestId: "r", TenantId: 1, ApiKeyId: 2, Model: "m", EstPromptTokens: 4}
	ctxA := controlplane.WithReserveContent(context.Background(), strings.Repeat("a", 64))
	ctxB := controlplane.WithReserveContent(context.Background(), strings.Repeat("b", 64))
	fp := reserveFingerprintForContext(ctxA, req)
	row := &Bill{ReserveFingerprint: &fp, Status: StatusPending}
	if err := verifyReserveReplayWithContext(ctxA, row, req); err != nil {
		t.Fatal(err)
	}
	if status.Code(verifyReserveReplayWithContext(ctxB, row, req)) != codes.FailedPrecondition {
		t.Fatal("different content accepted")
	}
	if status.Code(verifyReserveReplay(row, req)) != codes.FailedPrecondition {
		t.Fatal("old contract replayed v2 row")
	}
	old := reserveFingerprint(req)
	legacy := &Bill{ReserveFingerprint: &old, Status: StatusPending}
	if status.Code(verifyReserveReplayWithContext(ctxA, legacy, req)) != codes.FailedPrecondition {
		t.Fatal("v2 replayed unverifiable v1 row")
	}
}
