package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	target := flag.String("url", "", "loopback gateway")
	workers := flag.Int("workers", 1, "workers")
	seconds := flag.Int("seconds", 5, "seconds")
	tenants := flag.Int("tenants", 1, "tenants 1..1024")
	stream := flag.Bool("stream", false, "SSE")
	flag.Parse()
	u, e := url.Parse(*target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || *workers < 1 || *tenants < 1 || *tenants > 1024 {
		panic("isolated loopback target and valid workers/tenants required")
	}
	runID := time.Now().UnixNano()
	tr := &http.Transport{Proxy: nil, MaxIdleConns: *workers * 2, MaxIdleConnsPerHost: *workers, MaxConnsPerHost: *workers, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext}
	defer tr.CloseIdleConnections()
	cli := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	type sample struct {
		lat, first float64
		code       string
		valid      bool
	}
	data := make([][]sample, *workers)
	var wg, ready sync.WaitGroup
	start := make(chan struct{})
	var deadline time.Time
	ready.Add(*workers)
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(w int) {
			body, _ := json.Marshal(map[string]any{"message": "你好，请介绍一下服务", "session_id": fmt.Sprintf("load-%x-%d", runID, w)})
			defer wg.Done()
			ready.Done()
			<-start
			for time.Now().Before(deadline) {
				begin := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				req, _ := http.NewRequestWithContext(ctx, "POST", *target+"/portal/chat", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", fmt.Sprintf("Bearer sk-isolated-%03d", w%*tenants+1))
				resp, err := cli.Do(req)
				s := sample{}
				if err != nil {
					s.code = "transport"
					if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
						s.code = "timeout"
					}
				} else {
					s.first = float64(time.Since(begin)) / float64(time.Millisecond)
					b, re := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
					resp.Body.Close()
					s.code = fmt.Sprint(resp.StatusCode)
					if re != nil {
						s.code = "body_error"
					} else if resp.StatusCode == 200 {
						var obj map[string]any
						e := json.Unmarshal(b, &obj)
						reply, ok := obj["reply"].(string)
						s.valid = e == nil && ok && strings.TrimSpace(reply) != ""
						if !s.valid {
							s.code = "invalid_200"
						}
					}
				}
				cancel()
				s.lat = float64(time.Since(begin)) / float64(time.Millisecond)
				data[w] = append(data[w], s)
			}
		}(w)
	}
	ready.Wait()
	begin := time.Now()
	deadline = begin.Add(time.Duration(*seconds) * time.Second)
	close(start)
	wg.Wait()
	elapsed := time.Since(begin).Seconds()
	counts := map[string]int{}
	var lats, first []float64
	ok := 0
	sum := 0.0
	for _, bucket := range data {
		for _, s := range bucket {
			counts[s.code]++
			lats = append(lats, s.lat)
			sum += s.lat
			if s.valid {
				ok++
				first = append(first, s.first)
			}
		}
	}
	sort.Float64s(lats)
	sort.Float64s(first)
	pct := func(a []float64, p float64) float64 {
		if len(a) == 0 {
			return 0
		}
		return a[int(math.Ceil(float64(len(a))*p))-1]
	}
	total := len(lats)
	avg := 0.0
	if total > 0 {
		avg = sum / float64(total)
	}
	out := map[string]any{"workers": *workers, "tenants": *tenants, "stream": *stream, "kind": "agent", "requested_seconds": *seconds, "elapsed_seconds": elapsed, "requests": total, "valid_success": ok, "success_per_second": float64(ok) / elapsed, "statuses": counts, "mean_ms": avg, "p50_ms": pct(lats, .5), "p95_ms": pct(lats, .95), "p99_ms": pct(lats, .99), "first_response_p95_ms": pct(first, .95)}
	j, _ := json.Marshal(out)
	fmt.Println(string(j))
}
