package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestFastAnswersRequireExplicitFields(t *testing.T) {
	for _, risk := range []string{`{"type":"score","confidence":0.99}`, `{"type":"score","score":null,"confidence":0.99}`, `{"type":"choice","choice":"high","confidence":0.99}`, `{"type":"score","score":0,"confidence":0.99}`} {
		t.Run(risk, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"answers":{"intent":{"type":"choice","choice":"get_balance","confidence":0.99},"legit":{"type":"noul","noul":0.99},"route_shape":{"type":"choice","choice":"single_read","confidence":0.99},"risk":%s}}`, risk)
			}))
			defer srv.Close()
			result, err := jev.NewClient(srv.URL, "test", "test").Evaluate(context.Background(), "test", map[string]jev.Question{"risk": {Type: jev.QScore}})
			if err != nil {
				t.Fatal(err)
			}
			want := risk == `{"type":"score","score":0,"confidence":0.99}`
			if got := validFastAnswers(result); got != want {
				t.Fatalf("explicit field validation=%v want=%v", got, want)
			}
		})
	}
}
