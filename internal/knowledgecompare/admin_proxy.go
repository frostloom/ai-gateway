package knowledgecompare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
)

// This proxy only fronts the owned fixture HTTP listener. Technical failure is
// observed at its source before Handler can send a follow-up provider request.
func adminProxy(target string, g *Guard) *httptest.Server {
	u, _ := url.Parse(target)
	p := httputil.NewSingleHostReverseProxy(u)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	p.Transport = tr
	p.ModifyResponse = func(r *http.Response) error {
		b, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		r.Body.Close()
		if e != nil || len(b) > 1<<20 || r.StatusCode != 200 || !json.Valid(b) {
			g.externalStop("fixture_admin_response")
			return fmt.Errorf("fixture admin failed")
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		return nil
	}
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		g.externalStop("fixture_admin_transport")
		http.Error(w, "fixture admin failure", 502)
	}
	s := httptest.NewUnstartedServer(p)
	s.Config.RegisterOnShutdown(tr.CloseIdleConnections)
	s.Start()
	return s
}
func (g *Guard) externalStop(reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state.Stopped {
		return
	}
	g.state.Stopped = true
	// No provider request is made here; prior settled usage/unknown holds remain.
	if e := g.append(Event{Kind: "external_halt", Failure: reason}); e != nil {
		g.state.UnknownCharge = true
	}
}
