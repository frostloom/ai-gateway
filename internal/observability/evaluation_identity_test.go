package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEvaluationIdentityRequiresInitializedObserver(t *testing.T) {
	for _, o := range []*Observer{nil, {}, {cfg: Config{Enabled: true, Environment: "evaluation", Release: "fake"}}} {
		if got := o.EvaluationIdentity(); got != (EvaluationIdentity{}) {
			t.Fatal("incomplete observer certified", got)
		}
	}
}
func TestEvaluationIdentityIsDetachedRedactedAndReadOnly(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	cfg := Config{Enabled: true, CaptureContent: true, Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "public-marker", SecretKey: "secret-marker", HashKey: strings.Repeat("hash-marker", 4), Release: "release-a", Environment: "evaluation", SampleRate: 1}
	o, e := New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Shutdown(context.Background())
	got := o.EvaluationIdentity()
	digest := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	want := EvaluationIdentity{Enabled: true, CaptureContent: true, SampleRate: 1, Environment: "evaluation", Release: "release-a", ExportTimeoutMS: 2000, QueueSize: 256, EndpointSHA256: digest(cfg.Endpoint), ProjectSHA256: digest(cfg.PublicKey)}
	if got != want {
		t.Fatal(got, want)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{cfg.Endpoint, cfg.PublicKey, cfg.SecretKey, cfg.HashKey} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("configuration material exposed")
		}
	}
	got.Release = "mutated"
	got.Enabled = false
	if o.EvaluationIdentity() != want {
		t.Fatal("returned identity mutated observer")
	}
	if calls.Load() != 0 {
		t.Fatal("accessor performed network I/O", calls.Load())
	}
}
