package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type fakeBilling struct {
	reserves  []*billingv1.ReserveRequest
	reportErr error
	reports   []*billingv1.ReportUsageRequest
	settles   []*billingv1.SettleRequest
	reverses  []*billingv1.ReverseRequest
}

func (f *fakeBilling) Reserve(_ context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	f.reserves = append(f.reserves, req)
	return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: 1}, nil
}
func (f *fakeBilling) ReportUsage(_ context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error) {
	f.reports = append(f.reports, req)
	return &billingv1.ReportUsageResponse{Accepted: f.reportErr == nil}, f.reportErr
}

func TestChatLeavesPendingWhenActualUsageCannotBeRecorded(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer good.Close()
	bill := &fakeBilling{reportErr: errors.New("usage persistence unavailable")}
	router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 2, BaseURL: good.URL}}}
	c, w := newGinCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	c.Set("request_id", "rid-report-failed")
	Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "completion_tokens") {
		t.Fatalf("delivered response: status=%d body=%q", w.Code, w.Body.String())
	}
	if len(bill.reports) != 1 || bill.reports[0].ProviderId != 2 {
		t.Fatalf("usage report = %+v", bill.reports)
	}
	if len(bill.settles) != 0 || len(bill.reverses) != 0 {
		t.Fatalf("usage failure must retain pending reserve: settles=%d reverses=%d", len(bill.settles), len(bill.reverses))
	}
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

func TestChatReportsSuccessfulFallbackProvider(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stream         bool
		body           string
		wantCompletion int64
	}{
		{"nonstream", false, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`, 2},
		{"stream_usage", true, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n", 2},
		{"stream_estimate", true, "data: {\"choices\":[{\"delta\":{\"content\":\"你好世界\"}}]}\n\ndata: [DONE]\n\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer bad.Close()
			good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer good.Close()
			bill := &fakeBilling{}
			router := &fakeRouter{next: []*client.RouteResult{
				{ProviderID: 1, BaseURL: bad.URL}, {ProviderID: 2, BaseURL: good.URL},
			}}
			c, w := newGinCtx()
			stream := "false"
			if tc.stream {
				stream = "true"
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":`+stream+`}`))
			c.Set("request_id", "rid-fallback")
			Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%q", w.Code, w.Body.String())
			}
			if len(bill.reserves) != 1 || bill.reserves[0].ProviderId != 1 {
				t.Fatalf("initial reserve = %+v", bill.reserves)
			}
			if len(bill.reports) != 1 {
				t.Fatalf("reports = %+v", bill.reports)
			}
			if got := bill.reports[0]; got.ProviderId != 2 || got.CompletionTokens != tc.wantCompletion {
				t.Fatalf("actual usage = %+v, want provider 2 completion %d", got, tc.wantCompletion)
			}
			if len(bill.settles) != 1 || len(bill.reverses) != 0 {
				t.Fatalf("settles=%d reverses=%d", len(bill.settles), len(bill.reverses))
			}
		})
	}
}

func TestRelayStreamDoesNotWaitForFailedResponseBody(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "refund", true: "failover"}[fallback], func(t *testing.T) {
			bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.(http.Flusher).Flush()
				<-r.Context().Done() // Headers arrive, but the error body never finishes.
			}))
			defer bad.Close()
			bill := &fakeBilling{}
			router := &fakeRouter{}
			if fallback {
				good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
				}))
				defer good.Close()
				router.next = []*client.RouteResult{{ProviderID: 2, BaseURL: good.URL}}
			}
			c, w := newGinCtx()
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				relayStream(c, bill, router, slog.New(slog.DiscardHandler),
					&client.RouteResult{ProviderID: 1, BaseURL: bad.URL},
					&chatRequest{Model: "m", Stream: true}, "rid-open-error", 9, 4)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				cancel()
				<-done
				t.Fatal("stream waited for a 503 body instead of failing over or refunding")
			}
			if fallback {
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "[DONE]") || len(bill.settles) != 1 || len(bill.reverses) != 0 {
					t.Fatalf("fallback result: status=%d body=%q settles=%d reverses=%d", w.Code, w.Body.String(), len(bill.settles), len(bill.reverses))
				}
			} else if w.Code != http.StatusBadGateway || len(bill.reverses) != 1 || len(bill.reports) != 0 {
				t.Fatalf("refund result: status=%d reverses=%d reports=%d", w.Code, len(bill.reverses), len(bill.reports))
			}
		})
	}
}
