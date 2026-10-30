package taskbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

// The only public constructor consumes an opaque, freshly checked ready token.
// Settings cannot replace endpoints, caps, sources or authorizations. The
// separately pinned JEV CONNECT route is explicit and limited to local HTTP.
type windowActualSettings struct {
	rawSHA256            string
	ModelKey             string                       `json:"model_key"`
	JEVKey               string                       `json:"jev_key"`
	JEVHTTPSProxy        string                       `json:"jev_https_proxy"`
	LoopbackAdminDSN     string                       `json:"loopback_admin_dsn"`
	AllowFixtureCreation bool                         `json:"allow_fixture_creation"`
	Observer             windowActualObserverSettings `json:"observer"`
}
type windowActualObserverSettings struct {
	Enabled         bool    `json:"enabled"`
	CaptureContent  bool    `json:"capture_content"`
	Endpoint        string  `json:"endpoint"`
	PublicKey       string  `json:"public_key"`
	SecretKey       string  `json:"secret_key"`
	HashKey         string  `json:"hash_key"`
	Release         string  `json:"release"`
	Environment     string  `json:"environment"`
	SampleRate      float64 `json:"sample_rate"`
	QueueSize       int     `json:"queue_size"`
	ExportTimeoutMS int64   `json:"export_timeout_ms"`
}

func (o windowActualObserverSettings) config() observability.Config {
	return observability.Config{Enabled: o.Enabled, CaptureContent: o.CaptureContent, Endpoint: o.Endpoint, PublicKey: o.PublicKey, SecretKey: o.SecretKey, HashKey: o.HashKey, Release: o.Release, Environment: o.Environment, SampleRate: o.SampleRate, QueueSize: o.QueueSize, ExportTimeout: time.Duration(o.ExportTimeoutMS) * time.Millisecond}
}
func windowActualLoopbackEndpoint(endpoint string) (*url.URL, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "/api/public/otel/v1/traces" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return nil, fmt.Errorf("literal loopback observer required")
	}
	return u, nil
}
func readWindowActualSettings(path string) (windowActualSettings, error) {
	return readWindowActualSettingsForTimeout(path, 2000)
}
func readWindowActualSettingsForTimeout(path string, timeoutMS int64) (out windowActualSettings, err error) {
	reject := func() (windowActualSettings, error) {
		return windowActualSettings{}, fmt.Errorf("actual private settings rejected")
	}
	raw, e := readWindowActualPrivateBytes(path, 65536)
	if e != nil || decodeWindowActualJSON(raw, &out) != nil {
		return reject()
	}
	// Require every explicit setting, including values whose zero value is valid.
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	var obs map[string]json.RawMessage
	_ = json.Unmarshal(obj["observer"], &obs)
	if len(obj) != 6 || bytes.Equal(bytes.TrimSpace(obj["jev_https_proxy"]), []byte("null")) || len(obs) != 11 || strings.TrimSpace(out.ModelKey) == "" || strings.TrimSpace(out.JEVKey) == "" || strings.ContainsAny(out.ModelKey+out.JEVKey, "\r\n") || !out.AllowFixtureCreation {
		return reject()
	}
	if _, e = windowActualLoopbackEndpoint(out.Observer.Endpoint); e != nil {
		return reject()
	}
	if _, e = windowActualDatabaseDSN(out.LoopbackAdminDSN); e != nil {
		return reject()
	}
	o := out.Observer
	if !o.Enabled || !o.CaptureContent || o.SampleRate != 1 || o.Environment != "evaluation" || !windowActualRevision(o.Release) || o.ExportTimeoutMS != timeoutMS || o.QueueSize < 1 || o.QueueSize > 4096 || len(o.HashKey) < 32 || strings.TrimSpace(o.PublicKey) == "" || strings.TrimSpace(o.SecretKey) == "" || strings.ContainsAny(o.PublicKey+o.SecretKey, "\r\n:") {
		return reject()
	}
	if _, e = windowActualJEVProxy(out.JEVHTTPSProxy); e != nil {
		return reject()
	}
	out.rawSHA256 = conversationHash(raw)
	return out, nil
}

// Parsing is shared by settings validation and the read-only database probe so
// an invalid service-name or out-of-range port cannot reach the dialer.
// Unknown Params and charset trigger implicit SET SQL in the MySQL driver;
// this fixed Ping preflight rejects them rather than changing the frozen DSN.
func windowActualDatabaseDSN(raw string) (*mysqlDriver.Config, error) {
	reject := func() (*mysqlDriver.Config, error) { return nil, fmt.Errorf("actual loopback database DSN rejected") }
	cfg, e := mysqlDriver.ParseDSN(raw)
	if e != nil || cfg.Net != "tcp" || cfg.DBName != "" || len(cfg.Params) != 0 || strings.Contains(raw, "?charset=") || strings.Contains(raw, "&charset=") {
		return reject()
	}
	host, port, e := net.SplitHostPort(cfg.Addr)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || port == "" {
		return reject()
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return reject()
		}
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return reject()
	}
	return cfg, nil
}

// PingContext verifies connectivity and authentication without selecting a
// database, creating a fixture/schema or issuing any application SQL query.
func checkWindowActualDatabase(ctx context.Context, dsn string) error {
	reject := func() error { return fmt.Errorf("actual database preflight rejected") }
	if ctx == nil || ctx.Err() != nil {
		return reject()
	}
	cfg, e := windowActualDatabaseDSN(dsn)
	if e != nil {
		return e
	}
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 2 * time.Second
	cfg.WriteTimeout = 2 * time.Second
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return reject()
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pingErr := db.PingContext(probe)
	closeErr := db.Close()
	if pingErr != nil || closeErr != nil {
		return reject()
	}
	return nil
}

// Only an explicitly frozen local HTTP CONNECT route is permitted for JEV.
// The target remains the fixed HTTPS provider and retains default TLS checks.
func windowActualJEVProxy(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return nil, fmt.Errorf("actual JEV route rejected")
	}
	port := u.Port()
	if port == "" {
		return nil, fmt.Errorf("actual JEV route rejected")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("actual JEV route rejected")
		}
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("actual JEV route rejected")
	}
	return u, nil
}
func newWindowActualJEVTransport(proxy string) (*http.Transport, error) {
	u, e := windowActualJEVProxy(proxy)
	if e != nil {
		return nil, e
	}
	tr := newWindowActualTransport()
	if u != nil {
		tr.Proxy = http.ProxyURL(u)
	}
	return tr, nil
}

func newWindowActualTransport() *http.Transport {
	return &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 120 * time.Second, ExpectContinueTimeout: time.Second}
}
func checkWindowActualHealth(ctx context.Context, o windowActualObserverSettings) error {
	u, e := windowActualLoopbackEndpoint(o.Endpoint)
	if e != nil {
		return e
	}
	tr := newWindowActualTransport()
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/api/public/health", "/api/public/projects"} {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		target := *u
		target.Path = path
		req, e := http.NewRequestWithContext(checkCtx, http.MethodGet, target.String(), nil)
		if e != nil {
			cancel()
			return fmt.Errorf("actual observer preflight rejected")
		}
		if path == "/api/public/projects" {
			req.SetBasicAuth(o.PublicKey, o.SecretKey)
		}
		resp, e := client.Do(req)
		if e != nil {
			cancel()
			return fmt.Errorf("actual observer preflight rejected")
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 65537))
		closeErr := resp.Body.Close()
		cancel()
		if resp.StatusCode != 200 || readErr != nil || closeErr != nil {
			return fmt.Errorf("actual observer preflight rejected")
		}
	}
	return nil
}

type FullWindowActualStatus struct {
	V8                   *windowV8RuntimeSummary `json:"v8,omitempty"`
	EvidenceKind         string                  `json:"evidence_kind"`
	AcquisitionDone      bool                    `json:"acquisition_done"` // Collection closure does not establish model quality or goal completion.
	Closed               bool                    `json:"closed"`
	NextWindowIndex      int                     `json:"next_window_index"`
	LastReceiptSHA256    string                  `json:"last_receipt_sha256"`
	PlannedPairs         int                     `json:"planned_pairs"`
	PlannedArms          int                     `json:"planned_arms"`
	DefinedTurns         int                     `json:"defined_turns"`
	BoundariesExcluded   int                     `json:"boundaries_excluded"`
	AttemptedArms        int                     `json:"attempted_arms"`
	FinishedArms         int                     `json:"finished_arms"`
	FailedAttemptedArms  int                     `json:"failed_attempted_arms"`
	UnattemptedArms      int                     `json:"unattempted_arms"`
	ObservedTurns        int                     `json:"observed_turns"`
	QualityEndUnrunTurns int                     `json:"quality_end_unrun_turns"`
	GlobalUnrunTurns     int                     `json:"global_unrun_turns"`
	ModelAttempts        int                     `json:"model_attempts"`
	JEVAttempts          int                     `json:"jev_attempts"`
}
type FullWindowActualReceipt struct {
	FullWindowActualStatus
	WindowIndex            int    `json:"window_index"`
	ReceiptSHA256          string `json:"-"`
	ReportSHA256           string `json:"report_sha256"`
	PayloadManifestSHA256  string `json:"payload_manifest_sha256"`
	LedgerSHA256           string `json:"ledger_sha256"`
	FirstUnrunOrdinal      int    `json:"first_unrun_ordinal"`
	FirstUnrunArm          string `json:"first_unrun_arm"`
	FutureUnrunFromOrdinal int    `json:"future_unrun_from_ordinal"`
	ReportStatus           string `json:"report_status"`
}
type FullWindowActualSession struct {
	op           sync.Mutex
	mu           sync.Mutex
	status       FullWindowActualStatus
	ready        *FullWindowActualReady
	auth         windowAcquisitionAuthorization
	campaign     *windowCampaign
	lifetime     context.Context
	cancel       context.CancelFunc
	stopParent   func() bool
	rootInfo     os.FileInfo
	files        map[string]string
	reportRoots  map[string]bool
	reportDirs   map[string]os.FileInfo
	transport    *http.Transport
	jevTransport *http.Transport
	shutdown     bool
	closeErr     error
}

func OpenFullWindowActual(ctx context.Context, ready *FullWindowActualReady, privateSettingsPath string) (_ *FullWindowActualSession, err error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, fmt.Errorf("actual session context rejected")
	}
	// Actual v1 through v6 owners are permanently consumed. New execution must
	// include them in the separately verified v7 budget epoch, even if a caller
	// can still read an old readiness snapshot. No settings or claim first.
	v8 := ready != nil && ready.manifest.Schema == "full-window-actual-ready.v8" && ready.manifest.Scope.Schema == "full-window-scope.v8"
	if v8 && (ready.v8Authority == nil || ready.v8Authority.revalidate() != nil) {
		return nil, fmt.Errorf("actual v7 or signed v8 final authority required before settings")
	}
	if !v8 && (ready == nil || ready.manifest.Schema != "full-window-actual-ready.v7" || ready.manifest.Scope.Schema != "full-window-scope.v7") {
		return nil, fmt.Errorf("actual v7 scope required for current budget epoch")
	}
	if e := ready.revalidateStopped(); e != nil {
		return nil, e
	}
	if e := ready.revalidate(); e != nil {
		return nil, e
	}
	settings, e := readWindowActualSettingsForTimeout(privateSettingsPath, 10000)
	if e != nil {
		return nil, e
	}
	if e = checkWindowActualSettingsDigest(settings, ready.manifest.PrivateSettingsSHA256); e != nil {
		return nil, e
	}
	jevTransport, e := newWindowActualJEVTransport(settings.JEVHTTPSProxy)
	if e != nil {
		return nil, e
	}
	lifetime, cancel := context.WithCancel(ctx)
	s := &FullWindowActualSession{ready: ready, lifetime: lifetime, cancel: cancel, files: map[string]string{}, reportRoots: map[string]bool{}, reportDirs: map[string]os.FileInfo{}, transport: newWindowActualTransport(), jevTransport: jevTransport, status: FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, UnattemptedArms: 7320, DefinedTurns: 11772, GlobalUnrunTurns: 11772, BoundariesExcluded: 1851}}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	observer, e := observability.New(lifetime, settings.Observer.config())
	if e != nil {
		return nil, fmt.Errorf("actual observer rejected")
	}
	s.auth.observer = observer
	if observer.EvaluationIdentity() != ready.manifest.Observer {
		return nil, fmt.Errorf("actual observer identity mismatch")
	}
	if e = checkWindowActualDatabase(lifetime, settings.LoopbackAdminDSN); e != nil {
		return nil, e
	}
	if e = checkWindowActualHealth(lifetime, settings.Observer); e != nil {
		return nil, e
	}
	// Recheck after preflight: roots and all evidence must still be new and frozen.
	if e = ready.revalidate(); e != nil {
		return nil, e
	}
	if lifetime.Err() != nil {
		return nil, fmt.Errorf("actual startup canceled")
	}
	if e = os.Mkdir(ready.manifest.SessionRoot, 0700); e != nil {
		return nil, e
	}
	if e = os.Chmod(ready.manifest.SessionRoot, 0700); e != nil {
		return nil, e
	}
	s.rootInfo, e = os.Lstat(ready.manifest.SessionRoot)
	if e != nil {
		return nil, e
	}
	m := ready.manifest
	if v8 {
		s.campaign, e = startWindowCampaign(ready.prepared, ready.baseline)
	} else {
		s.campaign, e = newWindowCampaignV7(m.CampaignRoot, m.CampaignRoot, m.Scope, ready.source, ready.profiles, m.Scope.Bindings, m.ScopeSHA256, m.ExecutionSHA256, m.Limits, ready.stoppedV6, ready.approval130)
	}
	if e != nil {
		return nil, e
	}
	s.auth = windowAcquisitionAuthorization{campaign: s.campaign, source: ready.source, frozenScopeSHA: m.ScopeSHA256, frozenExecutionSHA: m.ExecutionSHA256, actualExecutionSHA: m.ExecutionSHA256, observer: observer, observerIdentity: m.Observer, modelKey: settings.ModelKey, jevKey: settings.JEVKey, modelUpstream: &windowActualNoRedirectTransport{s.transport}, jevUpstream: &windowActualNoRedirectTransport{s.jevTransport}, fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: settings.LoopbackAdminDSN, ProfilesDir: m.ProfilesDir}, evidenceKind: "recorded_run"}
	if _, e = s.persist("startup.json", s.Status()); e != nil {
		return nil, e
	}
	released := make(chan struct{})
	s.stopParent = context.AfterFunc(ctx, func() { <-released; _ = s.Close() })
	close(released)
	return s, nil
}
func (s *FullWindowActualSession) Status() FullWindowActualStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
func (s *FullWindowActualSession) RunWindow(ctx context.Context, index int, priorReceiptSHA string) (out FullWindowActualReceipt, err error) {
	s.op.Lock()
	defer s.op.Unlock()
	st := s.Status()
	if ctx == nil || ctx.Err() != nil || s.lifetime.Err() != nil || st.Closed || index != st.NextWindowIndex || priorReceiptSHA != st.LastReceiptSHA256 {
		return out, errors.Join(fmt.Errorf("actual session operation rejected"), s.stop())
	}
	if e := s.verify(); e != nil {
		return out, errors.Join(e, s.stop())
	}
	operation, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	detach := context.AfterFunc(s.lifetime, cancel)
	defer detach()
	auth := s.auth
	name := fmt.Sprintf("window-%06d", index)
	auth.privateReportRoot = filepath.Join(s.ready.manifest.SessionRoot, name)
	report, runErr := runWindowAcquisition(operation, &auth)
	s.mu.Lock()
	s.status.V8 = mergeWindowV8Runtime(s.status.V8, report.V8)
	s.status.ModelAttempts += report.Accounting.ModelAttempts
	s.status.JEVAttempts += report.Accounting.JEVAttempts
	s.status.AttemptedArms += report.AttemptedArms
	s.status.FinishedArms += report.FinishedArms
	s.status.FailedAttemptedArms = s.status.AttemptedArms - s.status.FinishedArms
	s.status.UnattemptedArms = s.status.PlannedArms - s.status.AttemptedArms
	s.status.ObservedTurns += report.ObservedTurns
	s.status.QualityEndUnrunTurns += report.QualityEndUnrunTurns
	s.status.GlobalUnrunTurns = s.status.DefinedTurns - s.status.ObservedTurns - s.status.QualityEndUnrunTurns
	s.mu.Unlock()
	if _, e := os.Lstat(auth.privateReportRoot); e == nil {
		s.reportRoots[name] = true
		runErr = errors.Join(runErr, s.rememberReport(name, report))
	}
	raw, e := readWindowActualPrivateBytes(filepath.Join(auth.privateReportRoot, "receipt.json"), 64<<20)
	runErr = errors.Join(runErr, e)
	if report.EvidenceKind != "recorded_run" || report.ScopeSHA256 != s.auth.frozenScopeSHA || report.ExecutionSHA256 != s.auth.frozenExecutionSHA || !report.Advanced || !report.Cleanup || !report.EnvironmentCleanup || !report.Observer || !report.PayloadVerified || operation.Err() != nil || s.lifetime.Err() != nil {
		runErr = errors.Join(runErr, fmt.Errorf("actual acquisition stopped"))
	}
	s.mu.Lock()
	if runErr == nil {
		if index == len(s.campaign.prepared.scope.Windows) {
			s.status.AcquisitionDone = true
			s.status.NextWindowIndex = 0
		} else {
			s.status.NextWindowIndex++
		}
	}
	if runErr != nil || s.status.AcquisitionDone {
		s.status.Closed = true
	}
	out = FullWindowActualReceipt{FullWindowActualStatus: s.status, WindowIndex: index, ReportSHA256: conversationHash(raw), PayloadManifestSHA256: report.PayloadManifestSHA256, LedgerSHA256: report.LedgerSHA256, ReportStatus: report.Status, FutureUnrunFromOrdinal: s.campaign.prepared.scope.Windows[index-1].LastOrdinal + 1}
	s.mu.Unlock()
	out.FirstUnrunOrdinal, out.FirstUnrunArm = closedFirstUnrun(s.campaign.prepared.scope, index, report.Rows)
	if out.FutureUnrunFromOrdinal > len(s.campaign.prepared.scope.Slots) {
		out.FutureUnrunFromOrdinal = 0
	}
	sha, e := s.persist(name+"-summary.json", out)
	if e != nil {
		out.Closed = true
		return out, errors.Join(runErr, e, s.stop())
	}
	out.ReceiptSHA256 = sha
	s.mu.Lock()
	s.status.LastReceiptSHA256 = sha
	s.mu.Unlock()
	if out.Closed {
		runErr = errors.Join(runErr, s.stop())
	}
	return out, runErr
}
func (s *FullWindowActualSession) Close() error {
	s.cancel()
	s.op.Lock()
	defer s.op.Unlock()
	return s.stop()
}
func (s *FullWindowActualSession) stop() error {
	s.mu.Lock()
	s.status.Closed = true
	s.mu.Unlock()
	if s.shutdown {
		return s.closeErr
	}
	s.shutdown = true
	if s.stopParent != nil {
		s.stopParent()
	}
	s.cancel()
	var failures []error
	if s.campaign != nil {
		failures = append(failures, s.campaign.closeWindowCampaign())
	}
	if s.auth.observer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		failures = append(failures, s.auth.observer.Shutdown(ctx))
		cancel()
	}
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	if s.jevTransport != nil {
		s.jevTransport.CloseIdleConnections()
	}
	if s.rootInfo != nil {
		_, e := s.persist("terminal.json", s.Status())
		failures = append(failures, e)
	}
	s.closeErr = errors.Join(failures...)
	return s.closeErr
}

func (s *FullWindowActualSession) persist(name string, v any) (string, error) {
	info, e := os.Lstat(s.ready.manifest.SessionRoot)
	if e != nil || !os.SameFile(info, s.rootInfo) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("actual root changed")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	root, e := os.OpenRoot(s.ready.manifest.SessionRoot)
	if e != nil {
		return "", e
	}
	defer root.Close()
	anchored, e := root.Stat(".")
	if e != nil || !os.SameFile(anchored, s.rootInfo) {
		return "", fmt.Errorf("actual root handle changed")
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return "", e
	}
	n, w := f.Write(raw)
	if n != len(raw) {
		w = errors.Join(w, io.ErrShortWrite)
	}
	if e = errors.Join(w, f.Sync(), f.Close(), windowSyncDirectory(s.ready.manifest.SessionRoot)); e != nil {
		return "", e
	}
	sha := conversationHash(raw)
	s.files[name] = sha
	return sha, nil
}

// The collector's returned receipt/manifest supplies the exact permitted file
// inventory. A newly observed orphan never becomes trusted evidence.
func (s *FullWindowActualSession) rememberReport(name string, report windowAcquisitionReceipt) error {
	receiptRaw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	allowed := map[string]string{"receipt.json": conversationHash(receiptRaw)}
	for _, row := range report.Rows {
		if row.RecordingFile != "" {
			allowed[row.RecordingFile] = row.RecordingSHA256
		}
		if row.QualityEnd != nil {
			allowed[row.QualityEnd.CandidateFile] = row.QualityEnd.CandidateSHA256
			allowed[row.QualityEnd.CleanupFile] = row.QualityEnd.CleanupSHA256
		}
	}
	if report.PayloadManifestSHA256 != "" {
		allowed["payload-manifest.json"] = report.PayloadManifestSHA256
		raw, e := readWindowActualPrivateBytes(filepath.Join(s.ready.manifest.SessionRoot, name, "payload-manifest.json"), 64<<20)
		if e != nil || conversationHash(raw) != report.PayloadManifestSHA256 {
			return fmt.Errorf("actual payload manifest changed")
		}
		var manifest windowPayloadManifest
		if json.Unmarshal(raw, &manifest) != nil {
			return fmt.Errorf("actual payload manifest invalid")
		}
		allowed["native-payloads/store.json"] = ""
		for _, packet := range manifest.Packets {
			allowed["native-payloads/"+packet.Request.File] = packet.Request.FullSHA256
			allowed["native-payloads/"+packet.RequestDescriptorFile] = ""
			if packet.Response != nil {
				allowed["native-payloads/"+packet.Response.DescriptorFile] = ""
				if packet.Response.Body != nil {
					allowed["native-payloads/"+packet.Response.Body.File] = packet.Response.Body.FullSHA256
				}
			}
		}
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(s.ready.manifest.SessionRoot, name), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Join(s.ready.manifest.SessionRoot, name), path)
		if e != nil {
			return e
		}
		key := filepath.Join(name, rel)
		if d.IsDir() {
			if info.Mode().Perm() != 0700 || (rel != "." && rel != "native-payloads") {
				return fmt.Errorf("actual report directory changed")
			}
			s.reportDirs[key] = info
			return nil
		}
		sha, ok := allowed[rel]
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return fmt.Errorf("actual report file changed")
		}
		raw, e := readWindowActualPrivateBytes(path, 64<<20)
		if e != nil {
			return e
		}
		actual := conversationHash(raw)
		if sha != "" && sha != actual {
			return fmt.Errorf("actual report digest differs")
		}
		seen[rel] = true
		s.files[key] = actual
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("actual report file missing")
	}
	return nil
}

// Revalidate frozen inputs without requiring this live owner's roots to be
// absent. Startup alone may use the public readiness gate's new-root rule.
func (s *FullWindowActualSession) verifyInputs() error {
	r := s.ready
	if r == nil || len(r.manifestRaw) == 0 {
		return fmt.Errorf("actual frozen inputs absent")
	}
	if r.manifest.Schema == "full-window-actual-ready.v8" {
		if e := requireWindowV8ExecutionAuthority(r.v8Authority); e != nil {
			return e
		}
	}
	if e := r.revalidateStopped(); e != nil {
		return e
	}
	m := r.manifest
	raw, e := readWindowActualFile(r.manifestPath, windowActualManifestAnchor(r.manifest), 8<<20)
	if e != nil || !reflect.DeepEqual(raw, r.manifestRaw) {
		return fmt.Errorf("actual manifest changed")
	}
	if e := r.verifyBinaryAttestation(); e != nil {
		return e
	}
	if _, e = validateWindowActualSource(m); e != nil {
		return e
	}
	source, e := LoadConversations(m.DefinitionsDir)
	if e != nil || !reflect.DeepEqual(source, r.source) {
		return fmt.Errorf("actual source changed")
	}
	profiles, e := LoadPairedProfiles(m.ProfilesDir)
	if e != nil || !reflect.DeepEqual(profiles, r.profiles) {
		return fmt.Errorf("actual profiles changed")
	}
	for _, f := range []windowActualFile{m.FullGo.Result, m.Prices, m.Envelope} {
		raw, e := readWindowActualFile(f.Path, f.SHA256, 64<<20)
		if e != nil {
			return e
		}
		if f.Path != m.FullGo.Result.Path {
			var evidence windowActualProviderEvidence
			if decodeWindowActualJSON(raw, &evidence) != nil {
				return fmt.Errorf("actual evidence invalid")
			}
			for _, basis := range evidence.Basis {
				if _, e := readWindowActualFile(basis.Path, basis.SHA256, 64<<20); e != nil {
					return e
				}
			}
		}
	}
	if s.auth.observer == nil || s.auth.observer.EvaluationIdentity() != m.Observer || s.auth.observerIdentity != m.Observer {
		return fmt.Errorf("actual observer changed")
	}
	return nil
}
func (s *FullWindowActualSession) verify() error {
	if e := s.verifyInputs(); e != nil {
		return e
	}
	if e := s.verifyReports(); e != nil {
		return e
	}
	if s.campaign == nil {
		return fmt.Errorf("actual campaign absent")
	}
	s.campaign.mu.Lock()
	defer s.campaign.mu.Unlock()
	if s.campaign.closed || s.campaign.poisoned || s.campaign.inventoryWindowCampaign() != nil {
		return fmt.Errorf("actual live owner changed")
	}
	return nil
}
func (s *FullWindowActualSession) verifyReports() error {
	fail := func() error { return fmt.Errorf("actual session evidence changed") }
	info, e := os.Lstat(s.ready.manifest.SessionRoot)
	if e != nil || !os.SameFile(info, s.rootInfo) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail()
	}
	entries, e := os.ReadDir(s.ready.manifest.SessionRoot)
	if e != nil {
		return fail()
	}
	for _, d := range entries {
		if s.reportRoots[d.Name()] {
			continue
		}
		if _, ok := s.files[d.Name()]; !ok {
			return fail()
		}
	}
	for report := range s.reportRoots {
		if e := filepath.WalkDir(filepath.Join(s.ready.manifest.SessionRoot, report), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(s.ready.manifest.SessionRoot, path)
			if e != nil {
				return e
			}
			if d.IsDir() {
				expected, ok := s.reportDirs[rel]
				if !ok || !os.SameFile(info, expected) || info.Mode().Perm() != 0700 {
					return fail()
				}
				return nil
			}
			if _, ok := s.files[rel]; !ok || !info.Mode().IsRegular() {
				return fail()
			}
			return nil
		}); e != nil {
			return e
		}
	}
	for name, expected := range s.reportDirs {
		info, e := os.Lstat(filepath.Join(s.ready.manifest.SessionRoot, name))
		if e != nil || !info.IsDir() || !os.SameFile(info, expected) || info.Mode().Perm() != 0700 {
			return fail()
		}
	}
	for name, sha := range s.files {
		path := filepath.Join(s.ready.manifest.SessionRoot, name)
		info, e := os.Lstat(path)
		if e != nil || info.Mode().Perm() != 0600 {
			return fail()
		}
		if raw, e := readWindowActualPrivateBytes(path, 64<<20); e != nil || conversationHash(raw) != sha {
			return fail()
		}
	}
	return nil
}

// Existing native guards own endpoint/body/accounting validation. This narrow
// wrapper stops http.Client from replaying a redirect response as another POST.
type windowActualNoRedirectTransport struct{ base *http.Transport }

func (t *windowActualNoRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil && response.StatusCode >= 300 && response.StatusCode < 400 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("actual provider redirect rejected")
	}
	return response, err
}

// Session-owned settings and artifacts are bounded private regular files. The
// nonblocking no-follow open prevents a replacement FIFO/symlink from trapping
// shutdown or following an alias after the path check.
func readWindowActualPrivateBytes(path string, max int64) ([]byte, error) {
	reject := func() ([]byte, error) { return nil, fmt.Errorf("actual private file rejected") }
	if max < 1 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return reject()
	}
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return reject()
	}
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0600 || before.Size() > max {
		return reject()
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return reject()
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 || !os.SameFile(before, opened) || opened.Size() > max {
		return reject()
	}
	raw, e := io.ReadAll(io.LimitReader(f, max+1))
	final, fe := f.Stat()
	after, ae := os.Lstat(path)
	ce := f.Close()
	if e != nil || fe != nil || ae != nil || ce != nil || int64(len(raw)) > max || int64(len(raw)) != opened.Size() || final.Size() != opened.Size() || !os.SameFile(opened, after) || final.Mode().Perm() != 0600 || after.Mode().Perm() != 0600 {
		return reject()
	}
	return raw, nil
}

// Binding precedes observer construction, health/database networking and claims.
func checkWindowActualSettingsDigest(settings windowActualSettings, expected string) error {
	if !comparisonDigest(settings.rawSHA256) || settings.rawSHA256 != expected {
		return fmt.Errorf("actual private settings identity mismatch")
	}
	return nil
}
