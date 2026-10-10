package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidRejectsEmptyAndMismatch(t *testing.T) {
	if Valid("", "secret") || Valid("secret", "") || Valid("nope", "secret") {
		t.Fatal("empty or mismatched token was accepted")
	}
	if !Valid("secret", "secret") {
		t.Fatal("matching token was rejected")
	}
}

func TestWrapHTTP(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := WrapHTTP("secret", map[string]struct{}{"/healthz": {}}, next)

	openReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	openRec := httptest.NewRecorder()
	h.ServeHTTP(openRec, openReq)
	if openRec.Code != http.StatusNoContent {
		t.Fatalf("healthz = %d, want 204", openRec.Code)
	}

	denied := httptest.NewRequest(http.MethodPost, "/admin/providers", nil)
	deniedRec := httptest.NewRecorder()
	h.ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", deniedRec.Code)
	}

	ok := httptest.NewRequest(http.MethodPost, "/admin/providers", nil)
	ok.Header.Set(Header, "secret")
	okRec := httptest.NewRecorder()
	h.ServeHTTP(okRec, ok)
	if okRec.Code != http.StatusNoContent {
		t.Fatalf("authenticated = %d, want 204", okRec.Code)
	}
}
