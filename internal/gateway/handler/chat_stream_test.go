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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type fakeBilling struct {
	reserveErr    error
	reserveCode   billingv1.ReserveCode
	reserveNil    bool
	reserveZeroID bool
	reserves      []*billingv1.ReserveRequest
	reportErr     error
	reports       []*billingv1.ReportUsageRequest
	settles       []*billingv1.SettleRequest
	reverses      []*billingv1.ReverseRequest
}

type failUsageFrameWriter struct{ gin.ResponseWriter }

func (w *failUsageFrameWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), `"usage"`) {
		return 0, errors.New("client disconnected while writing usage")
	}
	return w.ResponseWriter.Write(p)
}

func TestRelayStreamReportsUsageReadBeforeClientWriteFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n")
	}))
	defer upstream.Close()
	for _, conservative := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "conservative"}[conservative], func(t *testing.T) {
			bill := &fakeBilling{}
			c, _ := newGinCtx()
			c.Writer = &failUsageFrameWriter{ResponseWriter: c.Writer}
			prov := &client.RouteResult{ProviderID: 7, Name: "up", BaseURL: upstream.URL}
			req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}, Stream: true}
			relayStream(c, bill, &fakeRouter{}, slog.New(slog.DiscardHandler), prov, req, "rid-usage-disconnect", 9, 11, conservative)
			if len(bill.reports) != 1 || bill.reports[0].PromptTokens != 4 || bill.reports[0].CompletionTokens != 2 || bill.reports[0].ProviderId != 7 {
				t.Fatalf("received usage was not reported: %+v", bill.reports)
			}
			if len(bill.settles) != 1 || len(bill.reverses) != 0 {
				t.Fatalf("settles=%d reverses=%d", len(bill.settles), len(bill.reverses))
			}
		})
	}
}

func TestRelayStreamReportsFirstUsageFrameWhenClientWriteFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n")
	}))
	defer upstream.Close()
	bill := &fakeBilling{}
	c, _ := newGinCtx()
	c.Writer = &failUsageFrameWriter{ResponseWriter: c.Writer}
	prov := &client.RouteResult{ProviderID: 7, Name: "up", BaseURL: upstream.URL}
	req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}, Stream: true}
	relayStream(c, bill, &fakeRouter{}, slog.New(slog.DiscardHandler), prov, req, "rid-first-usage-disconnect", 9, 11, true)
	if len(bill.reports) != 1 || bill.reports[0].PromptTokens != 4 || bill.reports[0].CompletionTokens != 2 || len(bill.settles) != 1 || len(bill.reverses) != 0 {
		t.Fatalf("reports=%+v settles=%d reverses=%d", bill.reports, len(bill.settles), len(bill.reverses))
	}
}

func (f *fakeBilling) Reserve(_ context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	f.reserves = append(f.reserves, req)
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	if f.reserveNil {
		return nil, nil
	}
	if f.reserveZeroID {
		return &billingv1.ReserveResponse{Code: f.reserveCode}, nil
	}
	return &billingv1.ReserveResponse{Code: f.reserveCode, BillId: 1}, nil
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
func (f *fakeBilling) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	if usage, ok := controlplane.SettleUsageFrom(ctx); ok {
		f.reports = append(f.reports, &billingv1.ReportUsageRequest{
			RequestId: req.RequestId, BillId: req.BillId,
			PromptTokens: usage.Prompt, CompletionTokens: usage.Completion, ProviderId: usage.Provider,
		})
	}
	if f.reportErr != nil {
		return nil, f.reportErr
	}
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
	if len(bill.reports) != 1 || bill.reports[0].PromptTokens != 11 || bill.reports[0].CompletionTokens != 4 {
		t.Fatalf("usage = %+v, want prompt 11 completion 4", bill.reports)
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

func TestConservativeStatusRefundsPrecharge(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer bad.Close()
	bill := &fakeBilling{}
	c, w := newGinCtx()
	prov := &client.RouteResult{ProviderID: 1, Name: "bad", BaseURL: bad.URL}
	req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}}
	forwardNonStream(c, bill, &fakeRouter{}, slog.New(slog.DiscardHandler), prov, req, "rid-400", 9, 4, time.Second, true)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nope") {
		t.Fatalf("status = %d body=%q, want upstream 400", w.Code, w.Body.String())
	}
	if len(bill.reverses) != 1 || bill.reverses[0].RequestId != "rid-400" {
		t.Fatalf("reverse = %+v", bill.reverses)
	}
	if len(bill.settles) != 0 {
		t.Fatal("rejected request was settled")
	}
}

func TestConservativeDialKeepsPrecharge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()
	bill := &fakeBilling{}
	c, w := newGinCtx()
	prov := &client.RouteResult{ProviderID: 1, Name: "down", BaseURL: "http://" + closed}
	req := &chatRequest{Model: "m", Messages: []chatMessage{{Role: "user", Content: "hi"}}}
	forwardNonStream(c, bill, &fakeRouter{}, slog.New(slog.DiscardHandler), prov, req, "rid-dial", 9, 4, time.Second, true)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", w.Code)
	}
	if len(bill.reverses) != 0 || len(bill.settles) != 0 {
		t.Fatalf("reverses=%d settles=%d, dial failure must stay pending", len(bill.reverses), len(bill.settles))
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
		{"stream_estimate", true, "data: {\"choices\":[{\"delta\":{\"content\":\"你好世界\"}}]}\n\ndata: [DONE]\n\n", 4},
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
			Chat(bill, router, slog.New(slog.DiscardHandler), time.Second, false)(c)
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

func TestChatReserveConflictReturns409(t *testing.T) {
	bill := &fakeBilling{reserveErr: status.Error(codes.FailedPrecondition, "conflict")}
	router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 2, BaseURL: "http://127.0.0.1:1"}}}
	c, w := newGinCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	c.Set("request_id", "conflict")
	Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(bill.reports) != 0 || len(bill.settles) != 0 || len(bill.reverses) != 0 {
		t.Fatal("unexpected billing action")
	}
}

func TestChatRejectsNonAuthorizingReserveCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		code billingv1.ReserveCode
		want int
	}{
		{"duplicate", billingv1.ReserveCode_RESERVE_DUPLICATE, http.StatusConflict},
		{"unknown", billingv1.ReserveCode(99), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bill := &fakeBilling{reserveCode: tc.code}
			router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 2, BaseURL: "http://127.0.0.1:1"}}}
			c, w := newGinCtx()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
			c.Set("request_id", "duplicate")
			Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if len(bill.reports) != 0 || len(bill.settles) != 0 || len(bill.reverses) != 0 {
				t.Fatal("non-authorizing reserve triggered further billing")
			}
		})
	}
}

func TestChatRejectsNilReserveResponse(t *testing.T) {
	bill := &fakeBilling{reserveNil: true}
	router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 2, BaseURL: "http://127.0.0.1:1"}}}
	c, w := newGinCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	c.Set("request_id", "nil")
	Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
	if w.Code != http.StatusServiceUnavailable || len(bill.reverses) != 0 {
		t.Fatalf("status=%d reverses=%d", w.Code, len(bill.reverses))
	}
}

func TestChatRejectsReserveSuccessWithoutBillID(t *testing.T) {
	bill := &fakeBilling{reserveZeroID: true}
	router := &fakeRouter{next: []*client.RouteResult{{ProviderID: 2, BaseURL: "http://127.0.0.1:1"}}}
	c, w := newGinCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	c.Set("request_id", "zero-bill")
	Chat(bill, router, slog.New(slog.DiscardHandler), time.Second)(c)
	if w.Code != http.StatusServiceUnavailable || len(bill.reverses) != 0 {
		t.Fatalf("status=%d reverses=%d", w.Code, len(bill.reverses))
	}
}
