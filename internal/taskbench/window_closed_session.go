package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

// FullWindowClosedOptions has no provider credentials, endpoint, transport or
// evaluator. The revision is a declaration, not a verified build attestation.
type FullWindowClosedOptions struct {
	DefinitionsDir, ProfilesDir, NewPrivateRoot string
	Width                                       int
	DeclaredSourceRevision                      string
	AllowFixtureCreation                        bool
	LoopbackAdminDSN                            string
}

// FullWindowClosedStatus is detached data, never an execution capability.
type FullWindowClosedStatus struct {
	AttemptedArms         int  `json:"attempted_arms"`
	FailedAttemptedArms   int  `json:"failed_attempted_arms"`
	UnrunArms             int  `json:"unrun_arms"`
	QualityEndUnrunTurns  int  `json:"quality_end_unrun_turns"`
	GlobalUnrunTurns      int  `json:"global_unrun_turns"`
	ClosedAcquisitionDone bool `json:"closed_acquisition_done"`

	EvidenceKind       string `json:"evidence_kind"`
	AccountingKind     string `json:"accounting_kind"`
	Closed             bool   `json:"closed"`
	AllowPaid          bool   `json:"allow_paid"`
	ReadyForPaidRun    bool   `json:"ready_for_paid_run"`
	NextWindowIndex    int    `json:"next_window_index"`
	LastReceiptSHA256  string `json:"last_receipt_sha256"`
	PlannedPairs       int    `json:"planned_pairs"`
	PlannedArms        int    `json:"planned_arms"`
	DefinedTurns       int    `json:"defined_turns"`
	BoundariesExcluded int    `json:"boundaries_excluded"`
	FinishedArms       int    `json:"finished_arms"`
	ObservedTurns      int    `json:"observed_turns"`
	NativePackets      int    `json:"native_packets"`
	ModelAttempts      int    `json:"synthetic_model_attempts"`
	JEVAttempts        int    `json:"synthetic_jev_attempts"`
}

type FullWindowClosedReceipt struct {
	FirstUnrunOrdinal int    `json:"first_unrun_ordinal"`
	FirstUnrunArm     string `json:"first_unrun_arm"`

	FullWindowClosedStatus
	WindowIndex            int    `json:"window_index"`
	ReceiptSHA256          string `json:"-"` // digest of the exact persisted receipt, not self-referential
	ReportSHA256           string `json:"report_sha256"`
	PacketsSHA256          string `json:"packets_sha256"`
	PacketsVerified        bool   `json:"packets_verified"`
	UnrunCampaignArms      int    `json:"unrun_campaign_arms"`
	FutureUnrunFromOrdinal int    `json:"future_unrun_from_ordinal"`
	ReportStatus           string `json:"report_status"`
}

// FullWindowClosedSession retains one live owner and one pair of concrete closed
// providers. Its private lower authorization is never exposed to callers.
type FullWindowClosedSession struct {
	op          sync.Mutex
	mu          sync.Mutex
	status      FullWindowClosedStatus
	cancel      context.CancelFunc
	lifetime    context.Context
	options     FullWindowClosedOptions
	rootInfo    os.FileInfo
	files       map[string]string
	reportRoots map[string]bool
	campaign    *windowCampaign
	auth        windowAcquisitionAuthorization
	model       *windowClosedModelTransport
	judge       *windowClosedJEVTransport
	server      *http.Server
	listener    net.Listener
	shutdown    bool
	closeErr    error
	stopParent  func() bool
	reportDirs  map[string]os.FileInfo
}

func OpenFullWindowClosed(ctx context.Context, o FullWindowClosedOptions) (_ *FullWindowClosedSession, err error) {
	reject := func() (*FullWindowClosedSession, error) {
		return nil, fmt.Errorf("closed session declaration rejected")
	}
	if ctx == nil || ctx.Err() != nil || o.Width < 1 || o.Width > 100 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(o.DeclaredSourceRevision) || !o.AllowFixtureCreation {
		return reject()
	}
	dsn, e := mysqlDriver.ParseDSN(o.LoopbackAdminDSN)
	if e != nil || dsn.Net != "tcp" || dsn.DBName != "" {
		return reject()
	}
	host, _, e := net.SplitHostPort(dsn.Addr)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return reject()
	}
	if canonicalRootWindowCampaign(o.NewPrivateRoot) != nil {
		return reject()
	}
	if _, e = os.Lstat(o.NewPrivateRoot); !os.IsNotExist(e) {
		return reject()
	}
	source, e := LoadConversations(o.DefinitionsDir)
	if e != nil {
		return reject()
	}
	profiles, e := LoadPairedProfiles(o.ProfilesDir)
	if e != nil {
		return reject()
	}
	// These hashes identify explicitly fake rates/envelopes. They do not attest
	// official prices or establish an actual native maximum.
	bindings := fullWindowBindings{PolicySHA256: windowOwnedQualityPolicySHA(), ExecutorRevision: o.DeclaredSourceRevision, CustomerRevision: strings.Repeat("0", 40), PriceEvidenceSHA256: conversationHash([]byte("closed-only synthetic one-micro-unit-per-million v1")), RequestEnvelopeSHA256: conversationHash([]byte("closed-only declared 1MiB/10000/128 model;64000/128 JEV v1"))}
	caps := make([]fullWindowCaps, (3660+o.Width-1)/o.Width)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	scope, e := prepareFullWindowScope(source, profiles, o.Width, caps, bindings)
	if e != nil {
		return reject()
	}
	for i, w := range scope.Windows {
		caps[i] = fullWindowCaps{int64(2 * w.MaxModelCalls), int64(w.MaxJEVCalls)}
	}
	scope, e = prepareFullWindowScope(source, profiles, o.Width, caps, bindings)
	if e != nil {
		return reject()
	}
	scopeSHA, totals, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		return reject()
	}
	if e = os.Mkdir(o.NewPrivateRoot, 0700); e != nil {
		return nil, e
	}
	if e = os.Chmod(o.NewPrivateRoot, 0700); e != nil {
		return nil, e
	}
	info, e := os.Lstat(o.NewPrivateRoot)
	if e != nil {
		return nil, e
	}
	lifetime, cancel := context.WithCancel(ctx)
	s := &FullWindowClosedSession{options: o, rootInfo: info, files: map[string]string{}, reportDirs: map[string]os.FileInfo{}, reportRoots: map[string]bool{}, lifetime: lifetime, cancel: cancel, status: FullWindowClosedStatus{EvidenceKind: "local_stub", AccountingKind: "synthetic_usage_and_rates_not_actual_funds", NextWindowIndex: 1, PlannedPairs: totals.Pairs, PlannedArms: totals.Arms, UnrunArms: totals.Arms, GlobalUnrunTurns: totals.DefinedUserTurns, DefinedTurns: totals.DefinedUserTurns, BoundariesExcluded: totals.Boundaries}}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	root := filepath.Join(o.NewPrivateRoot, "synthetic-campaign")
	limits := windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 1 << 20, MaxInput: 10000, MaxOutput: 128, InputRate: 1, OutputRate: 1}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 1 << 20, MaxInput: 64000, MaxOutput: 128, InputRate: 1}}
	execution, e := executionSHAWindowCampaign(scopeSHA, root, limits)
	if e != nil {
		return nil, e
	}
	prepared, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, scopeSHA, execution, limits)
	if e != nil {
		return nil, e
	}
	// Exact historical constants are only mirrored DATA for the private primitive.
	// No raw paid claim is read/verified/created and no actual funding is consumed.
	baseline := fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104, RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: fullWindowCanonicalClaims()}
	declaration := struct {
		Schema, EvidenceKind, AccountingKind, BaselineMeaning string
		AllowPaid, ReadyForPaidRun                            bool
		Scope                                                 fullWindowScope
		Limits                                                windowCampaignLimits
		MirroredBaselineData                                  fullWindowBaseline
	}{"full-window-closed-declaration.v1", "local_stub", "synthetic_usage_and_rates_not_actual_funds", "mirrored historical DATA only; no raw claim verification or actual funding authorization", false, false, scope, limits, baseline}
	if _, e = s.persist("declaration.json", declaration); e != nil {
		return nil, e
	}
	s.campaign, e = startWindowCampaign(prepared, baseline)
	if e != nil {
		return nil, e
	}
	s.model, s.judge, e = newWindowClosedProviders(windowClosedProviderData{DefinitionsDir: o.DefinitionsDir, ProfilesDir: o.ProfilesDir, CampaignRoot: root, CaptureDir: filepath.Join(o.NewPrivateRoot, "native-packets")})
	if e != nil {
		return nil, e
	}
	s.listener, e = net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	s.server = &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != "POST" || r.URL.Path != "/api/public/otel/v1/traces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, e := io.Copy(io.Discard, io.LimitReader(r.Body, 8<<20)); e != nil {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	})}
	go func() { _ = s.server.Serve(s.listener) }()
	observer, e := observability.New(lifetime, observability.Config{Enabled: true, CaptureContent: true, Endpoint: "http://" + s.listener.Addr().String() + "/api/public/otel/v1/traces", PublicKey: "pk-closed-synthetic", SecretKey: "sk-closed-synthetic", HashKey: strings.Repeat("h", 32), Release: o.DeclaredSourceRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if e != nil {
		return nil, e
	}
	s.auth = windowAcquisitionAuthorization{campaign: s.campaign, source: source, frozenScopeSHA: scopeSHA, frozenExecutionSHA: execution, actualExecutionSHA: execution, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "closed-synthetic-model", jevKey: "closed-synthetic-jev", modelUpstream: s.model, jevUpstream: s.judge, fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: o.LoopbackAdminDSN, ProfilesDir: o.ProfilesDir}, evidenceKind: "local_stub"}
	if _, e = s.persist("startup.json", s.Status()); e != nil {
		return nil, e
	}
	ready := make(chan struct{})
	s.stopParent = context.AfterFunc(ctx, func() { <-ready; _ = s.Close() })
	close(ready)
	return s, nil
}

func (s *FullWindowClosedSession) Status() FullWindowClosedStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// RunWindow executes exactly one predetermined window. Invalid transitions also
// stop the owner, rather than providing a retryable route to the same operation.
func (s *FullWindowClosedSession) RunWindow(ctx context.Context, expectedIndex int, priorReceiptSHA string) (out FullWindowClosedReceipt, err error) {
	s.op.Lock()
	defer s.op.Unlock()
	st := s.Status()
	if ctx == nil || ctx.Err() != nil || s.lifetime.Err() != nil || st.Closed || expectedIndex != st.NextWindowIndex || priorReceiptSHA != st.LastReceiptSHA256 {
		s.stop()
		return out, fmt.Errorf("closed session operation rejected")
	}
	if err = s.verify(); err != nil {
		s.stop()
		return out, err
	}
	operation, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	detach := context.AfterFunc(s.lifetime, cancel)
	defer detach()
	auth := s.auth
	name := fmt.Sprintf("window-%06d", expectedIndex)
	auth.privateReportRoot = filepath.Join(s.options.NewPrivateRoot, name)
	report, runErr := runWindowAcquisition(operation, &auth)
	// Record actual attempts even when acquisition fails. They remain synthetic.
	s.mu.Lock()
	s.status.ModelAttempts += report.Accounting.ModelAttempts
	s.status.JEVAttempts += report.Accounting.JEVAttempts
	s.status.AttemptedArms += report.AttemptedArms
	s.status.FinishedArms += report.FinishedArms
	s.status.FailedAttemptedArms = s.status.AttemptedArms - s.status.FinishedArms
	s.status.UnrunArms = s.status.PlannedArms - s.status.AttemptedArms
	s.status.QualityEndUnrunTurns += report.QualityEndUnrunTurns
	s.status.ObservedTurns += report.ObservedTurns
	s.status.GlobalUnrunTurns = s.status.DefinedTurns - s.status.ObservedTurns - s.status.QualityEndUnrunTurns
	s.mu.Unlock()
	packets, packetErr := verifyWindowClosedPackets(s.model, s.judge)
	if packetErr == nil {
		s.mu.Lock()
		s.status.NativePackets = len(packets)
		s.mu.Unlock()
	}
	// The collector owns report creation; retain every report file as immutable
	// evidence before allowing a subsequent operation.
	if _, e := os.Lstat(auth.privateReportRoot); e == nil {
		s.reportRoots[name] = true
		if e = s.rememberReport(name, report); e != nil {
			runErr = errors.Join(runErr, e)
		}
	}
	reportRaw, reportReadErr := os.ReadFile(filepath.Join(auth.privateReportRoot, "receipt.json"))
	reportSHA := ""
	if reportReadErr == nil {
		reportSHA = conversationHash(reportRaw)
	} else {
		runErr = errors.Join(runErr, reportReadErr)
	}
	packetRaw, e := json.Marshal(packets)
	if e != nil {
		runErr = errors.Join(runErr, e)
	}
	s.mu.Lock()
	if runErr == nil && packetErr == nil && report.Advanced {
		if expectedIndex == len(s.campaign.prepared.scope.Windows) {
			s.status.ClosedAcquisitionDone = true
			s.status.NextWindowIndex = 0
		} else {
			s.status.NextWindowIndex++
		}
	}
	out = FullWindowClosedReceipt{FullWindowClosedStatus: s.status, WindowIndex: expectedIndex, ReportSHA256: reportSHA, PacketsSHA256: conversationHash(packetRaw), PacketsVerified: packetErr == nil, UnrunCampaignArms: s.status.UnrunArms, FutureUnrunFromOrdinal: s.campaign.prepared.scope.Windows[expectedIndex-1].LastOrdinal + 1, ReportStatus: report.Status}
	s.mu.Unlock()
	out.FirstUnrunOrdinal, out.FirstUnrunArm = closedFirstUnrun(s.campaign.prepared.scope, expectedIndex, report.Rows)
	if out.FutureUnrunFromOrdinal > len(s.campaign.prepared.scope.Slots) {
		out.FutureUnrunFromOrdinal = 0
	}
	if out.ClosedAcquisitionDone {
		out.Closed = true
	}
	if runErr != nil || packetErr != nil || !report.Advanced {
		out.Closed = true
		err = errors.Join(runErr, packetErr, fmt.Errorf("closed window stopped"))
	}
	if out.Closed {
		s.mu.Lock()
		s.status.Closed = true
		s.mu.Unlock()
	}
	sha, e := s.persist(name+"-summary.json", out)
	if e != nil {
		s.stop()
		out.Closed = true
		return out, errors.Join(err, e)
	}
	out.ReceiptSHA256 = sha
	s.mu.Lock()
	s.status.LastReceiptSHA256 = sha
	s.mu.Unlock()
	if out.Closed {
		err = errors.Join(err, s.stop())
	}
	return out, err
}

func (s *FullWindowClosedSession) Close() error {
	s.cancel() // promptly interrupt an active operation before waiting for it
	s.op.Lock()
	defer s.op.Unlock()
	return s.stop()
}
func (s *FullWindowClosedSession) stop() error {
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.auth.observer != nil {
		failures = append(failures, s.auth.observer.Shutdown(shutdownCtx))
	}
	if s.server != nil {
		failures = append(failures, s.server.Close())
	} else if s.listener != nil {
		failures = append(failures, s.listener.Close())
	}
	_, terminalErr := s.persist("terminal.json", s.Status())
	failures = append(failures, terminalErr)
	s.closeErr = errors.Join(failures...)
	return s.closeErr
}

func (s *FullWindowClosedSession) persist(name string, v any) (string, error) {
	info, e := os.Lstat(s.options.NewPrivateRoot)
	if e != nil || !os.SameFile(info, s.rootInfo) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("closed root changed")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	root, e := os.OpenRoot(s.options.NewPrivateRoot)
	if e != nil {
		return "", e
	}
	defer root.Close()
	anchored, e := root.Stat(".")
	if e != nil || !os.SameFile(anchored, s.rootInfo) {
		return "", fmt.Errorf("closed root handle changed")
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return "", e
	}
	n, w := f.Write(raw)
	if n != len(raw) {
		w = errors.Join(w, io.ErrShortWrite)
	}
	if e = errors.Join(w, f.Sync(), f.Close(), windowSyncDirectory(s.options.NewPrivateRoot)); e != nil {
		return "", e
	}
	sha := conversationHash(raw)
	s.files[name] = sha
	return sha, nil
}

// The collector's returned receipt/manifest supplies the exact permitted file
// inventory. A newly observed orphan never becomes trusted evidence.
func (s *FullWindowClosedSession) rememberReport(name string, report windowAcquisitionReceipt) error {
	allowed := map[string]string{"receipt.json": ""}
	for _, row := range report.Rows {
		if row.RecordingFile != "" {
			allowed[row.RecordingFile] = row.RecordingSHA256
		}
	}
	if report.PayloadManifestSHA256 != "" {
		allowed["payload-manifest.json"] = report.PayloadManifestSHA256
		raw, e := os.ReadFile(filepath.Join(s.options.NewPrivateRoot, name, "payload-manifest.json"))
		if e != nil || conversationHash(raw) != report.PayloadManifestSHA256 {
			return fmt.Errorf("closed payload manifest changed")
		}
		var manifest windowPayloadManifest
		if json.Unmarshal(raw, &manifest) != nil {
			return fmt.Errorf("closed payload manifest invalid")
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
	err := filepath.WalkDir(filepath.Join(s.options.NewPrivateRoot, name), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Join(s.options.NewPrivateRoot, name), path)
		if e != nil {
			return e
		}
		key := filepath.Join(name, rel)
		if d.IsDir() {
			if info.Mode().Perm() != 0700 || (rel != "." && rel != "native-payloads") {
				return fmt.Errorf("closed report directory changed")
			}
			s.reportDirs[key] = info
			return nil
		}
		sha, ok := allowed[rel]
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return fmt.Errorf("closed report file changed")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		actual := conversationHash(raw)
		if sha != "" && sha != actual {
			return fmt.Errorf("closed report digest differs")
		}
		seen[rel] = true
		s.files[key] = actual
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("closed report file missing")
	}
	return nil
}

func closedFirstUnrun(scope fullWindowScope, index int, rows []windowAcquisitionRow) (int, string) {
	attempted := map[string]bool{}
	for _, row := range rows {
		if row.Attempted {
			attempted[fmt.Sprintf("%d/%s", row.Ordinal, row.Arm)] = true
		}
	}
	for _, slot := range scope.Slots[scope.Windows[index-1].FirstOrdinal-1:] {
		for _, arm := range slot.ArmOrder {
			if !attempted[fmt.Sprintf("%d/%s", slot.Ordinal, arm)] {
				return slot.Ordinal, arm
			}
		}
	}
	return 0, ""
}

func (s *FullWindowClosedSession) verify() error {
	fail := func() error { return fmt.Errorf("closed session evidence changed") }
	info, e := os.Lstat(s.options.NewPrivateRoot)
	if e != nil || !os.SameFile(info, s.rootInfo) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail()
	}
	source, e := LoadConversations(s.options.DefinitionsDir)
	if e != nil || !reflect.DeepEqual(source, s.auth.source) {
		return fail()
	}
	profiles, e := LoadPairedProfiles(s.options.ProfilesDir)
	if e != nil || !reflect.DeepEqual(profiles, s.model.capture.profiles) {
		return fail()
	}
	entries, e := os.ReadDir(s.options.NewPrivateRoot)
	if e != nil {
		return fail()
	}
	for _, d := range entries {
		if d.Name() == "synthetic-campaign" || d.Name() == "native-packets" || s.reportRoots[d.Name()] {
			continue
		}
		if _, ok := s.files[d.Name()]; !ok {
			return fail()
		}
	}
	for report := range s.reportRoots {
		if e := filepath.WalkDir(filepath.Join(s.options.NewPrivateRoot, report), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if d.IsDir() {
				rel, e := filepath.Rel(s.options.NewPrivateRoot, path)
				expected, ok := s.reportDirs[rel]
				if e != nil || !ok || !os.SameFile(info, expected) || info.Mode().Perm() != 0700 {
					return fail()
				}
				return nil
			}
			rel, e := filepath.Rel(s.options.NewPrivateRoot, path)
			if e != nil {
				return e
			}
			if _, ok := s.files[rel]; !ok || !info.Mode().IsRegular() {
				return fail()
			}
			return nil
		}); e != nil {
			return fail()
		}
	}
	root, e := os.OpenRoot(s.options.NewPrivateRoot)
	if e != nil {
		return fail()
	}
	defer root.Close()
	for name, expected := range s.reportDirs {
		info, e := root.Lstat(name)
		if e != nil || !info.IsDir() || !os.SameFile(info, expected) {
			return fail()
		}
	}
	for name, sha := range s.files {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return fail()
		}
		raw, e := root.ReadFile(name)
		if e != nil || conversationHash(raw) != sha {
			return fail()
		}
	}
	if _, e = verifyWindowClosedPackets(s.model, s.judge); e != nil {
		return fail()
	}
	s.campaign.mu.Lock()
	defer s.campaign.mu.Unlock()
	if s.campaign.closed || s.campaign.poisoned || s.campaign.inventoryWindowCampaign() != nil {
		return fail()
	}
	return nil
}
