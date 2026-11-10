package admin

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoBillingDisabledByDefault(t *testing.T) {
	t.Setenv("ENABLE_DEMO_BILLING", "")
	h := Handler(nil, slog.Default(), "test")
	for _, path := range []string{"/admin/recharge", "/admin/recharge/pay", "/admin/subscribe", "/admin/change-plan"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("X-Control-Token", "test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s got %d want 403", path, w.Code)
		}
	}
}

func TestPaidOperationsClosedEvenWithDemoEnabled(t *testing.T) {
	t.Setenv("ENABLE_DEMO_BILLING", "true")
	h := Handler(nil, slog.Default(), "test")
	for _, path := range []string{"/admin/recharge", "/admin/recharge/pay", "/admin/subscribe", "/admin/change-plan"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"amount_money":100}`))
		r.Header.Set("X-Control-Token", "test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s got %d want 403", path, w.Code)
		}
	}
}

func TestModelCreationRequiresExplicitPricing(t *testing.T) {
	h := Handler(nil, slog.Default(), "test")
	for _, body := range []string{
		`{"vendor":"test","model_id":"configured","context_len":4096}`,
		`{"vendor":"test","model_id":"configured","context_len":4096,"input_price_cent":0}`,
		`{"vendor":"test","model_id":"configured","context_len":4096,"input_price_cent":-1,"output_price_cent":0}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/admin/models", strings.NewReader(body))
		r.Header.Set("X-Control-Token", "test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status=%d", w.Code)
		}
	}
}
