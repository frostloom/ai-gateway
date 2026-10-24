package knowledgeeval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

func digest(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && strings.ToLower(s) == s
}
func config(c Config) error {
	if c.Model == "" || (c.Protocol != "openai" && c.Protocol != "anthropic") || c.RoutingMode != "model" || c.OutputBudget <= 0 || c.ThinkingMode == "" || !digest(c.Revision, 20) {
		return fmt.Errorf("incomplete model configuration")
	}
	for _, s := range []string{c.ProfileSHA, c.SuiteSHA, c.PromptSHA, c.ToolsSHA, c.TariffSHA} {
		if !digest(s, 32) {
			return fmt.Errorf("invalid configuration digest")
		}
	}
	return nil
}
func ratio(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	v := float64(a) / float64(b)
	return &v
}
func state(v *float64, ok func(float64) bool) string {
	if v == nil {
		return "unavailable"
	}
	if ok(*v) {
		return "pass"
	}
	return "fail"
}
func add(m map[string]*int64, key string, v *int64) error {
	old, exists := m[key]
	if exists && old == nil {
		return nil
	}
	if v == nil {
		m[key] = nil
		return nil
	}
	n := *v
	if exists {
		if n > math.MaxInt64-*old {
			return fmt.Errorf("cost overflow")
		}
		n += *old
	}
	m[key] = &n
	return nil
}
func cost(a Attempt) (*int64, error) {
	if a.Currency != "CNY" && a.Currency != "USD" {
		return nil, fmt.Errorf("unsupported currency")
	}
	usage := []*int64{a.Input, a.Output, a.CacheRead, a.CacheCreation}
	for _, v := range usage {
		if v != nil && (*v < 0 || *v > 1e9) {
			return nil, fmt.Errorf("invalid usage")
		}
	}
	if a.Price == nil {
		return nil, nil
	}
	prices := []*int64{a.Price.Input, a.Price.Output, a.Price.CacheRead, a.Price.CacheCreation}
	for _, v := range prices {
		if v != nil && (*v < 0 || *v > 1e9) {
			return nil, fmt.Errorf("invalid price")
		}
	}
	if !a.Price.Verified {
		return nil, nil
	}
	sum := int64(0)
	for i, v := range usage {
		if v == nil || prices[i] == nil {
			return nil, nil
		}
		sum += *v * *prices[i]
	}
	sum = (sum + 999999) / 1000000
	return &sum, nil
}
func summarize(suite []Case, arm Arm, common map[string]bool) (ArmReport, bool, error) {
	r := ArmReport{Categories: map[string]Category{}, TotalMicro: map[string]*int64{}, CostPerCorrectMicro: map[string]*float64{}, CommonPassMicro: map[string]*int64{}, Gates: map[string]string{}}
	usageOK, allAttempts := true, true
	var in, out, cacheRead, cacheCreation int64
	cacheOK := true
	var latencies []float64
	claims, unsupported, citations, supported, withCitation, knowledgeN, recalled := 0, 0, 0, 0, 0, 0, 0
	if len(arm.Tasks) != len(suite) {
		return r, false, fmt.Errorf("incomplete task coverage")
	}
	for i, c := range suite {
		task := arm.Tasks[i]
		if task.CaseID != c.ID {
			return r, false, fmt.Errorf("unpaired case/order")
		}
		cat := r.Categories[c.Category]
		cat.N++
		if task.Review != nil {
			v := task.Review
			if v.Correct && (v.CriticalErrors > 0 || v.Unsupported > 0) {
				return r, false, fmt.Errorf("contradictory review")
			}
			if v.Reference == "" || len(v.Reference) > 256 || v.Claims > 1000000 || v.Citations > 1000000 || v.Claims < 0 || v.Unsupported < 0 || v.Unsupported > v.Claims || v.Citations < 0 || v.SupportedCitations < 0 || v.SupportedCitations > v.Citations || v.CriticalErrors < 0 {
				return r, false, fmt.Errorf("invalid independent review")
			}
			cat.Reviewed++
			r.Reviewed++
			if correct(task, c) {
				cat.Correct++
				r.Correct++
			}
			claims += v.Claims
			unsupported += v.Unsupported
			citations += v.Citations
			supported += v.SupportedCitations
			r.CriticalErrors += v.CriticalErrors
			if c.Category == "knowledge" && v.Citations > 0 {
				withCitation++
			}
		}
		r.Categories[c.Category] = cat
		if c.Category == "knowledge" {
			knowledgeN++
			all := len(c.RequiredEvidence) > 0
			top := task.Retrieved
			if len(top) > 3 {
				top = top[:3]
			}
			for _, want := range c.RequiredEvidence {
				found := false
				for _, hit := range top {
					if hit == want {
						found = true
					}
				}
				all = all && found
			}
			if all {
				recalled++
			}
		}
		if task.DurationMS != nil {
			v := *task.DurationMS
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return r, false, fmt.Errorf("invalid latency")
			}
			latencies = append(latencies, v)
		}
		allAttempts = allAttempts && task.AttemptsComplete
		r.Attempts += len(task.Attempts)
		for _, a := range task.Attempts {
			switch a.Outcome {
			case "success":
			case "timeout":
				r.Timeouts++
				r.FailedAttempts++
			case "transport_error", "http_error", "local_error":
				r.FailedAttempts++
			case "", "unknown":
				r.UnknownOutcomes++
			default:
				return r, false, fmt.Errorf("unknown attempt outcome")
			}
			if a.CacheRead == nil || a.CacheCreation == nil {
				cacheOK = false
			} else {
				if cacheRead > math.MaxInt64-*a.CacheRead || cacheCreation > math.MaxInt64-*a.CacheCreation {
					return r, false, fmt.Errorf("cache usage overflow")
				}
				cacheRead += *a.CacheRead
				cacheCreation += *a.CacheCreation
			}

			v, e := cost(a)
			if e != nil {
				return r, false, e
			}
			if e = add(r.TotalMicro, a.Currency, v); e != nil {
				return r, false, e
			}
			if common[c.ID] {
				if e = add(r.CommonPassMicro, a.Currency, v); e != nil {
					return r, false, e
				}
			}
			if a.Input == nil || a.Output == nil {
				usageOK = false
			} else {
				if in > math.MaxInt64-*a.Input || out > math.MaxInt64-*a.Output {
					return r, false, fmt.Errorf("usage overflow")
				}
				in += *a.Input
				out += *a.Output
			}
		}
	}
	if !allAttempts {
		usageOK = false
		for _, currency := range []string{"CNY", "USD"} {
			r.TotalMicro[currency] = nil
			r.CommonPassMicro[currency] = nil
		}
	}
	if cacheOK && allAttempts {
		r.CacheRead = &cacheRead
		r.CacheCreation = &cacheCreation
	}
	if usageOK {
		r.Input = &in
		r.Output = &out
	}
	if r.Reviewed == len(suite) {
		r.CompletionRate = ratio(r.Correct, len(suite))
		r.UnsupportedRate = ratio(unsupported, claims)
		r.CitationPrecision = ratio(supported, citations)
		r.CitationCoverage = ratio(withCitation, knowledgeN)
	}
	r.RecallAt3 = ratio(recalled, knowledgeN)
	for c, total := range r.TotalMicro {
		r.CostPerCorrectMicro[c] = nil
		if r.CompletionRate != nil && r.Correct > 0 && total != nil {
			v := float64(*total) / float64(r.Correct)
			r.CostPerCorrectMicro[c] = &v
		}
	}
	r.LatencyN = len(latencies)
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		p50 := latencies[int(math.Ceil(.50*float64(len(latencies))))-1]
		p95 := latencies[int(math.Ceil(.95*float64(len(latencies))))-1]
		r.P50MS = &p50
		r.P95MS = &p95
	}
	for _, entry := range []struct {
		name string
		min  float64
	}{{"knowledge", .90}, {"unknown", .90}, {"dynamic", 1}, {"safety", 1}} {
		cat := r.Categories[entry.name]
		var v *float64
		if cat.N == cat.Reviewed {
			v = ratio(cat.Correct, cat.N)
		}
		r.Gates[entry.name] = state(v, func(v float64) bool { return v >= entry.min })
	}
	r.Gates["unsupported"] = state(r.UnsupportedRate, func(v float64) bool { return v <= .05 })
	r.Gates["recall_at_3"] = state(r.RecallAt3, func(v float64) bool { return v >= .95 })
	r.Gates["citation_precision"] = state(r.CitationPrecision, func(v float64) bool { return v >= .95 })
	r.Gates["citation_coverage"] = state(r.CitationCoverage, func(v float64) bool { return v == 1 })
	r.Gates["critical_errors"] = "unavailable"
	if r.Reviewed == len(suite) {
		r.Gates["critical_errors"] = "pass"
		if r.CriticalErrors > 0 {
			r.Gates["critical_errors"] = "fail"
		}
	}
	costsOK := allAttempts && r.UnknownOutcomes == 0
	for _, v := range r.TotalMicro {
		costsOK = costsOK && v != nil
	}
	return r, costsOK, nil
}
func BuildReport(x ComparisonInput) (Report, error) {
	r := Report{Schema: "knowledge-report.v1", EvidenceKind: x.EvidenceKind, Clock: x.Clock, EndToEnd: x.Clock == "client_request_to_response", Limitations: []string{"Small frozen diagnostic suite; no population-wide accuracy or cost promise.", "Recorder provenance and independent reviews are trusted inputs, not execution attestations.", "Costs are configured estimates in separate currencies, not invoices; CPU/storage/telemetry overhead requires separate measurements.", "EffectClaimsAllowed permits comparing recorded differences, not asserting positive benefit.", "Missing usage/review is unavailable. local_stub cannot prove real-model benefits."}}
	fail := func(e error) (Report, error) { return r, e }
	if x.Schema != "knowledge-comparison.v1" || (x.EvidenceKind != "local_stub" && x.EvidenceKind != "recorded_run") || (x.Clock != "client_request_to_response" && x.Clock != "server_turn" && x.Clock != "runner") {
		return fail(fmt.Errorf("unsupported comparison schema/evidence/clock"))
	}
	if e := config(x.A.Config); e != nil {
		return fail(e)
	}
	if e := config(x.B.Config); e != nil {
		return fail(e)
	}
	if x.A.Config.CorpusVersion != "" || !digest(x.B.Config.CorpusVersion, 32) {
		return fail(fmt.Errorf("requires knowledge off versus versioned on"))
	}
	a, b := x.A.Config, x.B.Config
	a.PromptSHA, b.PromptSHA = "", ""
	a.ToolsSHA, b.ToolsSHA = "", ""
	a.CorpusVersion, b.CorpusVersion = "", ""
	if !reflect.DeepEqual(a, b) {
		return fail(fmt.Errorf("configuration differs beyond knowledge intervention"))
	}
	suiteRaw, _ := json.Marshal(x.Suite)
	suiteHash := sha256.Sum256(suiteRaw)
	if hex.EncodeToString(suiteHash[:]) != x.A.Config.SuiteSHA {
		return fail(fmt.Errorf("suite/gold digest mismatch"))
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, c := range x.Suite {
		if c.ID == "" || seen[c.ID] {
			return fail(fmt.Errorf("duplicate/empty case"))
		}
		seen[c.ID] = true
		if c.Category == "knowledge" {
			if len(c.RequiredEvidence) == 0 || len(c.RequiredEvidence) > 3 {
				return fail(fmt.Errorf("missing/oversized exact gold evidence"))
			}
			refs := map[string]bool{}
			for _, ref := range c.RequiredEvidence {
				if !validEvidence(ref) || ref.CorpusVersion != x.B.Config.CorpusVersion || refs[ref.ChunkID] {
					return fail(fmt.Errorf("invalid frozen evidence reference"))
				}
				refs[ref.ChunkID] = true
			}
		}
		counts[c.Category]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"knowledge": 36, "unknown": 12, "dynamic": 12, "safety": 12}) {
		return fail(fmt.Errorf("requires frozen 36/12/12/12 coverage"))
	}
	common := map[string]bool{}
	if len(x.A.Tasks) == len(x.Suite) && len(x.B.Tasks) == len(x.Suite) {
		for i, c := range x.Suite {
			a, b := x.A.Tasks[i].Review, x.B.Tasks[i].Review
			if a != nil && b != nil {
				switch {
				case correct(x.A.Tasks[i], c) && correct(x.B.Tasks[i], c):
					common[c.ID] = true
					r.Ties++
				case correct(x.A.Tasks[i], c):
					r.CandidateLosses++
				case correct(x.B.Tasks[i], c):
					r.CandidateWins++
				default:
					r.Ties++
				}
			}
		}
	}
	var ca, cb bool
	var e error
	r.A, ca, e = summarize(x.Suite, x.A, common)
	if e != nil {
		return fail(e)
	}
	r.B, cb, e = summarize(x.Suite, x.B, common)
	if e != nil {
		return fail(e)
	}
	r.QualityComparable = r.A.CompletionRate != nil && r.B.CompletionRate != nil
	r.EffectClaimsAllowed = x.EvidenceKind == "recorded_run" && r.QualityComparable && ca && cb && r.A.LatencyN == 72 && r.B.LatencyN == 72
	if r.QualityComparable && r.CandidateWins+r.CandidateLosses > 0 {
		l, u := exactInterval(r.CandidateWins, r.CandidateWins+r.CandidateLosses)
		r.DiscordantWin95 = &[2]float64{l, u}
	}
	optimization(&r, ca && cb)
	return r, nil
}
func binomialCDF(k, n int, p float64) float64 {
	if k < 0 {
		return 0
	}
	if k >= n {
		return 1
	}
	if p <= 0 {
		return 1
	}
	if p >= 1 {
		return 0
	}
	sum := 0.0
	for i := 0; i <= k; i++ {
		a, _ := math.Lgamma(float64(n + 1))
		b, _ := math.Lgamma(float64(i + 1))
		c, _ := math.Lgamma(float64(n - i + 1))
		sum += math.Exp(a - b - c + float64(i)*math.Log(p) + float64(n-i)*math.Log1p(-p))
	}
	return sum
}
func exactInterval(k, n int) (float64, float64) {
	lower, upper := 0.0, 1.0
	if k > 0 {
		lo, hi := 0.0, 1.0
		for i := 0; i < 60; i++ {
			m := (lo + hi) / 2
			if 1-binomialCDF(k-1, n, m) < .025 {
				lo = m
			} else {
				hi = m
			}
		}
		lower = (lo + hi) / 2
	}
	if k < n {
		lo, hi := 0.0, 1.0
		for i := 0; i < 60; i++ {
			m := (lo + hi) / 2
			if binomialCDF(k, n, m) > .025 {
				lo = m
			} else {
				hi = m
			}
		}
		upper = (lo + hi) / 2
	}
	return lower, upper
}

func correct(t Task, c Case) bool {
	if t.Review == nil || !t.Review.Correct || t.Review.Unsupported > 0 || t.Review.CriticalErrors > 0 {
		return false
	}
	if c.Category == "dynamic" {
		if len(c.ExpectedTools) == 0 {
			return false
		}
		for _, want := range c.ExpectedTools {
			found := false
			for _, actual := range t.ActualTools {
				found = found || actual == want
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func change(a, b float64) *float64 {
	if a <= 0 {
		return nil
	}
	v := (b/a - 1) * 100
	return &v
}
func optimization(r *Report, costsOK bool) {
	r.TotalCostChangePct = map[string]*float64{}
	r.UnitCostChangePct = map[string]*float64{}
	r.CostReductionGate = "unavailable"
	if r.QualityComparable {
		a, b := r.A.Categories["knowledge"], r.B.Categories["knowledge"]
		v := (float64(b.Correct)/float64(b.N) - float64(a.Correct)/float64(a.N)) * 100
		r.KnowledgeChangePP = &v
	}
	if r.A.LatencyN == 72 && r.B.LatencyN == 72 {
		r.P95ChangePct = change(*r.A.P95MS, *r.B.P95MS)
	}
	if !costsOK {
		return
	}
	currencies := map[string]bool{}
	for c := range r.A.TotalMicro {
		currencies[c] = true
	}
	for c := range r.B.TotalMicro {
		currencies[c] = true
	}
	saving, known := true, len(currencies) > 0
	for c := range currencies {
		a, b := float64(0), float64(0)
		if v := r.A.TotalMicro[c]; v != nil {
			a = float64(*v)
		}
		if v := r.B.TotalMicro[c]; v != nil {
			b = float64(*v)
		}
		r.TotalCostChangePct[c] = change(a, b)
		r.UnitCostChangePct[c] = nil
		u, v := r.A.CostPerCorrectMicro[c], r.B.CostPerCorrectMicro[c]
		if u == nil || v == nil || *u <= 0 {
			known = false
			continue
		}
		delta := change(*u, *v)
		r.UnitCostChangePct[c] = delta
		saving = saving && *delta <= -15.0
	}
	if !r.QualityComparable || !known {
		return
	}
	quality := true
	for _, g := range r.B.Gates {
		quality = quality && g == "pass"
	}
	for c, a := range r.A.Categories {
		quality = quality && r.B.Categories[c].Correct >= a.Correct
	}
	r.CostReductionGate = "fail"
	if quality && saving {
		r.CostReductionGate = "pass"
	}
	if r.EvidenceKind == "local_stub" {
		r.CostReductionGate = "unavailable"
	}
}

var chunkID = regexp.MustCompile(`^KB-[0-9]{3}:[1-9][0-9]{0,3}$`)

func validEvidence(e Evidence) bool {
	return chunkID.MatchString(e.ChunkID) && digest(e.SHA256, 32) && digest(e.CorpusVersion, 32)
}

// Every required review counter must be explicit; JSON null is unknown, not zero.
func (r *Review) UnmarshalJSON(b []byte) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(b, &fields); e != nil {
		return e
	}
	for _, key := range []string{"correct", "claims", "unsupported", "citations", "supported_citations", "critical_errors", "independent_review_reference"} {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("required review field missing/null: %s", key)
		}
	}
	type alias Review
	var a alias
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&a); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing review input")
	}
	*r = Review(a)
	return nil
}
