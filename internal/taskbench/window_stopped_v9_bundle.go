package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Pure historical DATA validation. It returns scalar liabilities and raw claim
// hashes, never an owned checkpoint, normal-window proof, or score permission.
func readWindowStoppedV9Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func(reason string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v9 bundle rejected: %s", reason)
	}
	if !reflect.DeepEqual(first, expectedStoppedV8WindowBaseline()) || m.Scope.Schema != "full-window-scope.v9" || len(m.Scope.Windows) != 511 || len(m.Scope.Slots) != 3660 || m.CampaignRoot != filepath.Join(root, "campaign") || m.Limits != windowV4ExpectedLimits() {
		return reject("predecessor/declaration")
	}
	var owner struct {
		ExitCode           *int     `json:"exit_code"`
		EndedEpoch         *float64 `json:"ended_epoch"`
		RelaunchAllowed    *bool    `json:"relaunch_allowed"`
		SupervisionFailure string   `json:"supervision_failure,omitempty"`
		SupervisionSignal  *int     `json:"supervision_signal,omitempty"`
		SupervisorExitCode *int     `json:"supervisor_exit_code,omitempty"`
	}
	// The original nullable supervision_signal is legitimate stop DATA.
	if validateWindowJSONShape(files["actual-owner-exit.json"], false) != nil {
		return reject("owner JSON")
	}
	d := json.NewDecoder(bytes.NewReader(files["actual-owner-exit.json"]))
	d.DisallowUnknownFields()
	if d.Decode(&owner) != nil || owner.ExitCode == nil || *owner.ExitCode != 1 || owner.EndedEpoch == nil || *owner.EndedEpoch <= 0 || math.IsNaN(*owner.EndedEpoch) || math.IsInf(*owner.EndedEpoch, 0) || owner.RelaunchAllowed == nil || *owner.RelaunchAllowed {
		return reject("owner stop")
	}
	used := map[string]bool{"campaign/.lock": true, "actual-owner-exit.json": true, "records/terminal.json": true, "records/startup.json": true}
	if raw, ok := files["campaign/.lock"]; !ok || len(raw) != 0 {
		return reject("owned lock data")
	}
	var startup FullWindowActualStatus
	if decodeWindowActualJSON(files["records/startup.json"], &startup) != nil || !reflect.DeepEqual(startup, FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, UnattemptedArms: 7320, GlobalUnrunTurns: 11772}) {
		return reject("original startup")
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	priorClaim, priorCheckpoint, priorSummary := "", "", ""
	var cumulative *windowV8RuntimeSummary
	var last FullWindowActualReceipt
	attempted, observed, models, jevs := 0, 0, 0, 0
	traces := map[string]bool{}
	for i := 1; i <= 56; i++ {
		cn := fmt.Sprintf("campaign/claim-%06d.json", i)
		cpn := fmt.Sprintf("campaign/checkpoint-%06d.json", i)
		ln := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		wd := fmt.Sprintf("records/window-%06d", i)
		rn, pn := wd+"/receipt.json", wd+"/payload-manifest.json"
		for _, name := range []string{cn, cpn, ln, sn, rn, pn} {
			used[name] = true
		}
		var c windowCampaignClaim
		var cp windowCampaignCheckpoint
		var r windowAcquisitionReceipt
		var s FullWindowActualReceipt
		var payload windowPayloadManifest
		for _, item := range []struct {
			name   string
			target any
		}{{cn, &c}, {cpn, &cp}, {rn, &r}, {sn, &s}, {pn, &payload}} {
			if decodeWindowActualJSON(files[item.name], item.target) != nil {
				return reject("decode " + item.name)
			}
		}
		w := m.Scope.Windows[i-1]
		if w.ID != fmt.Sprintf("window-%03d", i) || w.FirstOrdinal < 1 || w.LastOrdinal < w.FirstOrdinal || w.LastOrdinal > 390 || w.ModelCap <= 0 || w.JEVCap <= 0 || base.PermanentModel > math.MaxInt64-w.ModelCap || base.PermanentJEV > math.MaxInt64-w.JEVCap {
			return reject("window/cap bounds")
		}
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v9" || c.BaselineEvidenceSHA256 != windowV9BaselineSHA || c.BudgetApprovalSHA256 != windowStoppedV9AuthorizationSHA || c.ParentModel != 180000000 || c.ParentJEV != 2500000 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.PreviousClaimSHA256 != priorClaim || c.PreviousCheckpointSHA256 != priorCheckpoint || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject(fmt.Sprintf("claim chain %d", i))
		}
		claimSHA := conversationHash(files[cn])
		base.ClaimSHA256[fmt.Sprintf("full1220-v9-r5-20261031/window-%06d", i)] = claimSHA
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		audit, err := readWindowStoppedV9Native(wd+"/native-payloads", payload, files[ln], files, used)
		if err != nil {
			return reject(fmt.Sprintf("native %d: %v", i, err))
		}
		a := audit.State
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(r.Accounting, a) || a.ModelAttempts > w.MaxModelCalls || a.JEVAttempts > w.MaxJEVCalls || r.AccountingSource != "live_owner_snapshot" || r.LedgerCompletionProof != "unavailable_from_journal" || r.LedgerSHA256 != audit.JournalSHA256 {
			return reject("journal/contract linkage")
		}
		count, turns := 2*(w.LastOrdinal-w.FirstOrdinal+1), 0
		negative := 0
		evidence := []windowCampaignArmEvidence{}
		rowByKey := map[string]windowAcquisitionRow{}
		if len(r.Rows) != count {
			return reject("complete rows required")
		}
		for j, row := range r.Rows {
			ordinal := w.FirstOrdinal + j/2
			slot := m.Scope.Slots[ordinal-1]
			if slot.Ordinal != ordinal || slot.WindowID != w.ID || row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns || row.ObservedTurns != slot.UserTurns || !row.Attempted || !row.Finished || !row.Cleanup || !row.Observer || row.QualityEnd != nil || row.FailureEvidence != nil || row.ExcludeCompleteTaskComparison || row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || len(row.TraceID) != 32 || traces[row.TraceID] || !comparisonDigest(row.RecordingSHA256) {
				return reject("row identity/completion")
			}
			traces[row.TraceID] = true
			name := wd + "/" + row.RecordingFile
			used[name] = true
			raw := files[name]
			record, err := decodeWindowStoppedV2Recording(raw)
			if err != nil || conversationHash(raw) != row.RecordingSHA256 || record.PolicyID != collectionPolicyID || record.PolicySHA256 != collectionPolicySHA() || record.Disposition != row.Disposition || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Result.StructuralPass != row.StructuralPass || record.Result.Success != row.Success {
				return reject("original recording linkage")
			}
			resultRaw, err := json.Marshal(record.Result)
			if err != nil || conversationHash(resultRaw) != record.ResultSHA256 {
				return reject("record result digest")
			}
			if row.Status == "failed" {
				negative++
			}
			turns += row.ObservedTurns
			rowByKey[row.CaseKey+"/"+row.Arm] = row
			evidence = append(evidence, windowCampaignArmEvidence{Ordinal: ordinal, Pass: slot.Pass, ObservedTurns: row.ObservedTurns, Key: slot.CaseKey, Arm: row.Arm, RecordingSHA256: row.RecordingSHA256, Cleanup: true, Observer: true})
		}
		for _, packet := range payload.Packets {
			row, ok := rowByKey[packet.Identity.CaseKey+"/"+packet.Identity.Arm]
			if !ok || packet.Identity.Pass != row.Pass || packet.Identity.Turn < 1 || packet.Identity.Turn > row.ObservedTurns {
				return reject("native row linkage")
			}
		}
		runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: a.ModelAttempts, LogicalJEVCalls: a.JEVAttempts, NativeModelIntents: a.ModelAttempts, NativeJEVIntents: a.JEVAttempts, NativeSettlements: a.ModelAttempts + a.JEVAttempts, Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{base.PermanentModel - first.PermanentModel, base.PermanentJEV - first.PermanentJEV}, TotalPermanent: windowV8Amounts{base.PermanentModel, base.PermanentJEV}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
		if cp.Schema != "full-window-campaign-owned-checkpoint.v8" || cp.Index != i || cp.ScopeSHA256 != m.ScopeSHA256 || cp.ExecutionSHA256 != m.ExecutionSHA256 || cp.DefinitionSHA256 != m.Scope.DefinitionSHA256 || cp.ScheduleSHA256 != m.Scope.ScheduleSHA256 || cp.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || cp.ClaimSHA256 != claimSHA || cp.LedgerSHA256 != audit.JournalSHA256 || !reflect.DeepEqual(cp.Evidence, evidence) || !reflect.DeepEqual(cp.V8, &runtime) {
			return reject(fmt.Sprintf("checkpoint linkage %d", i))
		}
		// Checkpoint precedes the next claim. Receipts/summaries reflect the next
		// permanently acquired liability, including claim56 already present in H55.
		next := ""
		if i < 56 {
			nw := m.Scope.Windows[i]
			runtime.BasePermanent.Model += nw.ModelCap
			runtime.BasePermanent.JEV += nw.JEVCap
			runtime.TotalPermanent.Model += nw.ModelCap
			runtime.TotalPermanent.JEV += nw.JEVCap
			next = conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)])
		}
		if r.Schema != "full-window-acquisition-receipt.v8" || r.EvidenceKind != "recorded_run" || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != count/2 || r.PlannedCurrentArms != count || r.PlannedCurrentTurns != turns || r.AttemptedArms != count || r.FinishedArms != count || r.UnrunArms != 0 || r.ObservedTurns != turns || r.QualityNegativeArms != negative || r.QualityEndArms != 0 || r.QualityEndUnrunTurns != 0 || r.GlobalUnrunTurns != 0 || !r.EnvironmentCleanup || !r.Observer || r.OperationLimitSeconds != 300 || r.DurationMS <= 0 || math.IsNaN(r.DurationMS) || math.IsInf(r.DurationMS, 0) || r.PayloadPackets != len(payload.Packets) || r.PayloadManifestSHA256 != conversationHash(files[pn]) || r.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" || !reflect.DeepEqual(r.V8, &runtime) {
			return reject("receipt linkage")
		}
		if i < 56 {
			if r.Status != "collected_window" || !r.Cleanup || !r.Advanced || !r.PayloadVerified || r.PayloadStatus != "verified_ledger_linkage" || r.NextClaimSHA256 != next || r.FirstFailureStage != "" || r.FirstFailureOrdinal != 0 || r.FirstFailureArm != "" || r.DurationMS > 300000 {
				return reject("normal55 boundary")
			}
		} else if w.FirstOrdinal != 386 || w.LastOrdinal != 390 || count != 10 || turns != 22 || a.ModelAttempts != 18 || a.JEVAttempts != 5 || r.Status != "stopped" || r.Cleanup || r.Advanced || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.NextClaimSHA256 != "" || r.FirstFailureStage != "campaign_advance" || r.FirstFailureOrdinal != 0 || r.FirstFailureArm != "" || r.DurationMS != 300023.451 {
			return reject("stopped56 preserved negative boundary")
		}
		attempted += count
		observed += turns
		models += a.ModelAttempts
		jevs += a.JEVAttempts
		cumulative = mergeWindowV8Runtime(cumulative, &runtime)
		want := FullWindowActualStatus{V8: cumulative, EvidenceKind: "recorded_run", Closed: i == 56, NextWindowIndex: i + 1, LastReceiptSHA256: priorSummary, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: attempted, FinishedArms: attempted, UnattemptedArms: 7320 - attempted, ObservedTurns: observed, GlobalUnrunTurns: 11772 - observed, ModelAttempts: models, JEVAttempts: jevs}
		if i == 56 {
			want.NextWindowIndex = 56
		}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i || s.ReportSHA256 != conversationHash(files[rn]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || s.LedgerSHA256 != audit.JournalSHA256 || s.ReportStatus != r.Status || s.FirstUnrunOrdinal != w.LastOrdinal+1 || s.FutureUnrunFromOrdinal != w.LastOrdinal+1 || s.FirstUnrunArm != m.Scope.Slots[w.LastOrdinal].ArmOrder[0] {
			return reject(fmt.Sprintf("summary chain %d", i))
		}
		priorClaim, priorCheckpoint, priorSummary = claimSHA, conversationHash(files[cpn]), conversationHash(files[sn])
		last = s
	}
	var terminal FullWindowActualStatus
	want := last.FullWindowActualStatus
	want.LastReceiptSHA256 = priorSummary
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil || !reflect.DeepEqual(terminal, want) || attempted != 780 || observed != 1290 || models != 1580 || jevs != 390 {
		return reject("closed incomplete terminal totals")
	}
	if len(files) != len(used) {
		return reject("unreferenced/later owned path")
	}
	for name := range files {
		if !used[name] {
			return reject("unreferenced/later owned path " + name)
		}
	}
	if base.PermanentModel != 66472061 || base.PermanentJEV != 631114 || base.PermanentModel > base.TotalModel || base.PermanentJEV > base.TotalJEV {
		return reject("permanent102")
	}
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	base.ActualModelCalls += models
	base.ActualJEVCalls += jevs
	return base, nil
}

func readWindowStoppedV9Native(dir string, payload windowPayloadManifest, ledger []byte, files map[string][]byte, used map[string]bool) (windowAccountingAudit, error) {
	reject := func(reason string) (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("settled native data %s", reason)
	}
	audit, err := readWindowAccountingBytes(ledger)
	a := audit.State
	if err != nil || audit.Contract.Schema != windowAccountingV3 || !reflect.DeepEqual(audit.Contract, payload.Contract) || !a.Closed || a.Halt != "" || a.EnvelopeViolation || a.Pending != nil || a.RetryRequired != nil || len(a.UnknownIntents) != 0 || a.ModelHeld != 0 || a.JEVHeld != 0 || a.ModelUnknownHeld != 0 || a.JEVUnknownHeld != 0 || a.ModelExtraFunded != 0 || a.JEVExtraFunded != 0 || payload.Schema != "full-window-payload.v2" || payload.EvidenceKind != "recorded_run" || payload.CompletionProof != "unavailable_from_payload_capture" || len(payload.Packets) < 1 || len(payload.Packets) > 260 {
		return reject("contract/state")
	}
	get := func(name string) []byte { used[dir+"/"+name] = true; return files[dir+"/"+name] }
	var store windowPayloadManifest
	if decodeWindowActualJSON(get("store.json"), &store) != nil || store.Schema != payload.Schema || store.EvidenceKind != payload.EvidenceKind || store.CompletionProof != payload.CompletionProof || !reflect.DeepEqual(store.Contract, payload.Contract) || len(store.Packets) != 0 {
		return reject("store")
	}
	intents, settles := map[int]windowAccountingEvent{}, map[int]windowAccountingEvent{}
	for _, line := range bytes.Split(bytes.TrimSuffix(ledger, []byte("\n")), []byte("\n")) {
		var event windowAccountingEvent
		if json.Unmarshal(line, &event) != nil {
			return reject("event")
		}
		switch event.Kind {
		case "intent":
			intents[event.Sequence] = event
		case "settle":
			settles[event.Sequence] = event
		case "config", "close":
		default:
			return reject("retry/unknown/extra event")
		}
	}
	if len(intents) != len(payload.Packets) || len(settles) != len(payload.Packets) || a.ModelAttempts+a.JEVAttempts != len(payload.Packets) {
		return reject("complete native coverage")
	}
	for i, packet := range payload.Packets {
		seq := i + 1
		id := packet.Identity
		c := payload.Contract
		intent, ok := intents[seq]
		settle, settled := settles[seq]
		if !ok || !settled || intent.Request == nil || *intent.Request != id || packet.Sequence != seq || packet.ScopeSHA256 != c.ScopeSHA256 || packet.ClaimSHA256 != c.ClaimSHA256 || packet.WindowID != c.WindowID || c.validateRequest(id) != nil || id.NativeAttempt != 1 || id.Attempt > 5 || id.Provider == "jev" && id.Attempt != 1 || packet.FailureDescriptorSHA256 != "" {
			return reject("native identity")
		}
		requestName, responseName := fmt.Sprintf("request-%06d", seq), fmt.Sprintf("response-%06d", seq)
		if packet.RequestDescriptorFile != requestName+".json" || packet.Request.File != requestName+".body" || !packet.Request.Complete || packet.Request.PrefixSHA256 != "" || packet.Request.FullSHA256 != id.BodySHA256 || packet.Request.Bytes != id.Bytes {
			return reject("request descriptor")
		}
		raw := get(requestName + ".json")
		var request windowPayloadPacket
		want := packet
		want.Response = nil
		if decodeWindowActualJSON(raw, &request) != nil || !reflect.DeepEqual(request, want) {
			return reject("request packet")
		}
		limits := c.Model
		if id.Provider == "jev" {
			limits = c.JEV
		}
		endpoint, _, err := windowProviderEndpoint(id.Provider)
		body := get(requestName + ".body")
		if err != nil || validateWindowPOST(id.Provider, "POST", endpoint, body, limits, id) != nil {
			return reject("request body")
		}
		r := packet.Response
		if r == nil || r.DescriptorFile != responseName+".json" || r.TransportFailure != nil || !r.Available || !r.Complete || r.Status != 200 || r.Code != "complete" || r.Body == nil || r.Body.File != responseName+".body" || !r.Body.Complete || r.Body.PrefixSHA256 != "" || !comparisonDigest(r.Body.FullSHA256) {
			return reject("complete response")
		}
		var response windowPayloadPacket
		want = packet
		want.Request = windowPayloadBody{}
		want.RequestDescriptorFile = ""
		if decodeWindowActualJSON(get(responseName+".json"), &response) != nil || !reflect.DeepEqual(response, want) {
			return reject("response packet")
		}
		responseBody := get(responseName + ".body")
		if len(responseBody) != r.Body.Bytes || conversationHash(responseBody) != r.Body.FullSHA256 || settle.ResponseSHA256 != r.Body.FullSHA256 {
			return reject("response bytes/hash")
		}
		usage, err := parseWindowResponseUsage(id.Provider, responseBody, limits)
		if err != nil || !reflect.DeepEqual(usage, settle.Usage) {
			return reject("fresh usage/cache")
		}
	}
	return audit, nil
}

// Explicit, bounded inventory. Every file is verified from its supplying FD,
// retained only in its owned pass, and registered for fresh Finish.
// Only declared campaign/records namespaces are enumerated; no settings/cache walk.
func readWindowStoppedV9InventoryWithPass(p *historyReadPass, root string, inv windowStoppedV8Inventory) (map[string][]byte, map[string]os.FileInfo, error) {
	reject := func(reason string) (map[string][]byte, map[string]os.FileInfo, error) {
		return nil, nil, fmt.Errorf("stopped v9 inventory rejected: %s", reason)
	}
	if p == nil || inv.Root != root || !filepath.IsAbs(root) || filepath.Clean(root) != root || len(inv.Files) == 0 || len(inv.Files) > 9055 {
		return reject("owned pass/root/count")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return reject("root alias")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return reject("root identity")
	}
	files, identities := map[string][]byte{}, map[string]os.FileInfo{".": rootInfo}
	dirs := map[string]map[string]bool{}
	names := make([]string, 0, len(inv.Files))
	for name := range inv.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := inv.Files[name]
		if name == "." || !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "\\") || !(strings.HasPrefix(name, "campaign/") || strings.HasPrefix(name, "records/")) || entry.Bytes < 0 || entry.Bytes > 8<<20 || !comparisonDigest(entry.SHA256) {
			return reject("entry path/size/hash")
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		canonical, err = filepath.EvalSymlinks(path)
		if err != nil || canonical != path {
			return reject("entry alias " + name)
		}
		before, err := os.Lstat(path)
		if err != nil || !before.Mode().IsRegular() || before.Size() != entry.Bytes || !windowStoppedV7SingleLink(before) {
			return reject("entry missing/type/size/link " + name)
		}
		raw, err := historyStoppedV7File(p, path, entry.SHA256, max(1, entry.Bytes))
		after, ae := os.Lstat(path)
		if err != nil || ae != nil || int64(len(raw)) != entry.Bytes || validateWindowStoppedV7ReadMetadata(before, before, after, after) != nil || after.Size() != entry.Bytes {
			return reject("original bytes/identity " + name)
		}
		files[name], identities[name] = raw, after
		parts := strings.Split(name, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			if dirs[dir] == nil {
				dirs[dir] = map[string]bool{}
			}
			isDir := i < len(parts)-1
			if old, ok := dirs[dir][parts[i]]; ok && old != isDir {
				return reject("namespace conflict")
			}
			dirs[dir][parts[i]] = isDir
		}
	}
	dirNames := make([]string, 0, len(dirs))
	for dir := range dirs {
		dirNames = append(dirNames, dir)
	}
	sort.Strings(dirNames)
	for _, dir := range dirNames {
		path := filepath.Join(root, filepath.FromSlash(dir))
		before, err := os.Lstat(path)
		if err != nil || !before.IsDir() || historyStoppedDir(p, path, dirs[dir]) != nil {
			return reject("owned namespace " + dir)
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			return reject("owned directory changed")
		}
		identities[dir] = after
	}
	after, err := os.Lstat(root)
	if err != nil || !os.SameFile(rootInfo, after) {
		return reject("root replaced")
	}
	return files, identities, nil
}
