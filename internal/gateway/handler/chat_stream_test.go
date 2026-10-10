package handler

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type fakeBilling struct {
	reports  []*billingv1.ReportUsageRequest
	settles  []*billingv1.SettleRequest
	reverses []*billingv1.ReverseRequest
}

func (f *fakeBilling) Reserve(context.Context, *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: 1}, nil
}
func (f *fakeBilling) ReportUsage(_ context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error) {
	f.reports = append(f.reports, req)
	return &billingv1.ReportUsageResponse{}, nil
}
func (f *fakeBilling) Settle(_ context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	f.settles = append(f.settles, req)
	return &billingv1.SettleResponse{Settled: true}, nil
}
func (f *fakeBilling) Reverse(_ context.Context, req *billingv1.ReverseRequest) (*billingv1.ReverseResponse, error) {
	f.reverses = append(f.reverses, req)
	return &billingv1.ReverseResponse{Reversed: true}, nil
}

type fakeRouter struct {
	next   []*client.RouteResult
	failed []uint64
}

func (f *fakeRouter) Route(context.Context, string, ...uint64) (*client.RouteResult, error) {
	if len(f.next) == 0 {
		return nil, nil
	}
	n := f.next[0]
	f.next = f.next[1:]
	return n, nil
}
func (f *fakeRouter) Report(_ context.Context, id uint64, ok bool) {
	if !ok {
		f.failed = append(f.failed, id)
	}
}

func newGinCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, w
}

func TestRelayStreamSettlesForwardedContentWithoutUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好世界\"}}]}\n\n")
	}))
	defer upstream.Close()

	bill := &fakeBilling{}
	c, w := newGinCtx()
	prov := &client.RouteResult{ProviderID: 7, Name: "up", BaseURL: upstream.URL}
	req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}, Stream: true}
	relayStream(c, bill, &fakeRouter{}, slog.New(slog.DiscardHandler), prov, req, "rid-est", 9, 11)

	if len(bill.reverses) != 0 {
		t.Fatalf("reversed %d times, want 0", len(bill.reverses))
	}
	if len(bill.reports) != 1 || bill.reports[0].PromptTokens != 11 || bill.reports[0].CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want prompt 11 completion 2", bill.reports)
	}
	if len(bill.settles) != 1 || bill.settles[0].RequestId != "rid-est" {
		t.Fatalf("settle = %+v", bill.settles)
	}
	if !strings.Contains(w.Body.String(), "你好世界") {
		t.Fatalf("body missing forwarded text: %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestRelayStreamFailoverThen502Reverses(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()

	bill := &fakeBilling{}
	router := &fakeRouter{next: []*client.RouteResult{
		{ProviderID: 2, Name: "down", BaseURL: "http://" + closed},
	}}
	c, w := newGinCtx()
	prov := &client.RouteResult{ProviderID: 1, Name: "bad", BaseURL: bad.URL}
	req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}, Stream: true}
	relayStream(c, bill, router, slog.New(slog.DiscardHandler), prov, req, "rid-fail", 9, 4)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatal("failure response was served as SSE")
	}
	if !strings.Contains(w.Body.String(), "all providers unavailable") {
		t.Fatalf("body = %s", w.Body.String())
	}
	if len(bill.settles) != 0 || len(bill.reports) != 0 {
		t.Fatal("failed stream was billed")
	}
	if len(bill.reverses) != 1 || bill.reverses[0].RequestId != "rid-fail" {
		t.Fatalf("reverse = %+v", bill.reverses)
	}
	if len(router.failed) != 2 || router.failed[0] != 1 || router.failed[1] != 2 {
		t.Fatalf("failed providers = %v, want [1 2]", router.failed)
	}
}
