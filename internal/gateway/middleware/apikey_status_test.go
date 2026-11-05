package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
)

type keyValidatorStub struct {
	response *billingv1.ValidateAPIKeyResponse
	err      error
}

func (s keyValidatorStub) ValidateAPIKey(context.Context, string) (*billingv1.ValidateAPIKeyResponse, error) {
	return s.response, s.err
}

func TestAPIKeyDistinguishesAuthenticationOutage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		stub keyValidatorStub
		want int
	}{
		{"valid", keyValidatorStub{response: &billingv1.ValidateAPIKeyResponse{Valid: true, TenantId: 12, ApiKeyId: 34}}, http.StatusNoContent},
		{"invalid", keyValidatorStub{response: &billingv1.ValidateAPIKeyResponse{}}, http.StatusUnauthorized},
		{"upstream failure", keyValidatorStub{err: errors.New("temporary failure")}, http.StatusServiceUnavailable},
		{"nil response", keyValidatorStub{}, http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(APIKey(tc.stub))
			r.GET("/", func(c *gin.Context) {
				if c.GetInt64("tenant_id") != 12 || c.GetInt64("api_key_id") != 34 {
					t.Error("identity not propagated")
				}
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer sk-synthetic")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d body %s", w.Code, tc.want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "temporary failure") || strings.Contains(w.Body.String(), "sk-synthetic") {
				t.Fatal("response exposed credential or upstream error")
			}
		})
	}
}
