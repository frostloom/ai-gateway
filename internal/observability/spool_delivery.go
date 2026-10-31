package observability

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

// SpoolDeliveryConfig contains only evaluation credentials, kept in memory.
// The selected operator pins JournalRoot for this run and gates the helper on
// a genuine final normal cohort. A closed spool DATA proof alone is not authority.
type SpoolDeliveryConfig struct{ Endpoint, PublicKey, SecretKey, JournalRoot, BatchSHA256 string }
type SpoolDeliveryResult struct {
	Schema     string `json:"schema"`
	Batch      uint64 `json:"batch"`
	BodySHA256 string `json:"body_sha256"`
	Outcome    string `json:"outcome"` // acknowledged is only HTTP/protobuf ACK, never remote proof
	Attempts   int    `json:"attempts"`
}
type deliveryBody struct {
	mu      sync.Mutex
	reader  *bytes.Reader
	sealed  bool
	exposed int64
}

func newDeliveryBody(raw []byte) *deliveryBody { return &deliveryBody{reader: bytes.NewReader(raw)} }
func (b *deliveryBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sealed {
		return 0, errors.New("delivery body sealed")
	}
	n, e := b.reader.Read(p)
	b.exposed += int64(n)
	return n, e
}
func (b *deliveryBody) Close() error { b.seal(); return nil }
func (b *deliveryBody) seal() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sealed = true
	return b.exposed == 0
}

// DeliverSpoolBatch sends the exact original protobuf once. Only a sealed zero
// byte read with no HTTP status and a live parent permits a fresh pre-body try.
// Journal existence is a permanent prohibition on a second invocation, including
// after a crash or unknown result. Subsequent investigation is GET-only.
func DeliverSpoolBatch(ctx context.Context, v *VerifiedDurableSpool, index uint64, c SpoolDeliveryConfig) (SpoolDeliveryResult, error) {
	return deliverSpoolBatch(ctx, v, index, c, func() http.RoundTripper {
		return &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	}, func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

type deliveryJournal struct {
	root, dir *os.File
	chain     []spoolAncestor
	name      string
}

func openDeliveryJournal(path string, index uint64) (*deliveryJournal, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("delivery journal canonical root required")
	}
	parentChain, e := spoolAncestors(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	if e = os.Mkdir(path, 0700); e != nil && !os.IsExist(e) {
		return nil, e
	}
	parent, e := openSpoolDir(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	e = parent.Sync()
	closeErr := parent.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = checkSpoolAncestors(parentChain); e != nil {
		return nil, e
	}
	chain, e := spoolAncestors(path)
	if e != nil {
		return nil, e
	}
	root, e := openSpoolDir(path)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*deliveryJournal, error) { root.Close(); return nil, e }
	st, e := root.Stat()
	if e != nil || st.Mode().Perm() != 0700 {
		return fail(errors.New("private delivery journal required"))
	}
	if e = checkSpoolRoot(root, chain); e != nil {
		return fail(e)
	}
	name := fmt.Sprintf("batch-%020d", index)
	if e = unix.Mkdirat(int(root.Fd()), name, 0700); e != nil {
		return fail(errors.New("delivery batch already attempted or journal unavailable"))
	}
	if e = root.Sync(); e != nil {
		return fail(e)
	}
	fd, e := unix.Openat(int(root.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return fail(e)
	}
	j := &deliveryJournal{root: root, dir: os.NewFile(uintptr(fd), name), chain: chain, name: name}
	if e = j.check(); e != nil {
		j.close()
		return nil, e
	}
	return j, nil
}
func (j *deliveryJournal) check() error {
	if e := checkSpoolRoot(j.root, j.chain); e != nil {
		return e
	}
	var a, b unix.Stat_t
	if unix.Fstat(int(j.dir.Fd()), &a) != nil || unix.Fstatat(int(j.root.Fd()), j.name, &b, unix.AT_SYMLINK_NOFOLLOW) != nil || a.Dev != b.Dev || a.Ino != b.Ino || b.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("delivery journal replaced")
	}
	return nil
}
func (j *deliveryJournal) write(name string, value any) error {
	if e := j.check(); e != nil {
		return e
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	fd, e := unix.Openat(int(j.dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), name)
	n, e := f.Write(raw)
	if e == nil && n != len(raw) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = j.dir.Sync()
	}
	if e == nil {
		e = j.check()
	}
	return e
}
func (j *deliveryJournal) close() error {
	a := j.dir.Close()
	b := j.root.Close()
	if a != nil {
		return a
	}
	return b
}
func deliverSpoolBatch(ctx context.Context, v *VerifiedDurableSpool, index uint64, c SpoolDeliveryConfig, factory func() http.RoundTripper, wait func(context.Context, time.Duration) error) (result SpoolDeliveryResult, err error) {
	result = SpoolDeliveryResult{"durable-otel.delivery-result.v1", index, c.BatchSHA256, "unknown", 0}
	raw, e := v.ReadBatch(ctx, index)
	if e != nil {
		return result, e
	}
	if spoolDigest(raw) != c.BatchSHA256 {
		return result, errors.New("selected original batch SHA mismatch")
	}
	endpoint, e := url.Parse(c.Endpoint)
	if e != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/api/public/otel/v1/traces" || endpoint.Host == "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (net.ParseIP(endpoint.Hostname()).IsLoopback() || endpoint.Hostname() == "localhost"))) || c.PublicKey == "" || c.SecretKey == "" || strings.ContainsAny(c.PublicKey+c.SecretKey, "\r\n:") {
		return result, errors.New("restricted evaluation endpoint/settings required")
	}
	j, e := openDeliveryJournal(c.JournalRoot, index)
	if e != nil {
		return result, e
	}
	defer func() {
		if e := j.close(); err == nil && e != nil {
			err = errors.New("delivery journal close failed")
		}
	}()
	if e = j.write("intent.json", map[string]any{"schema": "durable-otel.delivery-intent.v1", "run_id": v.summary.RunID, "root_sha256": v.summary.RootSHA256, "batch": index, "body_sha256": c.BatchSHA256, "bytes": len(raw), "endpoint_sha256": spoolDigest([]byte(c.Endpoint)), "project_sha256": spoolDigest([]byte(c.PublicKey)), "remote_verified": false}); e != nil {
		return result, errors.New("delivery intent persistence failed")
	}
	for attempt := 1; attempt <= 5; attempt++ {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		fresh, e := v.ReadBatch(ctx, index)
		if e != nil || !bytes.Equal(fresh, raw) {
			return result, errors.New("original batch changed before send")
		}
		if e = j.write(fmt.Sprintf("attempt-%d.json", attempt), map[string]any{"attempt": attempt, "body_sha256": c.BatchSHA256, "state": "pending"}); e != nil {
			return result, errors.New("delivery attempt persistence failed")
		}
		result.Attempts = attempt
		child, cancel := context.WithTimeout(ctx, 30*time.Second)
		body := newDeliveryBody(raw)
		request, e := http.NewRequestWithContext(child, http.MethodPost, c.Endpoint, body)
		if e != nil {
			cancel()
			return result, errors.New("delivery request rejected")
		}
		request.ContentLength = int64(len(raw))
		request.Close = true
		request.GetBody = nil
		request.SetBasicAuth(c.PublicKey, c.SecretKey)
		request.Header.Set("Content-Type", "application/x-protobuf")
		request.Header.Set("Accept", "application/x-protobuf")
		request.Header.Set("x-langfuse-ingestion-version", "4")
		transport := factory()
		response, sendErr := transport.RoundTrip(request)
		zero := body.seal()
		status := 0
		ack := false
		if response != nil {
			status = response.StatusCode
			if response.Body != nil {
				reply, readErr := io.ReadAll(io.LimitReader(response.Body, durableBatchLimit+1))
				closeErr := response.Body.Close()
				var decoded collector.ExportTraceServiceResponse
				ack = ctx.Err() == nil && sendErr == nil && status == 200 && readErr == nil && closeErr == nil && len(reply) <= durableBatchLimit && proto.Unmarshal(reply, &decoded) == nil && (decoded.PartialSuccess == nil || (decoded.PartialSuccess.RejectedSpans == 0 && decoded.PartialSuccess.ErrorMessage == ""))
			}
		}
		cancel()
		if t, ok := transport.(interface{ CloseIdleConnections() }); ok {
			t.CloseIdleConnections()
		}
		retry := sendErr != nil && response == nil && zero && ctx.Err() == nil && attempt < 5
		outcome := "unknown"
		if ack {
			outcome = "acknowledged"
		} else if zero && response == nil && ctx.Err() == nil {
			outcome = "unsent"
		}
		result.Outcome = outcome
		if e = j.write(fmt.Sprintf("result-%d.json", attempt), map[string]any{"attempt": attempt, "body_sha256": c.BatchSHA256, "sealed_zero_read": zero, "http_status": status, "outcome": outcome, "retry_permitted": retry, "remote_verified": false}); e != nil {
			return result, errors.New("delivery result persistence failed; preserve pending")
		}
		if ack {
			if e = j.write("result.json", result); e != nil {
				return result, errors.New("delivery final persistence failed")
			}
			return result, nil
		}
		if !retry {
			if e = j.write("result.json", result); e != nil {
				return result, errors.New("delivery final persistence failed")
			}
			return result, ErrSpoolDeliveryUnacknowledged
		}
		if e = wait(ctx, time.Duration(100<<(attempt-1))*time.Millisecond); e != nil {
			return result, e
		}
	}
	return result, errors.New("delivery attempt bound")
}

// ErrSpoolDeliveryUnacknowledged means a complete immutable result was retained;
// that original body must never be sent again. Other errors may leave pending.
var ErrSpoolDeliveryUnacknowledged = errors.New("delivery not acknowledged; no resend")

type SpoolDeliveryBatch struct {
	Batch      uint64 `json:"batch"`
	BodySHA256 string `json:"body_sha256"`
}
type SpoolDeliveryRangeResult struct {
	Schema      string                `json:"schema"`
	Results     []SpoolDeliveryResult `json:"results"`
	Pending     []SpoolDeliveryBatch  `json:"pending"`
	Unattempted []SpoolDeliveryBatch  `json:"unattempted"`
	StopReason  string                `json:"stop_reason"`
}

// DeliverSpoolRange reuses one already verified closed DATA proof for at most
// 128 contiguous original batches. It never combines bodies or parallelizes
// sends. Unknown terminal outcomes permit only disjoint first sends. Expiry or
// an incomplete journal stops before the untouched suffix, which remains named.
func DeliverSpoolRange(ctx context.Context, v *VerifiedDurableSpool, batches []SpoolDeliveryBatch, c SpoolDeliveryConfig) (SpoolDeliveryRangeResult, error) {
	result := SpoolDeliveryRangeResult{Schema: "durable-otel.delivery-range-result.v1", Results: []SpoolDeliveryResult{}, Pending: []SpoolDeliveryBatch{}, Unattempted: append([]SpoolDeliveryBatch{}, batches...)}
	if len(batches) == 0 || len(batches) > 128 || c.BatchSHA256 != "" {
		result.StopReason = "invalid_range"
		return result, errors.New("bounded explicit delivery range required")
	}
	for i, b := range batches {
		if b.Batch == 0 || (i > 0 && b.Batch != batches[i-1].Batch+1) {
			result.StopReason = "invalid_range"
			return result, errors.New("ordered contiguous original batches required")
		}
		raw, e := v.ReadBatch(ctx, b.Batch)
		if e != nil || spoolDigest(raw) != b.BodySHA256 {
			result.StopReason = "invalid_range"
			return result, errors.New("range original batch proof mismatch")
		}
		if _, e := os.Lstat(filepath.Join(c.JournalRoot, fmt.Sprintf("batch-%020d", b.Batch))); !os.IsNotExist(e) {
			result.StopReason = "prior_journal"
			return result, errors.New("range contains previously journaled batch")
		}
	}
	for i, b := range batches {
		if e := ctx.Err(); e != nil {
			result.StopReason = "context"
			return result, e
		}
		cfg := c
		cfg.BatchSHA256 = b.BodySHA256
		one, e := DeliverSpoolBatch(ctx, v, b.Batch, cfg)
		if e != nil && !errors.Is(e, ErrSpoolDeliveryUnacknowledged) {
			result.StopReason = "incomplete_batch"
			if _, statErr := os.Lstat(filepath.Join(c.JournalRoot, fmt.Sprintf("batch-%020d", b.Batch))); statErr == nil {
				result.Pending = append(result.Pending, b)
				result.Unattempted = append([]SpoolDeliveryBatch{}, batches[i+1:]...)
			}
			return result, e
		}
		result.Results = append(result.Results, one)
		result.Unattempted = append([]SpoolDeliveryBatch{}, batches[i+1:]...)
	}
	return result, nil
}
