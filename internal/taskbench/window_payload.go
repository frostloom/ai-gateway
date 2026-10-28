package taskbench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// This store records raw bytes, never execution authority, supplier receipt,
// semantic validity, token usage or a bill. No HTTP/client/key discovery exists.
type windowPayloadBody struct {
	File         string `json:"file"`
	Bytes        int    `json:"bytes"`
	Complete     bool   `json:"complete"`
	FullSHA256   string `json:"full_sha256,omitempty"`
	PrefixSHA256 string `json:"prefix_sha256,omitempty"`
}
type windowPayloadResponse struct {
	Status           int                     `json:"status"`
	Code             string                  `json:"code"`
	Complete         bool                    `json:"complete"`
	Available        bool                    `json:"available"`
	Body             *windowPayloadBody      `json:"body,omitempty"`
	DescriptorFile   string                  `json:"descriptor_file"`
	TransportFailure *windowTransportFailure `json:"transport_failure,omitempty"`
}
type windowPayloadPacket struct {
	Sequence              int                    `json:"sequence"`
	Identity              windowRequestIdentity  `json:"identity"`
	ScopeSHA256           string                 `json:"scope_sha256"`
	ClaimSHA256           string                 `json:"claim_sha256"`
	WindowID              string                 `json:"window_id"`
	Request               windowPayloadBody      `json:"request"`
	RequestDescriptorFile string                 `json:"request_descriptor_file"`
	Response              *windowPayloadResponse `json:"response,omitempty"`
}
type windowPayloadManifest struct {
	Schema          string                   `json:"schema"`
	EvidenceKind    string                   `json:"evidence_kind"`
	CompletionProof string                   `json:"completion_proof"`
	Contract        windowAccountingContract `json:"contract"`
	Packets         []windowPayloadPacket    `json:"packets"`
}
type windowPayloadFile interface {
	io.Writer
	Sync() error
	Close() error
}
type windowPayloadExpected struct {
	sha   string
	bytes int
	info  os.FileInfo
}
type windowPayloadStore struct {
	mu                         sync.Mutex
	dir                        string
	root                       *os.Root
	directoryInfo              os.FileInfo
	contract                   windowAccountingContract
	kind                       string
	packets                    []windowPayloadPacket
	files                      map[string]windowPayloadExpected
	seen                       map[string]bool
	modelRequests, jevRequests int
	poisoned, closed           bool
	// Private durability-fault hook; the public factory never accepts an IO hook.
	fileWrap func(*os.File) windowPayloadFile
}

func windowPayloadCanonical(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute payload path required")
	}
	path = filepath.Clean(path)
	parent, e := filepath.EvalSymlinks(filepath.Dir(path))
	if e != nil {
		return "", e
	}
	path = filepath.Join(parent, filepath.Base(path))
	if _, e = os.Lstat(path); e == nil {
		return filepath.EvalSymlinks(path)
	} else if !os.IsNotExist(e) {
		return "", e
	}
	return path, nil
}
func windowPayloadDisjoint(a, b string) bool {
	within := func(a, b string) bool {
		rel, e := filepath.Rel(a, b)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return !within(a, b) && !within(b, a)
}
func newWindowPayloadStore(dir, campaignRoot string, contract windowAccountingContract, kind string) (*windowPayloadStore, error) {
	reject := func() (*windowPayloadStore, error) { return nil, fmt.Errorf("new private payload store rejected") }
	if contract.validate() != nil || (kind != "recorded_run" && kind != "local_stub") {
		return reject()
	}
	canonicalDir, e := windowPayloadCanonical(dir)
	if e != nil {
		return reject()
	}
	canonicalCampaign, e := windowPayloadCanonical(campaignRoot)
	if e != nil || !windowPayloadDisjoint(canonicalDir, canonicalCampaign) {
		return reject()
	}
	if e = os.Mkdir(canonicalDir, 0700); e != nil {
		return reject()
	}
	if e = os.Chmod(canonicalDir, 0700); e != nil {
		return reject()
	}
	root, e := os.OpenRoot(canonicalDir)
	if e != nil {
		return reject()
	}
	info, e := root.Stat(".")
	if e != nil {
		root.Close()
		return reject()
	}
	s := &windowPayloadStore{dir: canonicalDir, root: root, directoryInfo: info, contract: contract, kind: kind, files: map[string]windowPayloadExpected{}, seen: map[string]bool{}}
	declaration := windowPayloadManifest{Schema: "full-window-payload.v1", EvidenceKind: kind, CompletionProof: "unavailable_from_payload_capture", Contract: contract, Packets: []windowPayloadPacket{}}
	raw, e := json.Marshal(declaration)
	if e == nil {
		e = s.writeFreshLocked("store.json", raw)
	}
	if e == nil {
		e = windowSyncDirectory(filepath.Dir(canonicalDir))
	}
	if e != nil {
		s.poisoned = true
		root.Close()
		return reject()
	}
	return s, nil
}
func (s *windowPayloadStore) poisonLocked() error {
	s.poisoned = true
	return fmt.Errorf("payload store stopped")
}
func (s *windowPayloadStore) rootValidLocked(root *os.Root) error {
	if root == nil || s.directoryInfo == nil {
		return fmt.Errorf("missing payload root")
	}
	info, e := os.Lstat(s.dir)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !os.SameFile(info, s.directoryInfo) {
		return fmt.Errorf("payload directory changed")
	}
	actual, e := root.Stat(".")
	if e != nil || !os.SameFile(actual, s.directoryInfo) {
		return fmt.Errorf("payload root changed")
	}
	return nil
}
func (s *windowPayloadStore) writeFreshLocked(name string, raw []byte) error {
	if s.poisoned || s.closed || s.rootValidLocked(s.root) != nil {
		return s.poisonLocked()
	}
	f, e := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return s.poisonLocked()
	}
	var file windowPayloadFile = f
	if s.fileWrap != nil {
		file = s.fileWrap(f)
	}
	if file == nil {
		f.Close()
		return s.poisonLocked()
	}
	if e = f.Chmod(0600); e == nil {
		var n int
		n, e = file.Write(raw)
		if e == nil && n != len(raw) {
			e = io.ErrShortWrite
		}
	}
	if e == nil {
		e = file.Sync()
	}
	info, statErr := f.Stat()
	closeErr := file.Close()
	if e != nil || statErr != nil || closeErr != nil {
		return s.poisonLocked()
	}
	dir, e := s.root.Open(".")
	if e != nil {
		return s.poisonLocked()
	}
	e = dir.Sync()
	closeErr = dir.Close()
	if e != nil || closeErr != nil {
		return s.poisonLocked()
	}
	s.files[name] = windowPayloadExpected{sha: conversationHash(raw), bytes: len(raw), info: info}
	return nil
}
func (s *windowPayloadStore) writeWindowRequest(sequence int, identity windowRequestIdentity, body []byte) error {
	if s == nil {
		return fmt.Errorf("missing payload store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poisoned || s.closed {
		return s.poisonLocked()
	}
	if sequence != len(s.packets)+1 || sequence > s.contract.Model.MaxAttempts+s.contract.JEV.MaxAttempts || s.contract.validateRequest(identity) != nil || identity.Bytes != len(body) || identity.BodySHA256 != conversationHash(body) {
		return s.poisonLocked()
	}
	if len(s.packets) > 0 && s.packets[len(s.packets)-1].Response == nil {
		return s.poisonLocked()
	}
	slot := windowRequestSlot(identity)
	if s.seen[slot] {
		return s.poisonLocked()
	}
	if identity.Provider == "model" && s.modelRequests >= s.contract.Model.MaxAttempts || identity.Provider == "jev" && s.jevRequests >= s.contract.JEV.MaxAttempts {
		return s.poisonLocked()
	}
	packet := windowPayloadPacket{Sequence: sequence, Identity: identity, ScopeSHA256: s.contract.ScopeSHA256, ClaimSHA256: s.contract.ClaimSHA256, WindowID: s.contract.WindowID, Request: windowPayloadBody{File: fmt.Sprintf("request-%06d.body", sequence), Bytes: len(body), Complete: true, FullSHA256: conversationHash(body)}, RequestDescriptorFile: fmt.Sprintf("request-%06d.json", sequence)}
	if s.writeFreshLocked(packet.Request.File, body) != nil {
		return s.poisonLocked()
	}
	raw, e := json.Marshal(packet)
	if e != nil || s.writeFreshLocked(packet.RequestDescriptorFile, raw) != nil {
		return s.poisonLocked()
	}
	s.packets = append(s.packets, packet)
	s.seen[slot] = true
	if identity.Provider == "model" {
		s.modelRequests++
	} else {
		s.jevRequests++
	}
	return nil
}
func (s *windowPayloadStore) writeWindowResponse(sequence int, identity windowRequestIdentity, status int, body []byte, complete bool, code string) error {
	return s.writeWindowResponseWithFailure(sequence, identity, status, body, complete, code, nil)
}
func (s *windowPayloadStore) writeWindowResponseWithFailure(sequence int, identity windowRequestIdentity, status int, body []byte, complete bool, code string, failure *windowTransportFailure) error {
	if s == nil {
		return fmt.Errorf("missing payload store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poisoned || s.closed {
		return s.poisonLocked()
	}
	if sequence < 1 || sequence != len(s.packets) || s.packets[sequence-1].Identity != identity || s.packets[sequence-1].Response != nil {
		return s.poisonLocked()
	}
	if !hasString([]string{"complete", "round_trip", "response_read", "response_bound", "response_close", "http_status", "protocol"}, code) || (status != 0 && (status < 100 || status > 599)) || len(body) > windowMaxResponseBytes+1 || complete && len(body) > windowMaxResponseBytes || code == "complete" && !complete || complete && (code == "response_read" || code == "response_bound") || status == 0 && (complete || len(body) > 0) {
		return s.poisonLocked()
	}
	if failure != nil && (!failure.valid() || !hasString([]string{"round_trip", "response_read", "response_close"}, code) || complete && code != "response_close") {
		return s.poisonLocked()
	}
	response := windowPayloadResponse{Status: status, Code: code, Complete: complete, Available: body != nil || complete, DescriptorFile: fmt.Sprintf("response-%06d.json", sequence)}
	if failure != nil {
		copy := *failure
		response.TransportFailure = &copy
	}
	if response.Available {
		descriptor := &windowPayloadBody{File: fmt.Sprintf("response-%06d.body", sequence), Bytes: len(body), Complete: complete}
		if complete {
			descriptor.FullSHA256 = conversationHash(body)
		} else {
			descriptor.PrefixSHA256 = conversationHash(body)
		}
		if s.writeFreshLocked(descriptor.File, body) != nil {
			return s.poisonLocked()
		}
		response.Body = descriptor
	}
	record := struct {
		Sequence    int                   `json:"sequence"`
		Identity    windowRequestIdentity `json:"identity"`
		ScopeSHA256 string                `json:"scope_sha256"`
		ClaimSHA256 string                `json:"claim_sha256"`
		WindowID    string                `json:"window_id"`
		Response    windowPayloadResponse `json:"response"`
	}{sequence, identity, s.contract.ScopeSHA256, s.contract.ClaimSHA256, s.contract.WindowID, response}
	raw, e := json.Marshal(record)
	if e != nil || s.writeFreshLocked(response.DescriptorFile, raw) != nil {
		return s.poisonLocked()
	}
	s.packets[sequence-1].Response = &response
	return nil
}

// Readonly verification returns a logical raw-byte manifest. It can never prove
// upstream execution, durable vendor receipt or successful semantic validation.
func verifyWindowPayloadStore(s *windowPayloadStore) (windowPayloadManifest, string, error) {
	if s == nil {
		return windowPayloadManifest{}, "", fmt.Errorf("missing payload store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func() (windowPayloadManifest, string, error) { return windowPayloadManifest{}, "", s.poisonLocked() }
	if s.poisoned {
		return fail()
	}
	root := s.root
	if s.closed {
		var e error
		root, e = os.OpenRoot(s.dir)
		if e != nil {
			return fail()
		}
		defer root.Close()
	}
	if s.rootValidLocked(root) != nil {
		return fail()
	}
	dir, e := root.Open(".")
	if e != nil {
		return fail()
	}
	entries, e := dir.ReadDir(-1)
	closeErr := dir.Close()
	if e != nil || closeErr != nil || len(entries) != len(s.files) {
		return fail()
	}
	for _, entry := range entries {
		expected, ok := s.files[entry.Name()]
		if !ok {
			return fail()
		}
		info, e := root.Lstat(entry.Name())
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != int64(expected.bytes) || !os.SameFile(info, expected.info) {
			return fail()
		}
		f, e := root.Open(entry.Name())
		if e != nil {
			return fail()
		}
		actual, e := f.Stat()
		if e != nil || !os.SameFile(actual, expected.info) {
			f.Close()
			return fail()
		}
		raw, e := io.ReadAll(io.LimitReader(f, int64(expected.bytes)+1))
		closeErr := f.Close()
		if e != nil || closeErr != nil || len(raw) != expected.bytes || conversationHash(raw) != expected.sha {
			return fail()
		}
	}
	manifest := windowPayloadManifest{Schema: "full-window-payload.v1", EvidenceKind: s.kind, CompletionProof: "unavailable_from_payload_capture", Contract: s.contract, Packets: s.packets}
	if manifest.Packets == nil {
		manifest.Packets = []windowPayloadPacket{}
	}
	raw, e := json.Marshal(manifest)
	if e != nil {
		return fail()
	}
	var detached windowPayloadManifest
	if json.Unmarshal(raw, &detached) != nil {
		return fail()
	}
	return detached, conversationHash(raw), nil
}

// Close only closes the local root handle; it is not an execution checkpoint.
// Unknown responses and partial evidence remain on disk, with no deletion path.
func (s *windowPayloadStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.root == nil {
		return nil
	}
	return s.root.Close()
}
