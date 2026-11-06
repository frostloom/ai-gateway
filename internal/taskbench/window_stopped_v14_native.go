package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

func readWindowStoppedV14Native(dir string, payload windowPayloadManifest, ledger []byte, files map[string][]byte, used map[string]bool) (windowAccountingAudit, error) {
	reject := func(reason string) (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("stopped14 pending native data %s", reason)
	}
	audit, err := readWindowAccountingBytes(ledger)
	a := audit.State
	if err != nil || audit.Contract.Schema != windowAccountingV3 || !reflect.DeepEqual(audit.Contract, payload.Contract) || !a.Closed || a.Halt != "upstream_failure" || a.EnvelopeViolation || a.Pending == nil || a.Pending.Sequence != 10 || a.RetryRequired != nil || len(a.UnknownIntents) != 0 || a.ModelHeld != 0 || a.JEVHeld != 2688 || a.ModelUnknownHeld != 0 || a.JEVUnknownHeld != 0 || a.ModelExtraFunded != 0 || a.JEVExtraFunded != 0 || payload.Schema != "full-window-payload.v2" || payload.EvidenceKind != "recorded_run" || payload.CompletionProof != "unavailable_from_payload_capture" || len(payload.Packets) != 10 {
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
		case "halt":
			if event.Code != "upstream_failure" {
				return reject("unrecognized halt")
			}
		default:
			return reject("retry/unknown/extra event")
		}
	}
	if len(intents) != len(payload.Packets) || len(settles) != 9 || a.ModelAttempts+a.JEVAttempts != len(payload.Packets) {
		return reject("complete native coverage")
	}
	for i, packet := range payload.Packets {
		seq := i + 1
		id := packet.Identity
		c := payload.Contract
		intent, ok := intents[seq]
		settle, settled := settles[seq]
		if !ok || settled != (seq < 10) || intent.Request == nil || *intent.Request != id || packet.Sequence != seq || packet.ScopeSHA256 != c.ScopeSHA256 || packet.ClaimSHA256 != c.ClaimSHA256 || packet.WindowID != c.WindowID || c.validateRequest(id) != nil || id.NativeAttempt != 1 || id.Attempt > 5 || id.Provider == "jev" && id.Attempt != 1 || seq < 10 && packet.FailureDescriptorSHA256 != "" {
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
		want.FailureDescriptorSHA256 = ""
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

		if seq == 10 {
			if packet.FailureDescriptorSHA256 != "cf7f31a93ae1bd9364eaeb66971dc21de8e6ae212d48778d4c1440b33fd105cc" || conversationHash(files[dir+"/"+responseName+".json"]) != packet.FailureDescriptorSHA256 {
				return reject("pending failure anchor")
			}
			r := packet.Response
			if id.Provider != "jev" || id.CaseKey != "change_plan-067/answer_complete" || id.Pass != 1 || id.Arm != "jev_selective" || id.Turn != 1 || id.Attempt != 1 || id.BodySHA256 != "37535580b459865ec8fc83f5e8d9d854c0f1dc8b56f28ea5c8bcb6fd34c0d78f" || id.Bytes != 2565 || !reflect.DeepEqual(a.Pending.Request, id) {
				return reject("exact pending identity")
			}
			if r == nil || r.Status != 0 || r.Code != "round_trip" || r.Complete || r.Available || r.Body != nil || r.DescriptorFile != responseName+".json" || r.TransportFailure == nil {
				return reject("pending response boundary")
			}
			f := r.TransportFailure
			if f.Cause != "timeout" || f.Phase != "tls_handshake" || f.ElapsedMS != 1152 || !f.TraceObserved || f.ConnectionReused || f.RequestWritten || f.FirstByte {
				return reject("pending transport boundary")
			}
			var response windowPayloadPacket
			want := packet
			want.Request = windowPayloadBody{}
			want.RequestDescriptorFile = ""
			want.FailureDescriptorSHA256 = ""
			if decodeWindowActualJSON(get(responseName+".json"), &response) != nil || !reflect.DeepEqual(response, want) {
				return reject("pending descriptor")
			}
			if _, ok := files[dir+"/"+responseName+".body"]; ok {
				return reject("pending response body")
			}
			continue
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
	if a.Pending == nil || a.ModelAttempts != 8 || a.JEVAttempts != 2 || a.ModelKnown != 7033 || a.JEVKnown != 51 || a.ModelHeld != 0 || a.JEVHeld != 2688 {
		return reject("unexpected pending hold")
	}
	return audit, nil
}
