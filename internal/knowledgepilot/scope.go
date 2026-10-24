package knowledgepilot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

func SHA(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func digest(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && strings.ToLower(s) == s
}

// StrictDecode rejects duplicates, unknown fields, trailing JSON and absent/null
// non-pointer fields, including nested objects. Pointer fields explicitly allow null.
func StrictDecode(b []byte, v any) error {
	if len(b) > 8<<20 || !utf8.Valid(b) {
		return fmt.Errorf("invalid JSON size/encoding")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if e := uniqueJSON(d); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	return required(b, reflect.TypeOf(v).Elem())
}
func uniqueJSON(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	x, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if x == '{' {
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[s] = true
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
	} else if x == '[' {
		for d.More() {
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
	} else {
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, e = d.Token()
	return e
}
func required(b []byte, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return fmt.Errorf("null required field")
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var m map[string]json.RawMessage
	if e := json.Unmarshal(b, &m); e != nil {
		return e
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		raw, ok := m[name]
		if !ok {
			if opts == "omitempty" {
				continue
			}
			return fmt.Errorf("missing %s", name)
		}
		if e := required(raw, f.Type); e != nil {
			return fmt.Errorf("%s: %w", name, e)
		}
	}
	return nil
}
func CheckScope(b, billing, source []byte) (CheckResult, error) {
	var s Scope
	var r CheckResult
	if e := StrictDecode(b, &s); e != nil {
		return r, e
	}
	if s.Schema != "knowledge-control-scope.v1" || s.ID != ScopeID || !digest(s.Revision, 20) || s.Model != "deepseek-flash" || s.Protocol != "openai" || s.Endpoint != "https://api.deepseek.com/v1" || s.Limits.PerTask != 5 || s.Limits.Total != 720 || s.Limits.Output != 512 || s.Limits.RequestBytes < 1 || s.Limits.RequestBytes > 1<<20 {
		return r, fmt.Errorf("unbound experiment configuration")
	}
	for _, h := range []string{s.SuiteSHA, s.SuiteFileSHA, s.ProfileSHA, s.OrderSHA, s.CorpusVersion} {
		if !digest(h, 32) {
			return r, fmt.Errorf("invalid input digest")
		}
	}
	if s.A.RoutingMode != "model" || s.B.RoutingMode != "model" || s.A.KnowledgeEnabled || !s.B.KnowledgeEnabled || s.A.CorpusVersion != "" || s.B.CorpusVersion != s.CorpusVersion {
		return r, fmt.Errorf("invalid arm modes")
	}
	if s.ReservedCNYMicro != nil && *s.ReservedCNYMicro <= 0 {
		return r, fmt.Errorf("invalid reservation")
	}
	// No billing schema can grant execution in this build. Do not silently accept
	// an unrelated historical attestation as new verified evidence.
	if len(billing) != 0 || len(source) != 0 {
		return r, fmt.Errorf("billing integration unavailable; no paid attestation accepted")
	}
	r = CheckResult{Scope: s, Mode: "check", Blockers: []string{BudgetBlocker, "billing_evidence_unverified", "execution_authorization_unbound"}}
	return r, nil
}
func BuildOrder(c []knowledgeeval.Case) []ScheduledTask {
	var out []ScheduledTask
	for i, x := range c {
		arms := []string{"a", "b"}
		if i%2 == 1 {
			arms = []string{"b", "a"}
		}
		for _, a := range arms {
			out = append(out, ScheduledTask{x.ID, a, x.ID + "-" + a})
		}
	}
	return out
}
func ReadBounded(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("input unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if e != nil || len(b) > 8<<20 {
		return nil, fmt.Errorf("input exceeds bound")
	}
	return b, nil
}
func ArmMetadata(s Scope, c *knowledge.Corpus) (agent.RuntimeMetadata, agent.RuntimeMetadata, error) {
	cl, e := agent.NewEvaluationLLMClient(s.Endpoint, "", s.Model, s.Protocol, &http.Client{Transport: denyNetwork{}, Timeout: time.Second}, s.Limits.Output)
	if e != nil {
		return agent.RuntimeMetadata{}, agent.RuntimeMetadata{}, e
	}
	tb := agent.NewToolbox(nil)
	h := agent.NewHandler(nil, tb, cl, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e = h.SetRoutingMode("model"); e != nil {
		return agent.RuntimeMetadata{}, agent.RuntimeMetadata{}, e
	}
	a := *h.EvaluationRuntimeMetadata()
	if e = tb.SetKnowledge(c); e != nil {
		return a, a, e
	}
	return a, *h.EvaluationRuntimeMetadata(), nil
}
func LoadInputs(root string, s Scope) (Inputs, error) {
	var in Inputs
	in.Scope = s
	read := func(p string) ([]byte, error) { return ReadBounded(filepath.Join(root, p)) }
	mb, e := read("eval/knowledge/acceptance-v1/manifest.json")
	if e != nil {
		return in, e
	}
	var m struct {
		Schema    string            `json:"schema"`
		Counts    map[string]int    `json:"counts"`
		Canonical string            `json:"suite_canonical_sha256"`
		Files     map[string]string `json:"files"`
	}
	if e = StrictDecode(mb, &m); e != nil {
		return in, e
	}
	if m.Schema != "knowledge-acceptance-manifest.v1" || m.Canonical != s.SuiteSHA || m.Files["questions.json"] != s.SuiteFileSHA || len(m.Files) != 3 {
		return in, fmt.Errorf("suite manifest differs")
	}
	for p, h := range m.Files {
		if p != "questions.json" && p != "rubric.md" && p != "README.md" {
			return in, fmt.Errorf("invalid manifest path")
		}
		b, e := read("eval/knowledge/acceptance-v1/" + p)
		if e != nil || SHA(b) != h {
			return in, fmt.Errorf("frozen file differs")
		}
	}
	raw, e := read("eval/knowledge/acceptance-v1/questions.json")
	if e != nil {
		return in, e
	}
	var suite struct {
		Schema string               `json:"schema"`
		Cases  []knowledgeeval.Case `json:"cases"`
	}
	if e = StrictDecode(raw, &suite); e != nil {
		return in, e
	}
	canon, _ := json.Marshal(suite.Cases)
	if suite.Schema != "knowledge-acceptance.v1" || SHA(canon) != s.SuiteSHA || len(suite.Cases) != 72 {
		return in, fmt.Errorf("canonical suite differs")
	}
	in.Cases = suite.Cases
	pb, e := read("eval/knowledge/control-v1/profiles.json")
	if e != nil || SHA(pb) != s.ProfileSHA {
		return in, fmt.Errorf("profiles differ")
	}
	if e = StrictDecode(pb, &in.Profiles); e != nil {
		return in, e
	}
	if len(in.Profiles) != 72 {
		return in, fmt.Errorf("profile count differs")
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, x := range in.Cases {
		if seen[x.ID] {
			return in, fmt.Errorf("duplicate case")
		}
		seen[x.ID] = true
		counts[x.Category]++
		p, ok := in.Profiles[x.ID]
		want := "none"
		if x.ID == "S011" {
			want = "recharge_100"
		}
		if !ok || p.Pending != want || p.BalanceCents != 2500 || p.Subscription != "active_seeded" {
			return in, fmt.Errorf("invalid initial state")
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"knowledge": 36, "unknown": 12, "dynamic": 12, "safety": 12}) || !reflect.DeepEqual(counts, m.Counts) {
		return in, fmt.Errorf("category counts differ")
	}
	ob, e := read("eval/knowledge/control-v1/order.json")
	if e != nil || SHA(ob) != s.OrderSHA {
		return in, fmt.Errorf("order differs")
	}
	var order []ScheduledTask
	if e = StrictDecode(ob, &order); e != nil || !reflect.DeepEqual(order, BuildOrder(in.Cases)) {
		return in, fmt.Errorf("alternating order differs")
	}
	in.Corpus, e = knowledge.Load(os.DirFS(filepath.Join(root, "knowledge/platform")))
	if e != nil {
		return in, e
	}
	if in.Corpus.Version() != s.CorpusVersion {
		return in, fmt.Errorf("corpus differs")
	}
	a, b, e := ArmMetadata(s, in.Corpus)
	if e != nil {
		return in, e
	}
	if !reflect.DeepEqual(a, s.A) || !reflect.DeepEqual(b, s.B) {
		return in, fmt.Errorf("actual handler identities differ")
	}
	return in, nil
}

// Metadata preparation must never contact an upstream.
type denyNetwork struct{}

func (denyNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("metadata-only transport")
}
