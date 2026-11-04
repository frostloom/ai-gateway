package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
)

func v11TestObserver(t *testing.T) (*observability.Observer, observability.DurableSpoolConfig) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	dc := observability.DurableSpoolConfig{Root: filepath.Join(root, "spool"), RunID: strings.Repeat("b", 64), MaxBytes: 4 << 30}
	c := observability.Config{Enabled: true, CaptureContent: true, SampleRate: 1, Environment: "evaluation", Release: strings.Repeat("a", 40), Endpoint: "http://127.0.0.1:1/api/public/otel/v1/traces", PublicKey: "test", SecretKey: "test", HashKey: strings.Repeat("x", 32), QueueSize: 256, ExportTimeout: 10 * time.Second, DurableSpool: &dc}
	o, e := observability.New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { o.Shutdown(context.Background()) })
	return o, dc
}
func TestWindowV11GenuineObservationReceipt(t *testing.T) {
	o, dc := v11TestObserver(t)
	ctx := context.Background()
	id := o.EvaluationIdentity()
	if e := checkWindowObservationReceipt(ctx, "full-window-scope.v11", o, id); e != nil {
		t.Fatal(e)
	}
	start := o.Stats()
	_, turn := o.StartTurn(ctx, "model")
	turn.End(200, observability.Summary{})
	if e := o.ForceFlush(ctx); e != nil {
		t.Fatal(e)
	}
	if e := checkWindowArmObservationReceipt(ctx, "full-window-scope.v11", o, id, start, 1); e != nil {
		t.Fatal(e)
	}
	if e := checkWindowArmObservationReceipt(ctx, "full-window-scope.v11", o, id, start, 2); e == nil {
		t.Fatal("incomplete turn sampling accepted")
	}
	if e := checkWindowObservationReceipt(ctx, "full-window-scope.v10", o, id); e == nil {
		t.Fatal("durable exporter accepted as direct")
	}
	bad := id
	bad.SpoolRunID = strings.Repeat("c", 64)
	if e := checkWindowObservationReceipt(ctx, "full-window-scope.v11", o, bad); e == nil {
		t.Fatal("foreign run accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if e := checkWindowObservationReceipt(canceled, "full-window-scope.v11", o, id); e == nil {
		t.Fatal("canceled receipt")
	}
	if e := os.WriteFile(filepath.Join(dc.Root, "foreign"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := checkWindowObservationReceipt(ctx, "full-window-scope.v11", o, id); e == nil {
		t.Fatal("foreign leaf accepted")
	}
}
func TestWindowV11SettingsWireAndFrozenSpool(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	spool := filepath.Join(root, "spool")
	var raw map[string]any
	json.Unmarshal([]byte(strings.ReplaceAll(actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces"), `"export_timeout_ms":2000`, `"export_timeout_ms":10000`)), &raw)
	raw["observer"].(map[string]any)["durable"] = map[string]any{"root": spool, "run_id": strings.Repeat("b", 64), "max_bytes": int64(4 << 30)}
	data, _ := json.Marshal(raw)
	os.WriteFile(p, data, 0600)
	settings, e := readWindowActualSettingsV11(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = readWindowActualSettingsForTimeout(p, 10000); e == nil {
		t.Fatal("old11-key parser accepts durable")
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v11", SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: spool, PrivateSettingsSHA256: conversationHash(data), Observer: observability.EvaluationIdentity{ObservationReceiptBoundary: "durable_local", SpoolRootSHA256: conversationHash([]byte(spool)), SpoolRunID: strings.Repeat("b", 64), SpoolMaxBytes: 4 << 30, SampleRate: 1}}
	bindV11TestManifest(&m)
	if e = validateWindowV11SpoolBinding(m); e != nil {
		t.Fatal(e)
	}
	if e = checkWindowV11SettingsSpool(settings, m); e != nil {
		t.Fatal(e)
	}
	m.Observer.SpoolRunID = strings.Repeat("c", 64)
	if e = checkWindowV11SettingsSpool(settings, m); e == nil {
		t.Fatal("settings run mismatch accepted")
	}
	m.Observer.SpoolRunID = strings.Repeat("b", 64)
	for _, bad := range []any{nil, map[string]any{"root": spool, "run_id": strings.Repeat("b", 64), "max_bytes": 1024}, map[string]any{"root": spool, "run_id": strings.Repeat("b", 64), "max_bytes": int64(4 << 30), "fresh": true}, map[string]any{"Root": spool, "run_id": strings.Repeat("b", 64), "max_bytes": int64(4 << 30)}} {
		raw["observer"].(map[string]any)["durable"] = bad
		b, _ := json.Marshal(raw)
		os.WriteFile(p, b, 0600)
		if _, e = readWindowActualSettingsV11(p); e == nil {
			t.Fatal("invalid durable wire accepted")
		}
	}
}
func TestWindowV11OldActualEpochsRejectBeforeSettings(t *testing.T) {
	for _, epoch := range []string{"v8", "v9", "v10", "v11"} {
		r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + epoch, Scope: fullWindowScope{Schema: "full-window-scope." + epoch}}}
		if _, e := OpenFullWindowActual(context.Background(), r, "/must-not-read-private-settings"); e == nil || !strings.Contains(e.Error(), "v12 delegated") {
			t.Fatalf("%s: %v", epoch, e)
		}
	}
}
func TestWindowV11EmptyPreparationCannotAuthorize(t *testing.T) {
	if _, e := AuthorizeFullWindowV11Preparation(&FullWindowV11Preparation{}, "/must-not-read"); e == nil {
		t.Fatal("empty preparation authorized")
	}
}
func TestWindowV11OwnershipTransitionAndCancellation(t *testing.T) {
	o, dc := v11TestObserver(t)
	root := filepath.Dir(dc.Root)
	m := windowActualManifest{Schema: "full-window-actual-ready.v11", Scope: fullWindowScope{Schema: "full-window-scope.v11"}, SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: dc.Root, Observer: o.EvaluationIdentity()}
	bindV11TestManifest(&m)
	if e := checkWindowV11FreshSpool(m); e == nil {
		t.Fatal("created root passed initial freshness")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner, e := selectWindowV11ObservationOwner(ctx, m, o)
	if e != nil {
		t.Fatal(e)
	}
	if e = owner.healthy(context.Background(), m); e != nil {
		t.Fatal("genuine owned root rejected", e)
	}
	forged := &windowV11ObservationOwner{}
	if e = forged.healthy(context.Background(), m); e == nil {
		t.Fatal("zero owner accepted")
	}
	cancel()
	if e = owner.healthy(context.Background(), m); e == nil {
		t.Fatal("Background bypasses bound owner cancellation")
	}
}
func TestWindowV11SpoolDisjointAllRoots(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	m := windowActualManifest{Schema: "full-window-actual-ready.v11", SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: filepath.Join(root, "spool")}
	m.Observer = observability.EvaluationIdentity{SampleRate: 1, ObservationReceiptBoundary: "durable_local", SpoolRootSHA256: conversationHash([]byte(m.SpoolRoot)), SpoolRunID: strings.Repeat("a", 64), SpoolMaxBytes: 4 << 30}
	bindV11TestManifest(&m)
	if e := validateWindowV11SpoolBinding(m); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*windowActualManifest){func(x *windowActualManifest) { x.SourceRoot = x.SessionRoot }, func(x *windowActualManifest) { x.CampaignRoot = filepath.Join(x.SourceRoot, "inside") }, func(x *windowActualManifest) { x.ClaimsDir = x.SessionRoot }} {
		bad := m
		change(&bad)
		if e := validateWindowV11SpoolBinding(bad); e == nil {
			t.Fatal("overlapping non-spool roots accepted")
		}
	}
}
func TestWindowV11ObservationStatusDetached(t *testing.T) {
	s := FullWindowActualStatus{Observation: &windowObservationReceiptV11{Boundary: "durable_local", RemoteDelivery: "pending"}}
	out := detachedWindowActualStatusV10(s)
	out.Observation.RemoteDelivery = "forged"
	if s.Observation.RemoteDelivery != "pending" {
		t.Fatal("mutable observation summary escaped")
	}
}
func TestWindowV11FailedReceiptRetainsLocalBoundary(t *testing.T) {
	o, dc := v11TestObserver(t)
	ctx := context.Background()
	_, turn := o.StartTurn(ctx, "model")
	turn.End(200, observability.Summary{})
	if e := o.ForceFlush(ctx); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dc.Root, "foreign"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := windowV11Receipt(o, true)
	if e == nil || r == nil || r.Boundary != "durable_local" || r.RemoteDelivery != "pending" || !r.Spool.Poisoned || r.Spool.Spans != 1 || r.Spool.RunID != dc.RunID || r.NormalClosure {
		t.Fatalf("failure lost original receipt boundary: %+v %v", r, e)
	}
	if e = o.Shutdown(ctx); e == nil {
		t.Fatal("poisoned writer closed normally")
	}
}

func bindV11TestManifest(m *windowActualManifest) {
	if m.PrivateSettingsSHA256 == "" {
		m.PrivateSettingsSHA256 = strings.Repeat("e", 64)
	}
	m.Scope.Schema = "full-window-scope.v11"
	m.Scope.Observation = &windowObservationDeclarationV11{Schema: "full-window-observation-declaration.v11", PrivateSettingsSHA256: m.PrivateSettingsSHA256, SpoolRoot: m.SpoolRoot, SpoolRunID: m.Observer.SpoolRunID, SpoolMaxBytes: m.Observer.SpoolMaxBytes, Boundary: m.Observer.ObservationReceiptBoundary, ObserverIdentitySHA256: windowV8DeclarationSHA(m.Observer)}
}
