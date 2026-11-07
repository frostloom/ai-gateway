package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixtureWindowPayloadStore(t *testing.T, kind string) (*windowPayloadStore, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "payload")
	s, err := newWindowPayloadStore(dir, filepath.Join(root, "campaign"), windowTestContract(), kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func TestWindowPayloadNewPrivateDirectory(t *testing.T) {
	s, dir := fixtureWindowPayloadStore(t, "recorded_run")
	if s == nil {
		t.Fatal("missing store")
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private anchored directory missing", err)
	}
	if _, _, err := verifyWindowPayloadStore(s); err != nil {
		t.Fatal(err)
	}
}

func payloadRequest(provider string, body []byte) windowRequestIdentity {
	id := windowTestRequest(provider)
	id.Bytes = len(body)
	id.BodySHA256 = conversationHash(body)
	return id
}
func TestWindowPayloadRetainsCompleteInvalidResponseWithoutSemanticClaim(t *testing.T) {
	s, dir := fixtureWindowPayloadStore(t, "recorded_run")
	request := []byte(`{"native":9007199254740993}`)
	id := payloadRequest("model", request)
	if err := s.writeWindowRequest(1, id, request); err != nil {
		t.Fatal(err)
	}
	invalid := []byte("not JSON\x00preserved provider error body")
	if err := s.writeWindowResponse(1, id, 503, invalid, true, "protocol"); err != nil {
		t.Fatal(err)
	}
	m, sha, err := verifyWindowPayloadStore(s)
	if err != nil || !comparisonDigest(sha) || len(m.Packets) != 1 {
		t.Fatal("raw evidence missing", err)
	}
	p := m.Packets[0]
	if m.EvidenceKind != "recorded_run" || m.CompletionProof != "unavailable_from_payload_capture" || p.Identity != id || p.ScopeSHA256 != s.contract.ScopeSHA256 || p.ClaimSHA256 != s.contract.ClaimSHA256 || p.WindowID != s.contract.WindowID {
		t.Fatal("raw capture identity/claim differs")
	}
	if p.Response == nil || !p.Response.Complete || !p.Response.Available || p.Response.Status != 503 || p.Response.Body.FullSHA256 != conversationHash(invalid) || p.Response.Body.PrefixSHA256 != "" {
		t.Fatal("schema failure body discarded or marked semantic failure incomplete")
	}
	saved, err := os.ReadFile(filepath.Join(dir, p.Response.Body.File))
	if err != nil || !bytes.Equal(saved, invalid) {
		t.Fatal("response bytes altered", err)
	}
	saved, _ = os.ReadFile(filepath.Join(dir, p.Request.File))
	if !bytes.Equal(saved, request) {
		t.Fatal("request bytes altered")
	}
	raw, _ := json.Marshal(m)
	if bytes.Contains(raw, []byte(`"synthetic"`)) || bytes.Contains(raw, []byte(`"success"`)) {
		t.Fatal("kind inferred synthetic/quality truth")
	}
	m.Packets[0].Response.Body.FullSHA256 = "mutated returned copy"
	again, againSHA, err := verifyWindowPayloadStore(s)
	if err != nil || againSHA != sha || again.Packets[0].Response.Body.FullSHA256 != conversationHash(invalid) {
		t.Fatal("manifest is mutable or unstable")
	}
	for _, entry := range mustPayloadEntries(t, dir) {
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("raw file not private", err)
		}
	}
}
func mustPayloadEntries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
func TestWindowPayloadPrefixesAndUnavailableAreExplicit(t *testing.T) {
	for _, kind := range []string{"bound", "read", "unavailable", "empty_complete"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := fixtureWindowPayloadStore(t, "local_stub")
			req := []byte("original request")
			id := payloadRequest("jev", req)
			if err := s.writeWindowRequest(1, id, req); err != nil {
				t.Fatal(err)
			}
			var body []byte
			complete := false
			status := 200
			code := "response_read"
			switch kind {
			case "bound":
				body = bytes.Repeat([]byte("x"), windowMaxResponseBytes+1)
				code = "response_bound"
			case "read":
				body = []byte("partial prefix")
			case "unavailable":
				status = 0
				code = "round_trip"
			case "empty_complete":
				complete = true
				code = "http_status"
				status = 500
			}
			if err := s.writeWindowResponse(1, id, status, body, complete, code); err != nil {
				t.Fatal(err)
			}
			m, _, err := verifyWindowPayloadStore(s)
			if err != nil {
				t.Fatal(err)
			}
			response := m.Packets[0].Response
			if response.Complete != complete || response.Code != code || response.Status != status {
				t.Fatal("I/O and protocol status conflated")
			}
			if kind == "unavailable" {
				if response.Body != nil || response.Available || len(mustPayloadEntries(t, dir)) != 4 {
					t.Fatal("invented raw unavailable response")
				}
			} else {
				if response.Body == nil || !response.Available || response.Body.Bytes != len(body) {
					t.Fatal("missing complete or prefix bytes")
				}
				if complete {
					if response.Body.FullSHA256 != conversationHash(body) || response.Body.PrefixSHA256 != "" {
						t.Fatal("empty complete hash wrong")
					}
				} else if response.Body.PrefixSHA256 != conversationHash(body) || response.Body.FullSHA256 != "" {
					t.Fatal("prefix claimed full response digest")
				}
			}
		})
	}
}
func TestWindowPayloadReadFailureCannotClaimComplete(t *testing.T) {
	for _, code := range []string{"response_read", "response_bound"} {
		t.Run(code, func(t *testing.T) {
			s, _ := fixtureWindowPayloadStore(t, "recorded_run")
			req := []byte("request")
			id := payloadRequest("model", req)
			if err := s.writeWindowRequest(1, id, req); err != nil {
				t.Fatal(err)
			}
			if err := s.writeWindowResponse(1, id, 200, []byte("prefix"), true, code); err == nil {
				t.Fatal("read/bound failure falsely claimed full body")
			}
		})
	}
}
func TestWindowPayloadRejectsSequenceIdentityCodesAndBounds(t *testing.T) {
	for _, kind := range []string{"skip", "zero", "duplicate_request", "orphan", "identity", "duplicate_response", "bad_sha", "bad_length", "arbitrary_code", "complete_false", "oversize", "oversize_complete", "no_response_complete", "provider_bound"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := fixtureWindowPayloadStore(t, "recorded_run")
			req := []byte("request")
			id := payloadRequest("model", req)
			var err error
			switch kind {
			case "skip":
				err = s.writeWindowRequest(2, id, req)
			case "zero":
				err = s.writeWindowRequest(0, id, req)
			case "orphan":
				err = s.writeWindowResponse(1, id, 200, []byte("orphan"), true, "complete")
			case "bad_sha":
				id.BodySHA256 = strings.Repeat("e", 64)
				err = s.writeWindowRequest(1, id, req)
			case "bad_length":
				id.Bytes++
				err = s.writeWindowRequest(1, id, req)
			default:
				if e := s.writeWindowRequest(1, id, req); e != nil {
					t.Fatal(e)
				}
				switch kind {
				case "duplicate_request":
					err = s.writeWindowRequest(1, id, req)
				case "identity":
					id.Model = "foreign-model"
					err = s.writeWindowResponse(1, id, 200, []byte("response"), true, "complete")
				case "duplicate_response":
					if e := s.writeWindowResponse(1, id, 200, []byte("response"), true, "complete"); e != nil {
						t.Fatal(e)
					}
					err = s.writeWindowResponse(1, id, 200, []byte("response"), true, "complete")
				case "arbitrary_code":
					err = s.writeWindowResponse(1, id, 500, nil, false, "error leaks secret context")
				case "complete_false":
					err = s.writeWindowResponse(1, id, 200, nil, false, "complete")
				case "oversize":
					err = s.writeWindowResponse(1, id, 200, make([]byte, windowMaxResponseBytes+2), false, "response_bound")
				case "oversize_complete":
					err = s.writeWindowResponse(1, id, 200, make([]byte, windowMaxResponseBytes+1), true, "complete")
				case "no_response_complete":
					err = s.writeWindowResponse(1, id, 0, nil, true, "complete")
				case "provider_bound":
					if e := s.writeWindowResponse(1, id, 200, nil, true, "complete"); e != nil {
						t.Fatal(e)
					}
					s.modelRequests = s.contract.Model.MaxAttempts
					id.Turn++
					err = s.writeWindowRequest(2, id, req)
				}
			}
			if err == nil {
				t.Fatal("invalid request/response accepted")
			}
			if e := s.writeWindowRequest(1, payloadRequest("model", req), req); e == nil {
				t.Fatal("poisoned store resumed")
			}
			if _, _, e := verifyWindowPayloadStore(s); e == nil {
				t.Fatal("poisoned store verified")
			}
		})
	}
}
func TestWindowPayloadTamperDirectoryAndInodeChangesStop(t *testing.T) {
	for _, kind := range []string{"body", "inode", "mode", "metadata", "extra", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := fixtureWindowPayloadStore(t, "recorded_run")
			req := []byte("original request")
			id := payloadRequest("model", req)
			if err := s.writeWindowRequest(1, id, req); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "request-000001.body")
			switch kind {
			case "body":
				os.WriteFile(file, []byte("tampered request"), 0600)
			case "inode":
				// Remove-then-create can reuse the same inode. A new name while the
				// old inode is still allocated, then rename, is a real identity change.
				replacement := file + ".replacement"
				if err := os.WriteFile(replacement, req, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, file); err != nil {
					t.Fatal(err)
				}
			case "mode":
				os.Chmod(file, 0644)
			case "metadata":
				os.WriteFile(filepath.Join(dir, "request-000001.json"), []byte("mutated metadata"), 0600)
			case "extra":
				os.WriteFile(filepath.Join(dir, "foreign"), []byte("extra"), 0600)
			case "directory":
				os.Rename(dir, dir+"-old")
				os.Mkdir(dir, 0700)
			case "symlink":
				outside := filepath.Join(filepath.Dir(dir), "outside")
				os.WriteFile(outside, req, 0600)
				os.Remove(file)
				os.Symlink(outside, file)
			}
			if _, _, err := verifyWindowPayloadStore(s); err == nil {
				t.Fatal("tampered evidence verified")
			}
			if err := s.writeWindowResponse(1, id, 200, []byte("later response"), true, "complete"); err == nil {
				t.Fatal("tampered store resumed")
			}
		})
	}
}
func TestWindowPayloadDuplicateFileDoesNotOverwrite(t *testing.T) {
	s, dir := fixtureWindowPayloadStore(t, "recorded_run")
	req := []byte("request")
	id := payloadRequest("model", req)
	if err := s.writeWindowRequest(1, id, req); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "response-000001.body")
	if err := os.WriteFile(file, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.writeWindowResponse(1, id, 200, []byte("new response"), true, "complete"); err == nil {
		t.Fatal("overwrote existing file")
	}
	saved, _ := os.ReadFile(file)
	if string(saved) != "preserve" {
		t.Fatal("existing data overwritten")
	}
}
func TestWindowPayloadClosePreservesUnknownResponse(t *testing.T) {
	s, dir := fixtureWindowPayloadStore(t, "recorded_run")
	req := []byte("pending request")
	id := payloadRequest("model", req)
	if err := s.writeWindowRequest(1, id, req); err != nil {
		t.Fatal(err)
	}
	before, sha, err := verifyWindowPayloadStore(s)
	if err != nil || before.Packets[0].Response != nil {
		t.Fatal("unknown response fabricated")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	after, afterSHA, err := verifyWindowPayloadStore(s)
	if err != nil || afterSHA != sha || after.Packets[0].Response != nil || len(mustPayloadEntries(t, dir)) != 3 {
		t.Fatal("close erased/settled unknown response", err)
	}
	if err = s.writeWindowResponse(1, id, 200, []byte("late"), true, "complete"); err == nil {
		t.Fatal("closed store wrote response")
	}
}
func TestWindowPayloadConstructorDisjointContractKindsAndNewOnly(t *testing.T) {
	for _, kind := range []string{"contract", "kind", "inside", "ancestor", "same", "exists", "campaign_symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "payload")
			root := filepath.Join(base, "campaign")
			contract := windowTestContract()
			evidence := "recorded_run"
			switch kind {
			case "contract":
				contract.ClaimSHA256 = "bad"
			case "kind":
				evidence = "real_vendor_success"
			case "inside":
				os.Mkdir(root, 0700)
				dir = filepath.Join(root, "payload")
			case "ancestor":
				root = filepath.Join(dir, "campaign")
			case "same":
				dir = root
			case "exists":
				os.Mkdir(dir, 0700)
			case "campaign_symlink":
				real := filepath.Join(base, "real")
				os.Mkdir(real, 0700)
				os.Symlink(real, root)
				dir = filepath.Join(real, "payload")
			}
			if s, err := newWindowPayloadStore(dir, root, contract, evidence); err == nil {
				s.Close()
				t.Fatal("invalid constructor accepted")
			}
		})
	}
}

type faultWindowPayloadFile struct {
	*os.File
	short, syncFailure, closeFailure bool
}

func (f *faultWindowPayloadFile) Write(p []byte) (int, error) {
	if f.short && len(p) > 0 {
		return f.File.Write(p[:len(p)-1])
	}
	return f.File.Write(p)
}
func (f *faultWindowPayloadFile) Sync() error {
	if f.syncFailure {
		return fmt.Errorf("injected sync failure")
	}
	return f.File.Sync()
}
func (f *faultWindowPayloadFile) Close() error {
	err := f.File.Close()
	if f.closeFailure {
		return fmt.Errorf("injected close failure")
	}
	return err
}
func TestWindowPayloadDurabilityFailureRetainsBytesAndStops(t *testing.T) {
	for _, kind := range []string{"short", "sync", "close"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := fixtureWindowPayloadStore(t, "recorded_run")
			s.fileWrap = func(f *os.File) windowPayloadFile {
				return &faultWindowPayloadFile{File: f, short: kind == "short", syncFailure: kind == "sync", closeFailure: kind == "close"}
			}
			req := []byte("original uncertain request")
			id := payloadRequest("model", req)
			if err := s.writeWindowRequest(1, id, req); err == nil {
				t.Fatal("durability failure hidden")
			}
			if len(mustPayloadEntries(t, dir)) != 2 {
				t.Fatal("uncertain raw bytes deleted")
			}
			if _, _, err := verifyWindowPayloadStore(s); err == nil {
				t.Fatal("uncertain durable write verified")
			}
			if err := s.writeWindowResponse(1, id, 0, nil, false, "round_trip"); err == nil {
				t.Fatal("store resumed after durability failure")
			}
		})
	}
}

func TestWindowPayloadConcurrentReadonlySnapshots(t *testing.T) {
	s, _ := fixtureWindowPayloadStore(t, "recorded_run")
	var group sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 12; i++ {
				manifest, sha, err := verifyWindowPayloadStore(s)
				if err != nil || !comparisonDigest(sha) || manifest.CompletionProof != "unavailable_from_payload_capture" {
					t.Error("concurrent readonly snapshot", err)
					return
				}
			}
		}()
	}
	for sequence := 1; sequence <= 10; sequence++ {
		raw := []byte(fmt.Sprintf("native request %d", sequence))
		id := payloadRequest("model", raw)
		id.Attempt = sequence
		if err := s.writeWindowRequest(sequence, id, raw); err != nil {
			t.Fatal(err)
		}
		if err := s.writeWindowResponse(sequence, id, 200, []byte("invalid JSON still complete"), true, "protocol"); err != nil {
			t.Fatal(err)
		}
	}
	group.Wait()
	manifest, _, err := verifyWindowPayloadStore(s)
	if err != nil || len(manifest.Packets) != 10 {
		t.Fatal("concurrent snapshot changed store", err)
	}
}
