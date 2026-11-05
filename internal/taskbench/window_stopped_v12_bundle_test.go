package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Portable two-window data, constructed through the real v1 recorder and real
// accounting journal codec. It never reads frozen originals or uses providers.
func stoppedV12BundleFixture(t *testing.T) (string, windowActualManifest, map[string][]byte) {
	t.Helper()
	suite, profiles, bindings := windowScopeInputs(t)
	scope, e := prepareFullWindowScopeV12(suite, profiles, windowV9TestCaps(t)[55:], bindings, windowV12BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	root := "/synthetic/root4"
	m := windowActualManifest{Scope: scope, ScopeSHA256: strings.Repeat("a", 64), ExecutionSHA256: strings.Repeat("b", 64), CampaignRoot: root + "/campaign", Limits: windowV4ExpectedLimits()}
	m.Observer.SpoolRunID = strings.Repeat("c", 64)
	m.Observer.SpoolRootSHA256 = strings.Repeat("d", 64)
	files := map[string][]byte{}
	put := func(n string, v any) { stoppedV9Put(t, files, n, v) }
	put("actual-owner-group-cleanup.json", map[string]any{"group_empty": true, "leader_reaped": true, "confirmed": true, "owned_pgid": 68358, "error_type": "PermissionError", "errors": [][]string{{"probe", "PermissionError"}, {"SIGTERM", "PermissionError"}, {"probe", "PermissionError"}}})
	put("actual-owner-exit.json", map[string]any{"exit_code": 1, "relaunch_allowed": false})
	first := expectedStoppedV11WindowBaseline()
	model, jev := first.PermanentModel, first.PermanentJEV
	priorClaim, priorCP, priorSummary := "", "", ""
	totalModel, totalJEV, totalAt, totalFin, totalObs := 0, 0, 0, 0, 0
	var cumulative *windowV8RuntimeSummary
	var last FullWindowActualStatus
	cases := map[string]ConversationCase{}
	for _, c := range suite.Cases {
		cases[c.Key()] = c
	}
	observation := func(closed bool) *windowObservationReceiptV11 {
		o := &windowObservationReceiptV11{Boundary: "durable_local", RemoteDelivery: "pending"}
		o.Spool.RunID = m.Observer.SpoolRunID
		o.Spool.RootSHA256 = m.Observer.SpoolRootSHA256
		o.Spool.MaxBytes = 4 << 30
		o.Spool.Closed = closed
		return o
	}
	put("records/startup.json", FullWindowActualStatus{Observation: observation(false), EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, UnattemptedArms: 6550, GlobalUnrunTurns: 10504})
	for i := 1; i <= 2; i++ {
		w := scope.Windows[i-1]
		model += w.ModelCap
		jev += w.JEVCap
		c := windowCampaignClaim{Schema: "full-window-campaign-claim.v12", BaselineEvidenceSHA256: windowV12BaselineSHA, BudgetApprovalSHA256: windowStoppedV12AuthorizationSHA, ParentModel: 195000000, ParentJEV: 3000000, Root: m.CampaignRoot, Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: scope.DefinitionSHA256, ManifestSHA256: scope.ManifestSHA256, SelectionSHA256: scope.SelectionSHA256, ProfileSHA256: scope.ProfileSHA256, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, PreviousClaimSHA256: priorClaim, PreviousCheckpointSHA256: priorCP, Window: w, HistoricalClaims: first.ClaimSHA256, ModelPermanent: model, JEVPermanent: jev}
		cn := fmt.Sprintf("campaign/claim-%06d.json", i)
		put(cn, c)
		claimSHA := conversationHash(files[cn])
		priorClaim = claimSHA
		wd := fmt.Sprintf("records/window-%06d", i)
		nd := wd + "/native-payloads"
		contract := contractWindowCampaign(scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		payload := windowPayloadManifest{Schema: "full-window-payload.v2", EvidenceKind: "recorded_run", CompletionProof: "unavailable_from_payload_capture", Contract: contract, Packets: []windowPayloadPacket{}}
		put(nd+"/store.json", payload)
		var ledger []byte
		eventIndex := 0
		previous := ""
		appendEvent := func(v windowAccountingEvent) {
			v.Index = eventIndex
			v.PreviousSHA256 = previous
			raw, _ := json.Marshal(v)
			ledger = append(ledger, raw...)
			ledger = append(ledger, '\n')
			previous = conversationHash(raw)
			eventIndex++
		}
		appendEvent(windowAccountingEvent{Kind: "config", Contract: &contract})
		sequence := 0
		addPacket := func(provider string, row windowAcquisitionRow, turn int) {
			sequence++
			request := []byte(`{"model":"deepseek-flash","max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"synthetic"}]}],"thinking":{"type":"disabled"}}`)
			response := []byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`)
			mod := "deepseek-flash"
			usage := &windowUsage{Input: windowInt(1), Output: windowInt(1), CacheHit: windowInt(0), CacheMiss: windowInt(1)}
			if provider == "jev" {
				mod = "jev-1.13.0"
				request = []byte(`{"model":"jev-1.13.0","state":"synthetic","questions":{"q":{"type":"noul"}}}`)
				response = []byte(`{"model":"jev-1.13.0","usage":{"input_tokens":1,"output_tokens":1}}`)
				usage = &windowUsage{Input: windowInt(1), Output: windowInt(1)}
			}
			id := windowRequestIdentity{Provider: provider, Model: mod, CaseKey: row.CaseKey, Pass: row.Pass, Arm: row.Arm, Turn: turn, Attempt: 1, NativeAttempt: 1, BodySHA256: conversationHash(request), Bytes: len(request)}
			appendEvent(windowAccountingEvent{Kind: "intent", Sequence: sequence, Request: &id})
			appendEvent(windowAccountingEvent{Kind: "settle", Sequence: sequence, ResponseSHA256: conversationHash(response), Usage: usage})
			req, res := fmt.Sprintf("request-%06d", sequence), fmt.Sprintf("response-%06d", sequence)
			p := windowPayloadPacket{Sequence: sequence, Identity: id, ScopeSHA256: m.ScopeSHA256, ClaimSHA256: claimSHA, WindowID: w.ID, RequestDescriptorFile: req + ".json", Request: windowPayloadBody{File: req + ".body", Bytes: len(request), Complete: true, FullSHA256: id.BodySHA256}, Response: &windowPayloadResponse{Status: 200, Code: "complete", Complete: true, Available: true, DescriptorFile: res + ".json", Body: &windowPayloadBody{File: res + ".body", Bytes: len(response), Complete: true, FullSHA256: conversationHash(response)}}}
			rq := p
			rq.Response = nil
			put(nd+"/"+req+".json", rq)
			rs := p
			rs.Request = windowPayloadBody{}
			rs.RequestDescriptorFile = ""
			put(nd+"/"+res+".json", rs)
			files[nd+"/"+req+".body"] = request
			files[nd+"/"+res+".body"] = response
			payload.Packets = append(payload.Packets, p)
		}
		rows := []windowAcquisitionRow{}
		evidence := []windowCampaignArmEvidence{}
		at, fin, obs, modelLeft := 0, 0, 0, map[bool]int{false: 17, true: 15}[i == 2]
		for ordinal := w.FirstOrdinal; ordinal <= w.LastOrdinal; ordinal++ {
			slot := scope.Slots[ordinal-1]
			for _, arm := range slot.ArmOrder {
				j := len(rows)
				row := windowAcquisitionRow{Ordinal: ordinal, Pass: slot.Pass, DefinedTurns: slot.UserTurns, CaseKey: slot.CaseKey, Arm: arm}
				if i == 2 && j >= 6 {
					row.Status = "not_started"
					rows = append(rows, row)
					continue
				}
				driver := collectionUnmetDriver()
				if i == 2 && j == 5 {
					driver.steps[0].Action = "preview"
					driver.steps[0].Turn.Response.Trace = []Trace{{Tool: "change_plan", Phase: "preview", Outcome: "failure", ResolvedArgs: `{"plan_name":"pro plan"}`}}
				}
				testCase := cases[slot.CaseKey]
				testCase.Scenario.Profile = "no_subscription"
				record := recordCollectionConversation(context.Background(), testCase, driver)
				if i == 2 && j == 5 {
					if record.Disposition != "halt" {
						t.Fatal("synthetic v1 failure did not halt")
					}
				} else if record.Disposition != "continue_recorded" {
					t.Fatalf("safe unmet row%d/%d halt: %+v", i, j, record.Result.Errors)
				}
				row.StructuralPass = record.Result.StructuralPass
				row.Success = record.Result.Success
				row.Attempted = true
				row.Finished = !(i == 2 && j == 5)
				row.Cleanup = row.Finished
				row.Observer = row.Finished
				row.ObservedTurns = len(record.Result.Observations)
				row.Status = record.Result.Status
				row.Disposition = record.Disposition
				row.TraceID = fmt.Sprintf("%032x", ordinal*2+j%2)
				row.RecordingFile = fmt.Sprintf("arm-%06d-%s.json", ordinal, arm)
				put(wd+"/"+row.RecordingFile, record)
				row.RecordingSHA256 = conversationHash(files[wd+"/"+row.RecordingFile])
				at++
				if row.Finished {
					fin++
				}
				obs += row.ObservedTurns
				evidence = append(evidence, windowCampaignArmEvidence{Ordinal: ordinal, Pass: slot.Pass, ObservedTurns: row.ObservedTurns, Key: slot.CaseKey, Arm: arm, RecordingSHA256: row.RecordingSHA256, Cleanup: row.Cleanup, Observer: row.Observer})
				for turn := 1; turn <= row.ObservedTurns && modelLeft > 0; turn++ {
					addPacket("model", row, turn)
					modelLeft--
				}
				if arm == "jev_selective" {
					addPacket("jev", row, 1)
				}
				rows = append(rows, row)
			}
		}
		if modelLeft != 0 {
			t.Fatal("missing synthetic model packet")
		}
		halt := ""
		if i == 2 {
			halt = "invalid_request"
			appendEvent(windowAccountingEvent{Kind: "halt", Code: halt})
		}
		appendEvent(windowAccountingEvent{Kind: "close"})
		ln := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		files[ln] = ledger
		audit, e := readWindowAccountingBytes(ledger)
		if e != nil {
			t.Fatal(e)
		}
		a := audit.State
		put(wd+"/payload-manifest.json", payload)
		runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: a.ModelAttempts, LogicalJEVCalls: a.JEVAttempts, NativeModelIntents: a.ModelAttempts, NativeJEVIntents: a.JEVAttempts, NativeSettlements: sequence, Halt: halt, Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{model - first.PermanentModel, jev - first.PermanentJEV}, TotalPermanent: windowV8Amounts{model, jev}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
		r := windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt.v12", EvidenceKind: "recorded_run", Status: "stopped", ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, WindowID: w.ID, WindowIndex: i, PlannedCampaignPairs: 3275, PlannedCampaignArms: 6550, PlannedCampaignTurns: 10504, PlannedCurrentPairs: w.LastOrdinal - w.FirstOrdinal + 1, PlannedCurrentArms: len(rows), PlannedCurrentTurns: 22, AttemptedArms: at, FinishedArms: fin, UnrunArms: len(rows) - at, ObservedTurns: obs, Rows: rows, EnvironmentCleanup: true, Accounting: a, AccountingSource: "live_owner_snapshot", LedgerSHA256: audit.JournalSHA256, LedgerCompletionProof: "unavailable_from_journal", OperationLimitSeconds: 300, PayloadManifestSHA256: conversationHash(files[wd+"/payload-manifest.json"]), PayloadPackets: sequence, PayloadStatus: "audit_only", Observation: observation(false)}
		if i == 1 {
			cp := windowCampaignCheckpoint{Schema: "full-window-campaign-owned-checkpoint.v12", Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, ClaimSHA256: claimSHA, LedgerSHA256: audit.JournalSHA256, Evidence: evidence, V8: &runtime}
			put("campaign/checkpoint-000001.json", cp)
			priorCP = conversationHash(files["campaign/checkpoint-000001.json"])
			nw := scope.Windows[i]
			runtime.BasePermanent.Model += nw.ModelCap
			runtime.BasePermanent.JEV += nw.JEVCap
			runtime.TotalPermanent.Model += nw.ModelCap
			runtime.TotalPermanent.JEV += nw.JEVCap
			r.Status = "collected_window"
			r.Cleanup = true
			r.Observer = true
			r.Advanced = true
			r.PayloadVerified = true
			r.PayloadStatus = "verified_ledger_linkage"
		} else {
			r.FirstFailureStage = "arm_proof"
			r.FirstFailureOrdinal = 393
			r.FirstFailureArm = "jev_selective"
			r.ObserverCountersCumulative.Accepted = 160
		}
		r.V8 = &runtime
		put(wd+"/receipt.json", r)
		totalModel += a.ModelAttempts
		totalJEV += a.JEVAttempts
		totalAt += at
		totalFin += fin
		totalObs += obs
		cumulative = mergeWindowV8Runtime(cumulative, &runtime)
		last = FullWindowActualStatus{Observation: observation(false), V8: cumulative, EvidenceKind: "recorded_run", Closed: i == 2, NextWindowIndex: min(i+1, 2), LastReceiptSHA256: priorSummary, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, AttemptedArms: totalAt, FinishedArms: totalFin, FailedAttemptedArms: totalAt - totalFin, UnattemptedArms: 6550 - totalAt, ObservedTurns: totalObs, GlobalUnrunTurns: 10504 - totalObs, ModelAttempts: totalModel, JEVAttempts: totalJEV}
		firstUnrun := w.LastOrdinal + 1
		firstArm := scope.Slots[firstUnrun-1].ArmOrder[0]
		future := firstUnrun
		if i == 2 {
			firstUnrun = 394
			firstArm = "jev_selective"
			future = 395
		}
		summary := FullWindowActualReceipt{FirstUnrunOrdinal: firstUnrun, FirstUnrunArm: firstArm, FutureUnrunFromOrdinal: future, FullWindowActualStatus: last, WindowIndex: i, ReportSHA256: conversationHash(files[wd+"/receipt.json"]), PayloadManifestSHA256: r.PayloadManifestSHA256, LedgerSHA256: r.LedgerSHA256, ReportStatus: r.Status}
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		put(sn, summary)
		priorSummary = conversationHash(files[sn])
	}
	// Link receipt1 to the now-known second claim; refresh its summary and chain.
	var r windowAcquisitionReceipt
	json.Unmarshal(files["records/window-000001/receipt.json"], &r)
	r.NextClaimSHA256 = conversationHash(files["campaign/claim-000002.json"])
	put("records/window-000001/receipt.json", r)
	var s FullWindowActualReceipt
	json.Unmarshal(files["records/window-000001-summary.json"], &s)
	s.ReportSHA256 = conversationHash(files["records/window-000001/receipt.json"])
	put("records/window-000001-summary.json", s)
	json.Unmarshal(files["records/window-000002-summary.json"], &s)
	s.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	put("records/window-000002-summary.json", s)
	last.LastReceiptSHA256 = conversationHash(files["records/window-000002-summary.json"])
	last.Observation = observation(true)
	put("records/terminal.json", last)
	for len(files) < 323 {
		files[fmt.Sprintf("ancillary-%03d", len(files))] = []byte("synthetic pinned ancillary")
	}
	if len(files) != 323 {
		t.Fatal("fixture exceeds fixed323")
	}
	return root, m, files
}

func TestWindowStoppedV12SyntheticBundlePreservesV1Stop(t *testing.T) {
	root, m, files := stoppedV12BundleFixture(t)
	got, e := readWindowStoppedV12Bundle(root, m, expectedStoppedV11WindowBaseline(), files)
	if e != nil {
		t.Fatal(e)
	}
	if got.PermanentModel != 79974331 || got.PermanentJEV != 845904 || got.ActualModelCalls != 6691 || got.ActualJEVCalls != 1685 || len(got.ClaimSHA256) != 156 {
		t.Fatal("derived accounting mismatch", got)
	}
}

func TestWindowStoppedV12BundleRejectsMutation(t *testing.T) {
	for _, name := range []string{"startup-closed", "first-unrun", "orphan-native", "quality-exclusion", "terminal-normal", "terminal-done", "terminal-calls", "stop-cleanup", "stop-advanced", "stop-payload", "stop-next-claim", "stop-accounting", "stop-recording", "claim-history", "checkpoint", "native-body", "owner-relaunch", "permission-diagnostic"} {
		t.Run(name, func(t *testing.T) {
			root, m, files := stoppedV12BundleFixture(t)
			change := func(path string, mutate func(map[string]any)) {
				var v map[string]any
				if e := json.Unmarshal(files[path], &v); e != nil {
					t.Fatal(e)
				}
				mutate(v)
				stoppedV9Put(t, files, path, v)
			}
			switch name {
			case "startup-closed":
				change("records/startup.json", func(v map[string]any) { v["closed"] = true })
			case "first-unrun":
				change("records/window-000002-summary.json", func(v map[string]any) { v["first_unrun_ordinal"] = 395 })
			case "orphan-native":
				for k := range files {
					if strings.HasPrefix(k, "ancillary-") {
						delete(files, k)
						break
					}
				}
				files["records/window-000002/native-payloads/response-999999.body"] = []byte("fabricated")
			case "quality-exclusion":
				change("records/window-000002/receipt.json", func(v map[string]any) {
					v["rows"].([]any)[5].(map[string]any)["exclude_complete_task_comparison"] = true
				})
			case "terminal-normal":
				change("records/terminal.json", func(v map[string]any) { v["observation"].(map[string]any)["normal_closure"] = true })
			case "terminal-done":
				change("records/terminal.json", func(v map[string]any) { v["acquisition_done"] = true })
			case "terminal-calls":
				change("records/terminal.json", func(v map[string]any) { v["model_attempts"] = 31 })
			case "stop-cleanup", "stop-advanced", "stop-payload", "stop-next-claim", "stop-accounting":
				change("records/window-000002/receipt.json", func(v map[string]any) {
					switch name {
					case "stop-cleanup":
						v["cleanup"] = true
					case "stop-advanced":
						v["advanced"] = true
					case "stop-payload":
						v["payload_verified"] = true
					case "stop-next-claim":
						v["next_claim_sha256"] = strings.Repeat("f", 64)
					case "stop-accounting":
						v["accounting"].(map[string]any)["halt"] = ""
					}
				})
			case "stop-recording":
				change("records/window-000002/arm-000393-jev_selective.json", func(v map[string]any) { v["policy_id"] = "full-customer-recording-policy.v2" })
			case "claim-history":
				change("campaign/claim-000002.json", func(v map[string]any) { v["historical_claims"] = map[string]any{} })
			case "checkpoint":
				change("campaign/checkpoint-000001.json", func(v map[string]any) { v["claim_sha256"] = strings.Repeat("f", 64) })
			case "native-body":
				files["records/window-000002/native-payloads/response-000001.body"] = []byte("changed")
			case "owner-relaunch":
				change("actual-owner-exit.json", func(v map[string]any) { v["relaunch_allowed"] = true })
			case "permission-diagnostic":
				change("actual-owner-group-cleanup.json", func(v map[string]any) { delete(v, "errors") })
			}
			if _, e := readWindowStoppedV12Bundle(root, m, expectedStoppedV11WindowBaseline(), files); e == nil {
				t.Fatal("changed stopped DATA admitted")
			}
		})
	}
}

func TestWindowStoppedV12NativeAllSettledCollectionHalt(t *testing.T) {
	_, _, files := stoppedV12BundleFixture(t)
	for _, i := range []int{1, 2} {
		wd := fmt.Sprintf("records/window-%06d", i)
		var p windowPayloadManifest
		if e := json.Unmarshal(files[wd+"/payload-manifest.json"], &p); e != nil {
			t.Fatal(e)
		}
		ledger := files[fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)]
		got, e := readWindowStoppedV12Native(wd+"/native-payloads", p, ledger, files, map[string]bool{}, i == 2)
		if e != nil || got.State.Pending != nil || len(got.State.UnknownIntents) != 0 || got.State.RetryRequired != nil || got.State.ModelAttempts+got.State.JEVAttempts != len(p.Packets) {
			t.Fatal("all settled replay", e)
		}
		if i == 2 && got.State.Halt != "invalid_request" {
			t.Fatal("collection halt rewritten")
		}
		for _, name := range []string{"halt-kind", "response", "settlement-body", "packet-identity", "retry", "unknown", "request-body", "missing-packet"} {
			t.Run(fmt.Sprintf("%d/%s", i, name), func(t *testing.T) {
				raw, _ := json.Marshal(p)
				var v windowPayloadManifest
				json.Unmarshal(raw, &v)
				l := append([]byte{}, ledger...)
				fs := map[string][]byte{}
				for k, b := range files {
					fs[k] = b
				}
				switch name {
				case "halt-kind":
					if i == 1 {
						return
					}
					var events []windowAccountingEvent
					for _, line := range strings.Split(strings.TrimSuffix(string(l), "\n"), "\n") {
						var e windowAccountingEvent
						json.Unmarshal([]byte(line), &e)
						if e.Kind == "halt" {
							e.Code = "upstream_failure"
						}
						events = append(events, e)
					}
					l = nil
					previous := ""
					for _, e := range events {
						e.PreviousSHA256 = previous
						b, _ := json.Marshal(e)
						l = append(l, b...)
						l = append(l, '\n')
						previous = conversationHash(b)
					}
				case "response":
					v.Packets[0].Response.Available = false
				case "settlement-body":
					fs[wd+"/native-payloads/response-000001.body"] = []byte("different")
				case "packet-identity":
					v.Packets[0].Identity.Pass++
				case "retry":
					v.Packets[0].Identity.NativeAttempt = 2
				case "unknown":
					v.Packets[0].FailureDescriptorSHA256 = strings.Repeat("f", 64)
				case "request-body":
					fs[wd+"/native-payloads/request-000001.body"] = []byte("different")
				case "missing-packet":
					v.Packets = v.Packets[1:]
				}
				if _, e := readWindowStoppedV12Native(wd+"/native-payloads", v, l, fs, map[string]bool{}, i == 2); e == nil {
					t.Fatal("native mutation admitted")
				}
			})
		}
	}
}
