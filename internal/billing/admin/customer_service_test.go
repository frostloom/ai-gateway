package admin

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/supportconfig"
)

func TestCustomerServiceMissingCipherShowsOnlySetupStatus(t *testing.T) {
	t.Setenv(supportconfig.MasterKeyEnv, "")
	t.Setenv("AGENT_LLM_API_KEY", "must-not-appear-llm")
	t.Setenv("JEV_API_KEY", "must-not-appear-jev")
	mux := http.NewServeMux()
	RegisterCustomerService(mux, nil, slog.Default())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/customer-service", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("missing cipher status failed", w.Code)
	}
	var view supportconfig.View
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.EncryptionReady || !view.LLM.APIKeyConfigured || !view.JEV.APIKeyConfigured || strings.Contains(w.Body.String(), "must-not-appear") {
		t.Fatal("secret exposed or setup status incorrect")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/customer-service", strings.NewReader(`{}`)))
	if w.Code != 503 {
		t.Fatal("write without master accepted", w.Code)
	}
}

func TestCustomerServiceRejectsMalformedOrTrailingBodyBeforeStore(t *testing.T) {
	t.Setenv(supportconfig.MasterKeyEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("m", 32))))
	mux := http.NewServeMux()
	RegisterCustomerService(mux, nil, slog.Default())
	for _, body := range []string{`null`, `{`, `{} {}`, strings.Repeat(" ", 33<<10) + `{}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/customer-service", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("malformed body got %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/admin/customer-service", nil))
	if w.Code != 405 {
		t.Fatal("unsupported method accepted")
	}
}
