package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRoutableModelsRequiresExplicitConfiguredAndPricedModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Control-Token") != "tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/admin/providers":
			_, _ = w.Write([]byte(`{"items":[
				{"name":"off","status":1,"models":"[\"hidden\"]"},
				{"name":"deepseek-official","status":0,"weight":1,"base_url":"https://example.invalid","models":"[\"deepseek-flash\",\"*\"]"}
			]}`))
		case "/portal/models":
			_, _ = w.Write([]byte(`{"items":[{"model_id":"deepseek-flash"},{"model_id":"catalog-only"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	got, err := listRoutableModels(t.Context(), srv.Client(), srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "deepseek-flash" || got[0].OwnedBy != "deepseek-official" {
		t.Fatalf("models = %+v", got)
	}
}
