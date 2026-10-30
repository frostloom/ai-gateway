package taskbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// Read only. The caller must already anchor expected to the original pinned
// payload manifest. This proves retained bytes, never provider processing,
// durable Close, task correctness, billing, or execution authorization.
func readWindowStoppedV2Payload(dir string, expected windowPayloadManifest) error {
	return readWindowStoppedV2PayloadWithPass(nil, dir, expected)
}

func readWindowStoppedV2PayloadWithPass(pass *historyReadPass, dir string, expected windowPayloadManifest) error {
	reject := func() error { return fmt.Errorf("stopped v2 original payload rejected") }
	if expected.Schema != "full-window-payload.v1" || expected.EvidenceKind != "recorded_run" || expected.CompletionProof != "unavailable_from_payload_capture" || expected.Contract.validate() != nil || len(expected.Packets) < 1 || len(expected.Packets) > 1000 {
		return reject()
	}
	raw, err := historyStoppedBytes(pass, filepath.Join(dir, "store.json"), "", 65536)
	var store windowPayloadManifest
	if err != nil || decodeWindowActualJSON(raw, &store) != nil || store.Schema != expected.Schema || store.EvidenceKind != expected.EvidenceKind || store.CompletionProof != expected.CompletionProof || !reflect.DeepEqual(store.Contract, expected.Contract) || len(store.Packets) != 0 {
		return reject()
	}
	names := map[string]bool{"store.json": false}
	seen := map[string]bool{}
	c := expected.Contract
	for index, p := range expected.Packets {
		seq := index + 1
		if p.Sequence != seq || p.ScopeSHA256 != c.ScopeSHA256 || p.ClaimSHA256 != c.ClaimSHA256 || p.WindowID != c.WindowID || c.validateRequest(p.Identity) != nil || seen[windowRequestSlot(p.Identity)] {
			return reject()
		}
		seen[windowRequestSlot(p.Identity)] = true
		requestFile := fmt.Sprintf("request-%06d.json", seq)
		if p.RequestDescriptorFile != requestFile || p.Request.File != fmt.Sprintf("request-%06d.body", seq) || !p.Request.Complete || p.Request.PrefixSHA256 != "" || p.Request.FullSHA256 != p.Identity.BodySHA256 || p.Request.Bytes != p.Identity.Bytes {
			return reject()
		}
		requestRaw, err := historyStoppedBatch(pass, []windowStoppedReadSpec{
			{path: filepath.Join(dir, p.Request.File), sha256: p.Request.FullSHA256, maxBytes: windowMaxResponseBytes, kind: windowStoppedReadFile},
			{path: filepath.Join(dir, requestFile), maxBytes: 65536, kind: windowStoppedReadBytes},
		})
		if err != nil || len(requestRaw[0]) != p.Identity.Bytes {
			return reject()
		}
		var request windowPayloadPacket
		wantRequest := p
		wantRequest.Response = nil
		if decodeWindowActualJSON(requestRaw[1], &request) != nil || !reflect.DeepEqual(request, wantRequest) {
			return reject()
		}
		names[requestFile], names[p.Request.File] = false, false
		responseFile := fmt.Sprintf("response-%06d.json", seq)
		if p.Response == nil || p.Response.DescriptorFile != responseFile || p.Response.TransportFailure != nil {
			return reject()
		}
		r := p.Response
		responseSpecs := []windowStoppedReadSpec{{path: filepath.Join(dir, responseFile), maxBytes: 65536, kind: windowStoppedReadBytes}}
		if r.Available {
			if !r.Complete || r.Status != 200 || r.Code != "complete" || r.Body == nil || !r.Body.Complete || r.Body.PrefixSHA256 != "" || r.Body.File != fmt.Sprintf("response-%06d.body", seq) {
				return reject()
			}
			responseSpecs = append(responseSpecs, windowStoppedReadSpec{path: filepath.Join(dir, r.Body.File), sha256: r.Body.FullSHA256, maxBytes: windowMaxResponseBytes, kind: windowStoppedReadFile})
		}
		responseRaw, err := historyStoppedBatch(pass, responseSpecs)
		var response windowPayloadPacket
		wantResponse := p
		wantResponse.Request = windowPayloadBody{}
		wantResponse.RequestDescriptorFile = ""
		if err != nil || decodeWindowActualJSON(responseRaw[0], &response) != nil || !reflect.DeepEqual(response, wantResponse) {
			return reject()
		}
		names[responseFile] = false
		if !r.Available {
			if r.Complete || r.Status != 0 || r.Code != "round_trip" || r.Body != nil {
				return reject()
			}
			continue
		}
		if len(responseRaw[1]) != r.Body.Bytes {
			return reject()
		}
		names[r.Body.File] = false
	}
	return historyStoppedDir(pass, dir, names)
}

func validateWindowStoppedV2Dir(dir string, names map[string]bool) error {
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != dir || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || len(names) > 4096 {
		return fmt.Errorf("stopped v2 inventory root rejected")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(names) {
		return fmt.Errorf("stopped v2 inventory changed")
	}
	for _, entry := range entries {
		isDir, ok := names[entry.Name()]
		if !ok || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() != isDir || !isDir && !entry.Type().IsRegular() {
			return fmt.Errorf("stopped v2 inventory changed")
		}
	}
	return nil
}
