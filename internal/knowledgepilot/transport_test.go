package knowledgepilot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestClaimRejectsUncoordinatedLegacyWriters(t *testing.T) {
	_, s := template(t)
	root := t.TempDir()
	c, e := AcquireClaim(root, s, nil)
	if e == nil || c != nil || !strings.Contains(e.Error(), BudgetBlocker) {
		t.Fatalf("%v %v", c, e)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("refusal created paid state")
	}
}
func TestClaimCrossWorktreeExclusive(t *testing.T) {
	_, s := template(t)
	path := filepath.Join(t.TempDir(), "local.claim")
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := OpenLocalClaim(path, "http://127.0.0.1:1", s)
			if e == nil {
				mu.Lock()
				n++
				mu.Unlock()
				if c == nil {
					t.Error("nil claim")
				}
			}
		}()
	}
	wg.Wait()
	if n != 1 {
		t.Fatal(n)
	}
}
func TestClaimLatestCumulativeBudget(t *testing.T) {
	_, s := template(t)
	v := int64(1)
	s.ReservedCNYMicro = &v
	s.AllowPaid = true
	s.HumanAuthorized = true
	if _, e := AcquireClaim(t.TempDir(), s, []ClaimDependency{{Path: "missing", SHA256: strings.Repeat("0", 64), ReservedCNYMicro: 1}}); e == nil {
		t.Fatal("local scope cannot allocate remaining real budget")
	}
}
func TestClaimRejectsMissingChangedDependency(t *testing.T) {
	_, s := template(t)
	for _, p := range []string{"missing", "changed"} {
		if _, e := AcquireClaim(t.TempDir(), s, []ClaimDependency{{Path: p, SHA256: strings.Repeat("a", 64)}}); e == nil {
			t.Fatal("dependency bypass")
		}
	}
}
func TestClaimNotReissuedAfterFailureOrNewOutput(t *testing.T) {
	_, s := template(t)
	path := filepath.Join(t.TempDir(), "claim")
	if _, e := OpenLocalClaim(path, "http://127.0.0.1:1", s); e != nil {
		t.Fatal(e)
	}
	s.Revision = strings.Repeat("f", 40)
	if _, e := OpenLocalClaim(path, "http://127.0.0.1:2", s); e == nil {
		t.Fatal("reissued local identity")
	}
	for _, u := range []string{"https://api.deepseek.com/v1", "http://localhost:80", "http://127.0.0.1.example.com:80", "http://user:password@127.0.0.1"} {
		if _, e := OpenLocalClaim(filepath.Join(t.TempDir(), "claim"), u, s); e == nil {
			t.Fatal("accepted nonliteral-loopback or credentials", u)
		}
	}
}
func transportKit(t *testing.T, reply string) (*Transport, *Journal, *int) {
	t.Helper()
	n := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { *n++; io.WriteString(w, reply) }))
	t.Cleanup(srv.Close)
	_, s := template(t)
	c, e := OpenLocalClaim(filepath.Join(t.TempDir(), "claim"), srv.URL, s)
	if e != nil {
		t.Fatal(e)
	}
	j, e := OpenJournal(filepath.Join(t.TempDir(), "journal.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { j.Close() })
	tr := NewTransport(http.DefaultTransport, c, s.Limits, j)
	return tr, j, n
}
func send(tr *Transport, body string) (*http.Response, error) {
	r, _ := http.NewRequestWithContext(WithTask(context.Background(), "K001-a"), "POST", tr.claim.endpoint+"/chat/completions", strings.NewReader(body))
	return tr.RoundTrip(r)
}

const requestBody = `{"model":"deepseek-flash","max_tokens":512,"messages":[{"role":"user","content":"合成测试"}]}`
const goodResponse = `{"model":"local-stub","choices":[{"message":{"role":"assistant","content":"SYNTHETIC"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`

func TestTransportStopsAfterUnknownUsage(t *testing.T) {
	tr, j, n := transportKit(t, `{"choices":[]}`)
	if _, e := send(tr, requestBody); e == nil {
		t.Fatal("unknown usage accepted")
	}
	if _, e := send(tr, requestBody); e == nil || *n != 1 {
		t.Fatal("retried stopped attempt")
	}
	events := j.Events()
	if len(events) != 2 || events[0].Kind != "reserved" || events[1].Kind != "failed" {
		t.Fatal(events)
	}
}
func TestTransportCrashNeverResends(t *testing.T) {
	tr, j, _ := transportKit(t, goodResponse)
	if e := j.Append(Event{Kind: "reserved", TaskID: "interrupted"}); e != nil {
		t.Fatal(e)
	}
	path := j.path
	j.Close()
	if _, e := OpenJournal(path); e == nil {
		t.Fatal("existing journal reopened")
	}
	if _, e := send(tr, requestBody); e == nil {
		t.Fatal("sent with closed durable journal")
	}
}
func TestTransportRejectsOversizeBeforeSend(t *testing.T) {
	tr, _, n := transportKit(t, goodResponse)
	for _, b := range []string{strings.Repeat("x", 65537), strings.Replace(requestBody, "512", "513", 1), strings.Replace(requestBody, "deepseek-flash", "other", 1)} {
		if _, e := send(tr, b); e == nil || *n != 0 {
			t.Fatal("invalid request sent")
		}
	}
}
func TestTransportCacheNormalizationAndFees(t *testing.T) {
	tr, j, n := transportKit(t, goodResponse)
	resp, e := send(tr, requestBody)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	ev := j.Events()
	a := ev[1].Attempt
	if a == nil || *a.Input != 60 || *a.CacheRead != 40 || *a.CacheCreation != 0 || *a.Output != 20 || a.Price != nil {
		t.Fatalf("cache usage or unverified prices fabricated: %+v", a)
	}
	if *n != 1 {
		t.Fatal(*n)
	}
	b, _ := os.ReadFile(j.path)
	var x map[string]any
	if e = json.Unmarshal([]byte(strings.Split(string(b), "\n")[0]), &x); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "Authorization") {
		t.Fatal("header persisted")
	}
}
func TestTransportTaskAndTotalBounds(t *testing.T) {
	tr, _, n := transportKit(t, goodResponse)
	for i := 0; i < 5; i++ {
		r, e := send(tr, requestBody)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
	}
	if _, e := send(tr, requestBody); e == nil || *n != 5 {
		t.Fatal("sixth attempt sent")
	}
}
