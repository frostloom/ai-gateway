package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

func readWindowStoppedV3Files(root string) (map[string][]byte, error) {
	return readWindowStoppedV3FilesWithPass(nil, root)
}

func readWindowStoppedV3FilesWithPass(p *historyReadPass, root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for name, sha := range windowStoppedV3ArtifactPins() {
		raw, err := historyStoppedFile(p, filepath.Join(root, filepath.FromSlash(name)), sha, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("stopped v3 original top artifact changed: %s", name)
		}
		files[name] = raw
	}
	return files, nil
}

// The old nullable record decoder and payload-byte reader are reused unchanged.
// Here the new native responses must additionally have exact canonical ledger
// intent+settle identity, retained response SHA and freshly parsed native usage.
// This proves configured accounting, not a provider invoice or paid capability.
func readWindowStoppedV3Native(dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	return readWindowStoppedV3NativeWithPass(nil, dir, payload, ledger)
}

func readWindowStoppedV3NativeWithPass(p *historyReadPass, dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	reject := func() (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("stopped v3 native/ledger linkage rejected")
	}
	audit, err := readWindowAccountingBytes(ledger)
	if err != nil || !reflect.DeepEqual(audit.Contract, payload.Contract) || !audit.State.Closed || audit.State.Pending != nil || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.EnvelopeViolation {
		return reject()
	}
	if err = readWindowStoppedV2PayloadWithPass(p, dir, payload); err != nil {
		return windowAccountingAudit{}, err
	}
	intents, settles := map[int]windowAccountingEvent{}, map[int]windowAccountingEvent{}
	for _, line := range bytes.Split(bytes.TrimSuffix(ledger, []byte("\n")), []byte("\n")) {
		var event windowAccountingEvent
		if json.Unmarshal(line, &event) != nil {
			return reject()
		}
		switch event.Kind {
		case "intent":
			if _, ok := intents[event.Sequence]; ok {
				return reject()
			}
			intents[event.Sequence] = event
		case "settle":
			if _, ok := settles[event.Sequence]; ok {
				return reject()
			}
			settles[event.Sequence] = event
		}
	}
	if len(intents) != len(payload.Packets) || len(settles) != len(payload.Packets) {
		return reject()
	}
	for _, packet := range payload.Packets {
		intent, ok := intents[packet.Sequence]
		settle, settled := settles[packet.Sequence]
		if !ok || !settled || intent.Request == nil || *intent.Request != packet.Identity || packet.Response == nil || !packet.Response.Available || !packet.Response.Complete || packet.Response.Status != 200 || packet.Response.Body == nil || packet.Response.TransportFailure != nil {
			return reject()
		}
		limits := audit.Contract.Model
		if packet.Identity.Provider == "jev" {
			limits = audit.Contract.JEV
		}
		endpoint, _, err := windowProviderEndpoint(packet.Identity.Provider)
		if err != nil {
			return reject()
		}
		request, err := historyStoppedFile(p, filepath.Join(dir, packet.Request.File), packet.Request.FullSHA256, windowMaxResponseBytes)
		if err != nil || validateWindowPOST(packet.Identity.Provider, "POST", endpoint, request, limits, packet.Identity) != nil {
			return reject()
		}
		response, err := historyStoppedFile(p, filepath.Join(dir, packet.Response.Body.File), packet.Response.Body.FullSHA256, windowMaxResponseBytes)
		if err != nil || settle.ResponseSHA256 != conversationHash(response) {
			return reject()
		}
		usage, err := parseWindowResponseUsage(packet.Identity.Provider, response, limits)
		if err != nil || !reflect.DeepEqual(usage, settle.Usage) {
			return reject()
		}
	}
	return audit, nil
}
