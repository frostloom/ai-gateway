package taskbench

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
)

const windowProtocolModelBody = `{"model":"deepseek-flash","max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"thinking":{"type":"disabled"}}`
const windowProtocolJEVBody = `{"model":"jev-1.13.0","state":{"a":1},"questions":{"q":{"type":"noul","instructions":"yes?"}}}`

func windowProtocolLimits(provider string) windowProviderLimits {
	p := windowProviderLimits{Model: "deepseek-flash", MaxAttempts: 10, MaxRequestBytes: 4096, MaxInput: 1000, MaxOutput: 128, InputRate: 2000000, OutputRate: 8000000}
	if provider == "jev" {
		p.Model = "jev-1.13.0"
		p.MaxOutput = 100
	}
	return p
}
func windowProtocolIdentity(provider string, body []byte) windowRequestIdentity {
	return windowRequestIdentity{Provider: provider, Model: windowProtocolLimits(provider).Model, BodySHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Bytes: len(body)}
}

// Catches bypassing serialized body/model/budget/endpoint validation.
func TestWindowProtocolPOST(t *testing.T) {
	for _, tc := range []struct{ provider, body, endpoint string }{{"model", windowProtocolModelBody, "https://api.deepseek.com/anthropic/v1/messages"}, {"jev", windowProtocolJEVBody, "https://api.typesafe.ai/v1/systemone"}} {
		t.Run(tc.provider+"/valid", func(t *testing.T) {
			if e := validateWindowPOST(tc.provider, "POST", tc.endpoint, []byte(tc.body), windowProtocolLimits(tc.provider), windowProtocolIdentity(tc.provider, []byte(tc.body))); e != nil {
				t.Fatal(e)
			}
		})
		for _, endpoint := range []string{"http://api.deepseek.com/anthropic/v1/messages", tc.endpoint + "?", tc.endpoint + "?x=1", tc.endpoint + "#x", tc.endpoint + "/", strings.Replace(tc.endpoint, "https://", "https://user@", 1), strings.Replace(tc.endpoint, "https://", "https://evil.example/", 1), strings.Replace(tc.endpoint, ".com/", ".com:443/", 1), strings.Replace(tc.endpoint, ".ai/", ".ai:443/", 1)} {
			if endpoint == tc.endpoint {
				continue
			}
			t.Run(tc.provider+"/endpoint/"+endpoint, func(t *testing.T) {
				if validateWindowPOST(tc.provider, "POST", endpoint, []byte(tc.body), windowProtocolLimits(tc.provider), windowProtocolIdentity(tc.provider, []byte(tc.body))) == nil {
					t.Fatal("unsafe endpoint accepted")
				}
			})
		}
		for _, method := range []string{"GET", "post", "PUT"} {
			if validateWindowPOST(tc.provider, method, tc.endpoint, []byte(tc.body), windowProtocolLimits(tc.provider), windowProtocolIdentity(tc.provider, []byte(tc.body))) == nil {
				t.Error("non-POST accepted")
			}
		}
		for _, body := range []string{strings.Replace(tc.body, `"model":`, `"model":"wrong","model":`, 1), strings.Replace(tc.body, windowProtocolLimits(tc.provider).Model, "wrong", 1), tc.body + "{}", `null`, `[]`, strings.Replace(tc.body, `{`, `{"unknown":1,`, 1), strings.Replace(tc.body, `{`, `{"stream":true,`, 1), strings.Replace(tc.body, `{`, `{"stream":null,`, 1)} {
			t.Run(tc.provider+"/bad-body/"+body, func(t *testing.T) {
				if validateWindowPOST(tc.provider, "POST", tc.endpoint, []byte(body), windowProtocolLimits(tc.provider), windowProtocolIdentity(tc.provider, []byte(body))) == nil {
					t.Fatal("bad body accepted")
				}
			})
		}
		for _, mutate := range []func(*windowRequestIdentity){func(r *windowRequestIdentity) { r.Bytes++ }, func(r *windowRequestIdentity) { r.BodySHA256 = strings.Repeat("0", 64) }, func(r *windowRequestIdentity) { r.Model = "wrong" }, func(r *windowRequestIdentity) { r.Provider = "wrong" }} {
			r := windowProtocolIdentity(tc.provider, []byte(tc.body))
			mutate(&r)
			if validateWindowPOST(tc.provider, "POST", tc.endpoint, []byte(tc.body), windowProtocolLimits(tc.provider), r) == nil {
				t.Error("wrong identity accepted")
			}
		}
		p := windowProtocolLimits(tc.provider)
		p.MaxRequestBytes = len(tc.body) - 1
		if validateWindowPOST(tc.provider, "POST", tc.endpoint, []byte(tc.body), p, windowProtocolIdentity(tc.provider, []byte(tc.body))) == nil {
			t.Error("oversized body accepted")
		}
	}
	for _, body := range []string{strings.Replace(windowProtocolModelBody, `128`, `129`, 1), strings.Replace(windowProtocolModelBody, `128`, `128.0`, 1), strings.Replace(windowProtocolModelBody, `128`, `null`, 1), strings.Replace(windowProtocolModelBody, `"max_tokens":128,`, ``, 1), strings.Replace(windowProtocolModelBody, `"disabled"`, `"enabled"`, 1), strings.Replace(windowProtocolModelBody, `"thinking":{"type":"disabled"}`, `"thinking":null`, 1), strings.Replace(windowProtocolModelBody, `"type":"text"`, `"type":"text","type":"text"`, 1), strings.Replace(windowProtocolModelBody, `"messages":[{`, `"messages":[],"discarded":[{`, 1)} {
		if validateWindowPOST("model", "POST", "https://api.deepseek.com/anthropic/v1/messages", []byte(body), windowProtocolLimits("model"), windowProtocolIdentity("model", []byte(body))) == nil {
			t.Errorf("bad model body accepted: %s", body)
		}
	}
}

// Catches charging just Anthropic input_tokens, omitting separately counted cache read/create.
func TestWindowProtocolUsage(t *testing.T) {
	for _, tc := range []struct {
		provider, usage          string
		input, output, hit, miss int64
		cache                    bool
	}{
		{"model", `{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":30,"cache_creation_input_tokens":20}`, 60, 2, 30, 30, true},
		{"model", `{"input_tokens":10,"output_tokens":2}`, 10, 2, 0, 10, true},
		{"model", `{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":1000}`, 1000, 0, 1000, 0, true},
		{"jev", `{"input_tokens":120,"output_tokens":12}`, 120, 12, 0, 0, false},
	} {
		t.Run(tc.provider+tc.usage, func(t *testing.T) {
			u, e := parseWindowResponseUsage(tc.provider, []byte(`{"model":"`+windowProtocolLimits(tc.provider).Model+`","usage":`+tc.usage+`}`), windowProtocolLimits(tc.provider))
			if e != nil {
				t.Fatal(e)
			}
			if u == nil || u.Input == nil || u.Output == nil || *u.Input != tc.input || *u.Output != tc.output {
				t.Fatalf("bad total: %+v", u)
			}
			if tc.cache && (u.CacheHit == nil || u.CacheMiss == nil || *u.CacheHit != tc.hit || *u.CacheMiss != tc.miss) {
				t.Fatalf("bad cache: %+v", u)
			}
			if !tc.cache && (u.CacheHit != nil || u.CacheMiss != nil) {
				t.Fatal("invented JEV cache")
			}
		})
	}
}

// Catches treating absent/null/float/negative/overflow/duplicate usage as known spend.
func TestWindowProtocolRejectUnknownUsage(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		usages := []string{`null`, `[]`, `{}`, `{"input_tokens":1}`, `{"input_tokens":null,"output_tokens":1}`, `{"input_tokens":1,"output_tokens":null}`, `{"input_tokens":1.0,"output_tokens":1}`, `{"input_tokens":1e0,"output_tokens":1}`, `{"input_tokens":"1","output_tokens":1}`, `{"input_tokens":-1,"output_tokens":1}`, `{"input_tokens":-0,"output_tokens":1}`, `{"input_tokens":1,"output_tokens":-1}`, `{"input_tokens":9223372036854775808,"output_tokens":1}`, `{"input_tokens":1001,"output_tokens":1}`, `{"input_tokens":1,"output_tokens":129}`, `{"input_tokens":1,"input_tokens":1,"output_tokens":1}`, `{"input_tokens":1,"output_tokens":1,"output_\u0074okens":1}`, `{"input_tokens":1,"output_tokens":1,"unknown_tokens":1}`}
		if provider == "model" {
			usages = append(usages, `{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":null}`, `{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":-1}`, `{"input_tokens":999,"output_tokens":1,"cache_read_input_tokens":2}`, `{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":9223372036854775807}`, `{"input_tokens":1,"output_tokens":1,"cache_creation":{"ephemeral_5m_input_tokens":10}}`)
		} else {
			usages = append(usages, `{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":1}`)
		}
		for _, usage := range usages {
			t.Run(provider+usage, func(t *testing.T) {
				if _, e := parseWindowResponseUsage(provider, []byte(`{"model":"`+windowProtocolLimits(provider).Model+`","usage":`+usage+`}`), windowProtocolLimits(provider)); e == nil {
					t.Fatal("unknown usage accepted")
				}
			})
		}
		for _, body := range []string{`{}`, `null`, `{"model":null,"usage":{"input_tokens":1,"output_tokens":1}}`, `{"model":"wrong","usage":{"input_tokens":1,"output_tokens":1}}`, `{"model":"` + windowProtocolLimits(provider).Model + `","usage":{"input_tokens":1,"output_tokens":1},"error":{"type":"x"}}`, `{"model":"` + windowProtocolLimits(provider).Model + `","usage":{"input_tokens":1,"output_tokens":1}}{}`, `{"model":"` + windowProtocolLimits(provider).Model + `","usage":{"input_tokens":1,"output_tokens":1},"content":{"x":1,"x":2}}`} {
			if _, e := parseWindowResponseUsage(provider, []byte(body), windowProtocolLimits(provider)); e == nil {
				t.Errorf("unknown body accepted: %s", body)
			}
		}
	}
	if _, e := parseWindowResponseUsage("unknown", []byte(`{}`), windowProtocolLimits("model")); e == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, e := parseWindowResponseUsage("model", bytes.Repeat([]byte(" "), (1<<20)+1), windowProtocolLimits("model")); e == nil {
		t.Fatal("oversized response accepted")
	}
}

type windowProtocolBrokenReader struct{}

func (windowProtocolBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestWindowProtocolBoundedRead(t *testing.T) {
	b, e := readWindowResponse(strings.NewReader("abc"))
	if e != nil || string(b) != "abc" {
		t.Fatalf("small read: %q %v", b, e)
	}
	b, e = readWindowResponse(bytes.NewReader(bytes.Repeat([]byte("a"), 1<<20)))
	if e != nil || len(b) != 1<<20 {
		t.Fatal("exact max read failed")
	}
	if _, e = readWindowResponse(bytes.NewReader(bytes.Repeat([]byte("a"), (1<<20)+1))); e == nil {
		t.Fatal("over max accepted")
	}
	if _, e = readWindowResponse(windowProtocolBrokenReader{}); e == nil {
		t.Fatal("read error accepted")
	}
}

// Catches a parser accepting a model declaration that its POST guard rejects.
func TestWindowProtocolDeclarationMismatch(t *testing.T) {
	p := windowProtocolLimits("model")
	p.MaxOutput = 513
	if _, e := parseWindowResponseUsage("model", []byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1}}`), p); e == nil {
		t.Fatal("model output declaration above 512 accepted")
	}
	p = windowProtocolLimits("model")
	p.MaxInput = 10000001
	if validateWindowPOST("model", "POST", "https://api.deepseek.com/anthropic/v1/messages", []byte(windowProtocolModelBody), p, windowProtocolIdentity("model", []byte(windowProtocolModelBody))) == nil {
		t.Fatal("input declaration beyond accounting bound accepted")
	}
}
func TestWindowProtocolJSONBoundaries(t *testing.T) {
	for _, body := range [][]byte{append([]byte(`{"model":"deepseek-flash","x":"`), 0xff), []byte(`{"x":` + strings.Repeat("[", 130) + `0` + strings.Repeat("]", 130) + `}`), []byte(`{"model":"deepseek-flash","model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1}}`)} {
		if _, e := parseWindowResponseUsage("model", body, windowProtocolLimits("model")); e == nil {
			t.Fatal("invalid response accepted")
		}
	}
	body := strings.Replace(windowProtocolModelBody, `"max_tokens":128`, `"max_tokens":128,"stream":false`, 1)
	if e := validateWindowPOST("model", "POST", "https://api.deepseek.com/anthropic/v1/messages", []byte(body), windowProtocolLimits("model"), windowProtocolIdentity("model", []byte(body))); e != nil {
		t.Fatal(e)
	}
	if _, e := readWindowResponse(nil); e == nil {
		t.Fatal("nil reader accepted")
	}
}
