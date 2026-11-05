package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type sample struct {
	id, code, failure      string
	valid                  bool
	lat, scheduledLat, lag float64
	firstBodyMS            float64
}

type latencySummary struct {
	Count  int     `json:"count"`
	MeanMS float64 `json:"mean_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
}

func latencyByOutcome(samples []sample) (*latencySummary, *latencySummary) {
	var success, failure []float64
	for _, s := range samples {
		if s.valid {
			success = append(success, s.lat)
		} else {
			failure = append(failure, s.lat)
		}
	}
	summarize := func(values []float64) *latencySummary {
		if len(values) == 0 {
			return nil
		}
		sort.Float64s(values)
		var total float64
		for _, v := range values {
			total += v
		}
		n := len(values)
		return &latencySummary{Count: n, MeanMS: total / float64(n), P95MS: values[int(math.Ceil(float64(n)*.95))-1], P99MS: values[int(math.Ceil(float64(n)*.99))-1]}
	}
	return summarize(success), summarize(failure)
}

func main() {
	target := flag.String("url", "", "loopback gateway")
	capN := flag.Int("workers", 1000, "maximum in-flight requests")
	rate := flag.Int("rate", 50, "scheduled arrivals per second")
	seconds := flag.Int("seconds", 10, "duration")
	tenants := flag.Int("tenants", 64, "synthetic accounts")
	trace := flag.String("trace-file", "", "CSV path")
	idPrefix := flag.String("test-id-prefix", "", "synthetic request ID prefix for fault injection")
	stream := flag.Bool("stream", false, "validate a complete SSE stream with usage")
	flag.Parse()
	u, e := url.Parse(*target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || *capN < 1 || *rate < 1 || *seconds < 1 || *tenants < 1 || *tenants > 1024 {
		panic("invalid loopback load configuration")
	}
	body := []byte(`{"model":"test-model","messages":[{"role":"user","content":"hello load"}],"max_tokens":100,"stream":false}`)
	if *stream {
		body = []byte(`{"model":"test-model","messages":[{"role":"user","content":"hello load"}],"max_tokens":100,"stream":true}`)
	}
	tr := &http.Transport{Proxy: nil, MaxIdleConns: *capN, MaxIdleConnsPerHost: *capN, MaxConnsPerHost: *capN, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext}
	defer tr.CloseIdleConnections()
	cli := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	sem := make(chan struct{}, *capN)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var peak atomic.Int64
	var active atomic.Int64
	samples := make([]sample, 0, *rate**seconds)
	dropped := 0
	begin := time.Now()
	runID := begin.UnixNano()
	scheduled := *rate * *seconds
	for n := 0; n < scheduled; n++ {
		due := begin.Add(time.Duration(int64(n) * int64(time.Second) / int64(*rate)))
		if wait := time.Until(due); wait > 0 {
			time.Sleep(wait)
		}
		select {
		case sem <- struct{}{}:
		default:
			dropped++
			continue
		}
		wg.Add(1)
		now := active.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		go func(n int, due time.Time) {
			defer wg.Done()
			defer func() { <-sem; active.Add(-1) }()
			id := fmt.Sprintf("%s%x-%d", *idPrefix, runID, n)
			started := time.Now()
			s := sample{id: "idem:" + id, lag: float64(started.Sub(due)) / 1e6}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", *target+"/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", id)
			req.Header.Set("Authorization", fmt.Sprintf("Bearer sk-isolated-%03d", n%*tenants+1))
			resp, err := cli.Do(req)
			if err != nil {
				s.code = "transport"
				if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
					s.code = "timeout"
				}
			} else {
				reader := bufio.NewReader(resp.Body)
				if _, firstErr := reader.Peek(1); firstErr == nil {
					s.firstBodyMS = float64(time.Since(started)) / 1e6
				}
				b, re := io.ReadAll(io.LimitReader(reader, 1<<20))
				_ = resp.Body.Close()
				s.code = fmt.Sprint(resp.StatusCode)
				if re != nil {
					s.code = "body_error"
				} else if resp.StatusCode != 200 {
					var f struct {
						Error struct {
							Message string `json:"message"`
						} `json:"error"`
					}
					if json.Unmarshal(b, &f) == nil {
						s.failure = f.Error.Message
					} else {
						s.failure = "non_json"
					}
				} else {
					if *stream {
						s.valid = validStream(b)
					} else {
						var obj map[string]any
						s.valid = json.Unmarshal(b, &obj) == nil && obj["usage"] != nil
					}
					if !s.valid {
						s.code = "invalid_200"
					}
				}
			}
			end := time.Now()
			s.lat = float64(end.Sub(started)) / 1e6
			s.scheduledLat = float64(end.Sub(due)) / 1e6
			mu.Lock()
			samples = append(samples, s)
			mu.Unlock()
		}(n, due)
	}
	wg.Wait()
	elapsed := time.Since(begin).Seconds()
	counts := map[string]int{}
	failures := map[string]int{}
	var lats, scheduledLats, lags, firstBodyLats []float64
	var sum float64
	valid := 0
	for _, s := range samples {
		counts[s.code]++
		if s.failure != "" {
			failures[s.failure]++
		}
		if s.valid {
			valid++
			if s.firstBodyMS > 0 {
				firstBodyLats = append(firstBodyLats, s.firstBodyMS)
			}
		}
		lats = append(lats, s.lat)
		scheduledLats = append(scheduledLats, s.scheduledLat)
		lags = append(lags, s.lag)
		sum += s.lat
	}
	for _, a := range [][]float64{lats, scheduledLats, lags, firstBodyLats} {
		sort.Float64s(a)
	}
	pct := func(a []float64, p float64) float64 {
		if len(a) == 0 {
			return 0
		}
		return a[int(math.Ceil(float64(len(a))*p))-1]
	}
	if *trace != "" {
		f, e := os.Create(*trace)
		if e != nil {
			panic(e)
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"request_id", "status", "valid"})
		for _, s := range samples {
			_ = w.Write([]string{s.id, s.code, fmt.Sprint(s.valid)})
		}
		w.Flush()
		if e := w.Error(); e != nil {
			panic(e)
		}
		if e := f.Close(); e != nil {
			panic(e)
		}
	}
	avg := 0.0
	if len(lats) > 0 {
		avg = sum / float64(len(lats))
	}
	successLatency, failureLatency := latencyByOutcome(samples)
	out := map[string]any{"mode": "open_loop", "offered_rate": *rate, "scheduled": scheduled, "started": len(samples), "generator_dropped": dropped, "peak_inflight": peak.Load(), "tenants": *tenants, "requested_seconds": *seconds, "elapsed_seconds": elapsed, "requests": len(samples), "valid_success": valid, "success_per_second_drain": float64(valid) / elapsed, "statuses": counts, "failure_classes": failures, "mean_ms": avg, "p95_ms": pct(lats, .95), "p99_ms": pct(lats, .99), "success_latency_ms": successLatency, "failure_latency_ms": failureLatency, "scheduled_p95_ms": pct(scheduledLats, .95), "schedule_lag_p99_ms": pct(lags, .99)}
	if *stream {
		out["success_first_body_samples"] = len(firstBodyLats)
		out["success_first_body_p95_ms"] = pct(firstBodyLats, .95)
		out["success_first_body_p99_ms"] = pct(firstBodyLats, .99)
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

// The mock emits one JSON object per data line. Count only streams with a
// complete usage object followed by the actual SSE [DONE] sentinel.
func validStream(body []byte) bool {
	usageSeen := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return usageSeen
		}
		var frame struct {
			Usage *struct {
				Prompt     *int64 `json:"prompt_tokens"`
				Completion *int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &frame) != nil {
			return false
		}
		if frame.Usage != nil {
			if frame.Usage.Prompt == nil || frame.Usage.Completion == nil || *frame.Usage.Prompt < 0 || *frame.Usage.Completion < 0 {
				return false
			}
			usageSeen = true
		}
	}
	return false
}
