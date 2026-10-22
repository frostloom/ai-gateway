package jev

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewEvaluationClient uses an explicit transport, fixed model and no redirects.
// It does not discover proxy settings or retry unknown-charge failures.
func NewEvaluationClient(endpoint, apiKey, model string, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback())) || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" || transport == nil {
		return nil, fmt.Errorf("explicit evaluation endpoint/key/model/transport required")
	}
	return &Client{endpoint: endpoint, apiKey: apiKey, model: model, cli: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
