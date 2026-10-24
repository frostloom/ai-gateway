package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
)

func TestHealthcheckDoesNotInitializeServices(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Error("wrong endpoint")
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c := exec.Command(os.Args[0], "-test.run=TestHealthcheckHelper")
	c.Env = append(os.Environ(), "KNOWLEDGE_HEALTH_TEST=1", "AGENT_PORT="+u.Port(), "CONTROL_PLANE_TOKEN=", "MYSQL_DSN=invalid")
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("health mode initialized dependencies: %v %s", e, b)
	}
}
func TestHealthcheckHelper(t *testing.T) {
	if os.Getenv("KNOWLEDGE_HEALTH_TEST") != "1" {
		return
	}
	os.Args = []string{"agent", "-healthcheck"}
	main()
}
