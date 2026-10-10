package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouteHidesUpstreamKeyWithoutToken(t *testing.T) {
	reg := New(nil, slog.New(slog.DiscardHandler))
	reg.cache["m"] = &cachedRoute{
		providers: []Provider{{
			ID: 1, Name: "p", BaseURL: "http://up", UpstreamKey: "sk-secret", Weight: 1,
		}},
		expireAt: time.Now().Add(time.Hour),
	}
	h := NewHTTPHandler(reg, "secret", slog.New(slog.DiscardHandler))

	denied := httptest.NewRequest(http.MethodGet, "/route?model=m", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, denied)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("unauthenticated /route leaked key: %s", rec.Body.String())
	}

	ok := httptest.NewRequest(http.MethodGet, "/route?model=m", nil)
	ok.Header.Set("X-Control-Token", "secret")
	okRec := httptest.NewRecorder()
	h.ServeHTTP(okRec, ok)
	if okRec.Code != http.StatusOK || !strings.Contains(okRec.Body.String(), "sk-secret") {
		t.Fatalf("authed route = %d %s", okRec.Code, okRec.Body.String())
	}

	report := httptest.NewRequest(http.MethodPost, "/report", bytes.NewReader([]byte(`{"provider_id":1,"ok":false}`)))
	reportRec := httptest.NewRecorder()
	h.ServeHTTP(reportRec, report)
	if reportRec.Code != http.StatusUnauthorized {
		t.Fatalf("report status = %d, want 401", reportRec.Code)
	}
	if reg.breaker(1).consecutiveFail != 0 {
		t.Fatal("unauthenticated /report changed breaker state")
	}
}
