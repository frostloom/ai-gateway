package knowledgecompare

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundFunc func(*http.Request) (*http.Response, error)

func (f roundFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testContract() Contract {
	return Contract{Schema: "knowledge-answer100.v1", ID: CampaignID, FundingPool: "knowledge-independent", SuiteSHA: strings.Repeat("a", 64), Revision: strings.Repeat("b", 40), CorpusSHA: strings.Repeat("c", 64), MainSHA: strings.Repeat("d", 64), BinarySHA: strings.Repeat("e", 64), PriceSHA: strings.Repeat("f", 64), MaxMicroCNY: 5000000, MaxCalls: 1120, MaxOutput: 512, MaxBytes: 32768, InputCeiling: 1048576, InputRate: 2000000, OutputRate: 8000000}
}
func auth(c Contract) Approval {
	return Approval{Schema: "knowledge-independent-approval.v1", ContractSHA: c.SHA(), FundingPool: c.FundingPool, Approved: true, MaxMicroCNY: c.MaxMicroCNY, ConsentSHA: strings.Repeat("1", 64)}
}
func testGuard(t *testing.T, body string) (*Guard, *int) {
	t.Helper()
	c := testContract()
	cap, e := Acquire(filepath.Join(t.TempDir(), "permanent"), c, auth(c))
	if e != nil {
		t.Fatal(e)
	}
	n := new(int)
	g, e := NewGuard(filepath.Join(t.TempDir(), "events.jsonl"), cap, roundFunc(func(r *http.Request) (*http.Response, error) {
		*n++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { g.Close() })
	return g, n
}

const goodNative = `{"model":"deepseek-flash","choices":[{"message":{"role":"assistant","content":"合成"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_cache_hit_tokens":40,"prompt_cache_miss_tokens":60}}`
const wire = `{"model":"deepseek-flash","max_tokens":512,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"合成"}],"tools":[]}`

func call(g *Guard, turn int) (*http.Response, error) {
	r, _ := http.NewRequestWithContext(WithTurn(context.Background(), "K001-a", turn), "POST", ProviderURL, strings.NewReader(wire))
	return g.RoundTrip(r)
}
func TestIndependentApprovalCannotUseSharedBudget(t *testing.T) {
	c := testContract()
	a := auth(c)
	for _, kind := range []string{"false", "shared", "sha", "money"} {
		x := a
		switch kind {
		case "false":
			x.Approved = false
		case "shared":
			x.FundingPool = "main"
		case "sha":
			x.ContractSHA = strings.Repeat("0", 64)
		case "money":
			x.MaxMicroCNY = 6000000
		}
		p := filepath.Join(t.TempDir(), "claim")
		if _, e := Acquire(p, c, x); e == nil {
			t.Fatal("accepted " + kind)
		}
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatal("rejection wrote claim")
		}
	}
}
func TestPermanentClaimCannotReissue(t *testing.T) {
	c := testContract()
	p := filepath.Join(t.TempDir(), "claim")
	if _, e := Acquire(p, c, auth(c)); e != nil {
		t.Fatal(e)
	}
	c.Revision = strings.Repeat("9", 40)
	if _, e := Acquire(p, c, auth(c)); e == nil {
		t.Fatal("reissued")
	}
}
func TestGuardNativeUsageAndWriteAhead(t *testing.T) {
	g, n := testGuard(t, goodNative)
	r, e := call(g, 1)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	events := g.Events()
	if *n != 1 || len(events) != 2 || events[0].Kind != "intent" || events[0].Hold != 2101248 || events[1].Kind != "usage" || events[1].UpperCost != 360 || events[1].Input != 100 || events[1].CacheHit != 40 {
		t.Fatalf("%+v", events)
	}
}
func TestGuardUnknownUsageStopsWithoutRetry(t *testing.T) {
	g, n := testGuard(t, `{"choices":[]}`)
	if _, e := call(g, 1); e == nil {
		t.Fatal("unknown usage accepted")
	}
	if _, e := call(g, 1); e == nil || *n != 1 {
		t.Fatal("retry after unknown")
	}
	if !g.State().UnknownCharge || g.State().Hold != 2101248 {
		t.Fatal(g.State())
	}
}
func TestGuardIndependentCapAndPerTurnBound(t *testing.T) {
	g, n := testGuard(t, goodNative)
	for i := 0; i < 5; i++ {
		r, e := call(g, 1)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
	}
	if _, e := call(g, 1); e == nil || *n != 5 {
		t.Fatal("sixth per-turn attempt sent")
	}
}
func TestGuardFailedJournalCannotSend(t *testing.T) {
	g, n := testGuard(t, goodNative)
	g.Close()
	if _, e := call(g, 1); e == nil || *n != 0 {
		t.Fatal("sent without journal")
	}
}

func TestGuardMaximumContextCapStopsBeforeThirdRequest(t *testing.T) {
	body := strings.ReplaceAll(goodNative, `"prompt_tokens":100`, `"prompt_tokens":1048576`)
	body = strings.ReplaceAll(body, `"prompt_cache_hit_tokens":40`, `"prompt_cache_hit_tokens":0`)
	body = strings.ReplaceAll(body, `"prompt_cache_miss_tokens":60`, `"prompt_cache_miss_tokens":1048576`)
	g, n := testGuard(t, body)
	for i := 1; i <= 2; i++ {
		r, e := call(g, i)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
	}
	if _, e := call(g, 3); e == nil || *n != 2 {
		t.Fatal("budget exceeded")
	}
}
func TestGuardMalformedAssistantStops(t *testing.T) {
	g, n := testGuard(t, strings.Replace(goodNative, `"role":"assistant"`, `"role":"user"`, 1))
	if _, e := call(g, 1); e == nil {
		t.Fatal("malformed provider accepted")
	}
	if _, e := call(g, 1); e == nil || *n != 1 {
		t.Fatal("continued")
	}
}

func TestGuardHTTPErrorAndEOFNeverRetry(t *testing.T) {
	for _, code := range []int{503, 0} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := testContract()
			cap, e := Acquire(filepath.Join(t.TempDir(), "claim"), c, auth(c))
			if e != nil {
				t.Fatal(e)
			}
			n := 0
			g, e := NewGuard(filepath.Join(t.TempDir(), "events"), cap, roundFunc(func(r *http.Request) (*http.Response, error) {
				n++
				if code == 0 {
					return nil, io.EOF
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(goodNative))}, nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			defer g.Close()
			for i := 0; i < 2; i++ {
				if _, e := call(g, 1); e == nil {
					t.Fatal("accepted failure")
				}
			}
			if n != 1 || !g.State().UnknownCharge || g.State().Hold != 2101248 {
				t.Fatal("retry or lost hold")
			}
		})
	}
}

func TestExplicit20CNYApprovalWithinNewUserCap(t *testing.T) {
	c := testContract()
	c.MaxMicroCNY = 20000000
	if e := c.Validate(); e != nil {
		t.Fatal("user-approved 20CNY cap rejected", e)
	}
	if e := CheckApproval(c, auth(c)); e != nil {
		t.Fatal(e)
	}
	c.MaxMicroCNY = 20000001
	if e := c.Validate(); e == nil {
		t.Fatal("above approved cap accepted")
	}
}
