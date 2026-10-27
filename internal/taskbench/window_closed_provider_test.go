package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/jev"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureWindowClosedProviders(t *testing.T) (*windowClosedModelTransport, *windowClosedJEVTransport, string) {
	t.Helper()
	root := t.TempDir()
	model, judge, err := newWindowClosedProviders(windowClosedProviderData{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", CampaignRoot: filepath.Join(root, "campaign"), CaptureDir: filepath.Join(root, "packets")})
	if err != nil {
		t.Fatal(err)
	}
	return model, judge, filepath.Join(root, "packets")
}
func TestWindowClosedProviderConstructionAndOutsidePhase(t *testing.T) {
	model, judge, dir := fixtureWindowClosedProviders(t)
	if model == nil || judge == nil {
		t.Fatal("missing concrete closed providers")
	}
	for _, rt := range []http.RoundTripper{model, judge} {
		r, _ := http.NewRequestWithContext(context.Background(), "POST", "https://api.deepseek.com/anthropic/v1/messages", strings.NewReader(`{"model":"deepseek-flash"}`))
		if response, err := rt.RoundTrip(r); err == nil || response != nil {
			t.Fatal("outside owned phase admitted")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside phase wrote capture", err)
	}
}

func fixtureWindowClosedPhase(t *testing.T, selective bool) (*windowOwnedDriver, context.Context) {
	t.Helper()
	d, ctx := fixtureWindowOwnedPhase(t)
	source, profiles, _ := windowScopeInputs(t)
	primary, _, err := fullCustomerSelection(source)
	if err != nil {
		t.Fatal(err)
	}
	d.caseDefinition = primary.Cases[0]
	d.environment.profiles = profiles
	if selective {
		d.arm = "jev_selective"
		d.permit.nextArm = 1
		d.permit.proofs = []*windowOwnedArmProof{nil}
		d.permit.proofSHA = []string{"synthetic-prior-arm"}
	}
	return d, ctx
}
func closedNativeModelRequest(t *testing.T, ctx context.Context, d *windowOwnedDriver, unknown bool) (*http.Request, []byte) {
	t.Helper()
	user := d.caseDefinition.Scenario.Turns[0].User
	if unknown {
		user = "unrecognized closed oracle user"
	}
	userRaw, _ := json.Marshal(user)
	raw := []byte(`{"model":"deepseek-flash","max_tokens":128,"thinking":{"type":"disabled"},"temperature":9007199254740993,"messages":[{"role":"user","content":[{"type":"text","text":` + string(userRaw) + `}]}]}`)
	r, err := http.NewRequestWithContext(ctx, "POST", "https://api.deepseek.com/anthropic/v1/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer do-not-capture-provider-key")
	return r, raw
}
func TestWindowClosedProviderOriginalNativePacketAndGuards(t *testing.T) {
	model, judge, dir := fixtureWindowClosedProviders(t)
	d, ctx := fixtureWindowClosedPhase(t, false)
	transport, err := newWindowProviderTransport(d, "model", model)
	if err != nil {
		t.Fatal(err)
	}
	r, raw := closedNativeModelRequest(t, ctx, d, false)
	response, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	packets, err := verifyWindowClosedPackets(model, judge)
	if err != nil || len(packets) != 1 {
		t.Fatal("missing immutable packet", err)
	}
	packet := packets[0]
	persisted, err := os.ReadFile(filepath.Join(dir, packet.RequestFile))
	if err != nil || !bytes.Equal(persisted, raw) {
		t.Fatal("native packet rewritten", err)
	}
	if bytes.Contains(persisted, []byte("local-full-customer")) || bytes.Contains(persisted, []byte("do-not-capture-provider-key")) || !bytes.Contains(persisted, []byte("9007199254740993")) {
		t.Fatal("rewrite, header leakage, or numeric corruption")
	}
	if packet.RequestSHA256 != conversationHash(raw) || packet.ResponseSHA256 != conversationHash(responseBody) || !packet.Synthetic || packet.Request != (windowRequestIdentity{Provider: "model", Model: "deepseek-flash", CaseKey: d.slot.CaseKey, Pass: 1, Arm: "model", Turn: 1, Attempt: 1, BodySHA256: conversationHash(raw), Bytes: len(raw)}) {
		t.Fatalf("capture pending identity mismatch: %+v", packet)
	}
	if d.ledger.Snapshot().Pending != nil || d.ledger.Snapshot().ModelAttempts != 1 || d.ledger.Snapshot().Halt != "" {
		t.Fatal("guard did not settle bounded fake usage")
	}
	packets[0].Provider = "tampered returned copy"
	again, err := verifyWindowClosedPackets(model, judge)
	if err != nil || again[0].Provider != "model" {
		t.Fatal("mutable descriptor escaped")
	}
	for _, entry := range []string{packet.RequestFile, packet.ResponseFile, packet.DescriptorFile} {
		info, err := os.Stat(filepath.Join(dir, entry))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("packet permissions", err)
		}
	}
}
func TestWindowClosedProviderJEVActualClientTypedBenignComplex(t *testing.T) {
	model, judge, dir := fixtureWindowClosedProviders(t)
	d, ctx := fixtureWindowClosedPhase(t, true)
	evaluator, err := newWindowJEV(d, "synthetic-key-never-discovered", judge)
	if err != nil {
		t.Fatal(err)
	}
	questions := map[string]jev.Question{"route_shape": {Type: jev.QChoice, Criteria: map[string]string{"single_read": "read", "complex": "fallback"}}, "legit": {Type: jev.QNoul, Criteria: map[string]string{"true": "yes", "false": "no"}}, "risk": {Type: jev.QScore, Criteria: []string{"low", "high"}}}
	result, err := evaluator.Evaluate(ctx, map[string]any{"native_state_number": json.Number("9007199254740993")}, questions)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "jev-1.13.0" || result.Answers["route_shape"].Choice != "complex" || result.Answers["route_shape"].Conf() != .99 || result.Answers["legit"].Noul != .99 || result.Answers["risk"].Confidence != .99 {
		t.Fatal("not benign complex native JEV")
	}
	packets, err := verifyWindowClosedPackets(model, judge)
	if err != nil || len(packets) != 1 || packets[0].Provider != "jev" {
		t.Fatal("JEV capture", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, packets[0].RequestFile))
	if err != nil || !bytes.Contains(raw, []byte("9007199254740993")) || !bytes.Contains(raw, []byte(`"questions"`)) || bytes.Contains(raw, []byte("synthetic-key")) {
		t.Fatal("JEV native body missing or headers leaked")
	}
	if d.ledger.Snapshot().JEVAttempts != 1 || d.ledger.Snapshot().Halt != "" {
		t.Fatal("JEV response failed guard")
	}
}
func TestWindowClosedProviderRequestDurableBeforeOracleFailure(t *testing.T) {
	model, judge, dir := fixtureWindowClosedProviders(t)
	d, ctx := fixtureWindowClosedPhase(t, false)
	transport, err := newWindowProviderTransport(d, "model", model)
	if err != nil {
		t.Fatal(err)
	}
	r, raw := closedNativeModelRequest(t, ctx, d, true)
	if response, err := transport.RoundTrip(r); err == nil || response != nil {
		t.Fatal("oracle failure returned response")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "packet-000001-request.json" {
		t.Fatal("original request was not durable before oracle failure", err)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !bytes.Equal(saved, raw) || d.ledger.Snapshot().Halt == "" {
		t.Fatal("missing original or guard not poisoned")
	}
	if _, err := verifyWindowClosedPackets(model, judge); err == nil {
		t.Fatal("failed partial capture verified")
	}
}
func TestWindowClosedProviderDuplicateAndTamperStop(t *testing.T) {
	for _, kind := range []string{"preexisting_packet", "tampered_request", "extra_file", "directory_replace", "descriptor_mode"} {
		t.Run(kind, func(t *testing.T) {
			model, judge, dir := fixtureWindowClosedProviders(t)
			d, ctx := fixtureWindowClosedPhase(t, false)
			if kind == "preexisting_packet" {
				if err := os.WriteFile(filepath.Join(dir, "packet-000001-request.json"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			transport, err := newWindowProviderTransport(d, "model", model)
			if err != nil {
				t.Fatal(err)
			}
			r, _ := closedNativeModelRequest(t, ctx, d, false)
			response, err := transport.RoundTrip(r)
			if kind == "preexisting_packet" {
				if err == nil || response != nil || d.ledger.Snapshot().Halt == "" {
					t.Fatal("overwrote existing packet")
				}
				raw, _ := os.ReadFile(filepath.Join(dir, "packet-000001-request.json"))
				if string(raw) != "original" {
					t.Fatal("old bytes changed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			switch kind {
			case "tampered_request":
				os.WriteFile(filepath.Join(dir, "packet-000001-request.json"), []byte("tampered"), 0600)
			case "extra_file":
				os.WriteFile(filepath.Join(dir, "unowned"), []byte("foreign"), 0600)
			case "directory_replace":
				os.Rename(dir, dir+"-moved")
				os.Mkdir(dir, 0700)
			case "descriptor_mode":
				os.Chmod(filepath.Join(dir, "packet-000001.json"), 0644)
			}
			if _, err := verifyWindowClosedPackets(model, judge); err == nil || d.ledger.Snapshot().Halt == "" {
				t.Fatal("tamper admitted without poisoning")
			}
		})
	}
}
func TestWindowClosedProviderRejectsWrongEndpointIdentityAndReplay(t *testing.T) {
	for _, kind := range []string{"endpoint", "model", "identity", "direct_no_intent", "replay"} {
		t.Run(kind, func(t *testing.T) {
			model, _, dir := fixtureWindowClosedProviders(t)
			d, ctx := fixtureWindowClosedPhase(t, false)
			r, raw := closedNativeModelRequest(t, ctx, d, false)
			if kind == "direct_no_intent" {
				if _, err := model.RoundTrip(r); err == nil {
					t.Fatal("missing pending admitted")
				}
			} else {
				id, err := d.windowNextRequest(ctx, "model")
				if err != nil {
					t.Fatal(err)
				}
				id.BodySHA256 = conversationHash(raw)
				id.Bytes = len(raw)
				if kind == "identity" {
					id.CaseKey = "foreign/case"
				}
				if _, err = d.ledger.Begin(id); err != nil {
					t.Fatal(err)
				}
				if kind == "endpoint" {
					r.URL.Host = "example.invalid"
				}
				if kind == "model" {
					r.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(string(raw), "deepseek-flash", "wrong-model")))
				}
				response, err := model.RoundTrip(r)
				if kind == "replay" {
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					r, _ = closedNativeModelRequest(t, ctx, d, false)
					if _, err = model.RoundTrip(r); err == nil {
						t.Fatal("same pending replay captured")
					}
				} else if err == nil {
					t.Fatal("wrong native endpoint/model/identity admitted")
				}
			}
			if d.ledger.Snapshot().Halt == "" {
				t.Fatal("invalid closed call did not stop")
			}
			entries, _ := os.ReadDir(dir)
			if kind != "replay" && len(entries) != 0 {
				t.Fatal("rejected call wrote packets")
			}
		})
	}
}
func TestWindowClosedProviderConstructorNewDisjointOnly(t *testing.T) {
	root := t.TempDir()
	base := windowClosedProviderData{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", CampaignRoot: filepath.Join(root, "campaign"), CaptureDir: filepath.Join(root, "packets")}
	model, judge, err := newWindowClosedProviders(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = newWindowClosedProviders(base); err == nil {
		t.Fatal("capture resume admitted")
	}
	other := base
	other.CampaignRoot = root
	other.CaptureDir = filepath.Join(root, "inside")
	if _, _, err = newWindowClosedProviders(other); err == nil {
		t.Fatal("capture inside campaign admitted")
	}
	if _, err = verifyWindowClosedPackets(model, &windowClosedJEVTransport{}); err == nil {
		t.Fatal("foreign providers joined")
	}
	if packets, err := verifyWindowClosedPackets(model, judge); err != nil || len(packets) != 0 {
		t.Fatal("empty live capture verify", err)
	}
}

func TestWindowClosedProviderDirectoryReplacementBeforeRequest(t *testing.T) {
	model, _, dir := fixtureWindowClosedProviders(t)
	d, ctx := fixtureWindowClosedPhase(t, false)
	if err := os.Rename(dir, dir+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	transport, err := newWindowProviderTransport(d, "model", model)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := closedNativeModelRequest(t, ctx, d, false)
	response, err := transport.RoundTrip(r)
	if err == nil {
		response.Body.Close()
		t.Fatal("replacement capture directory accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("wrote into replacement directory")
	}
}

func TestWindowClosedProviderActualOwnedCollectorOnePair(t *testing.T) {
	auth, oldModel, oldJEV := testWindowCollectorAuthorization(t)
	dir := filepath.Join(filepath.Dir(auth.campaign.root), "native-packets")
	model, judge, err := newWindowClosedProviders(windowClosedProviderData{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", CampaignRoot: auth.campaign.root, CaptureDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	auth.modelUpstream = model
	auth.jevUpstream = judge
	receipt, err := runWindowAcquisition(context.Background(), auth)
	if err != nil {
		t.Fatalf("actual collector stopped: %+v %v", receipt, err)
	}
	if receipt.Status != "collected_window" || receipt.FinishedArms != 2 || !receipt.Cleanup || !receipt.EnvironmentCleanup || !receipt.Observer || !receipt.Advanced || *oldModel != 0 || *oldJEV != 0 {
		t.Fatalf("actual owned capture failed or delegated to another transport: %+v", receipt)
	}
	packets, err := verifyWindowClosedPackets(model, judge)
	if err != nil || len(packets) != 3 {
		t.Fatalf("actual native packets=%d err=%v", len(packets), err)
	}
	providers := map[string]int{}
	armModels := map[string]int{}
	for _, packet := range packets {
		providers[packet.Provider]++
		if packet.Provider == "model" {
			armModels[packet.Arm]++
		}
		if !packet.Synthetic || packet.Ordinal != 1 || packet.Pass != 1 || packet.Turn != 1 || packet.Attempt != 1 {
			t.Fatal("actual native identity differs")
		}
	}
	if providers["model"] != 2 || providers["jev"] != 1 || armModels["model"] != 1 || armModels["jev_selective"] != 1 {
		t.Fatal("NORMAL fallback did not cover both real serializers", providers)
	}
	if receipt.Accounting.ModelAttempts != 2 || receipt.Accounting.JEVAttempts != 1 || receipt.Accounting.Halt != "" {
		t.Fatal("guard usage accounting differs")
	}
	if auth.campaign.prepared.scope.Slots[0].CaseKey != "adv-001/answer_complete" {
		t.Fatal("first-source expectation changed")
	}
	for _, row := range receipt.Rows {
		raw, err := os.ReadFile(filepath.Join(auth.privateReportRoot, row.RecordingFile))
		if err != nil {
			t.Fatal(err)
		}
		var record collectionRecording
		if json.Unmarshal(raw, &record) != nil || len(record.Result.Observations) != 1 {
			t.Fatal("source observation missing")
		}
		obs := record.Result.Observations[0]
		stats := obs.Turn.Response.Telemetry
		if stats == nil || stats.LLMCalls != 1 || stats.JevBlocked || stats.JevShortCircuit || stats.JevFastPath || len(obs.Turn.Response.Trace) != 0 {
			t.Fatal("direct source rejection placeholder was bypassed or tool loop invented")
		}
		if row.Arm == "jev_selective" && (stats.JevCalls != 1 || stats.JevErrors != 0 || stats.JevRouteShape != "complex" || stats.JevIntentConfidence != .5 || stats.JevLegitProbability != .99 || stats.JevRiskConfidence != .99) {
			t.Fatal("selective NORMAL benign complex fallback missing")
		}
		t.Logf("native source=%s arm=%s action=%s model_calls=%d jev_calls=%d trace_items=%d jev_blocked=%t fast_path=%t reason=%s", record.Result.Case.Key(), row.Arm, obs.Action, stats.LLMCalls, stats.JevCalls, len(obs.Turn.Response.Trace), stats.JevBlocked, stats.JevFastPath, stats.JevFastPathReason)
		if row.Success || row.Disposition != "continue_recorded" {
			t.Fatal("closed collection promoted quality or ignored v1 halt")
		}
	}
}
