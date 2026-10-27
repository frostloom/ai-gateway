package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// These private concrete substitutes have no network client, delegated transport,
// endpoint option, credentials or disk-resume path. Captures are synthetic local
// evidence only; fake usage never measures tokens, billing or a paid envelope.
type windowClosedProviderData struct{ DefinitionsDir, ProfilesDir, CampaignRoot, CaptureDir string }
type windowClosedModelTransport struct{ capture *windowClosedCapture }
type windowClosedJEVTransport struct{ capture *windowClosedCapture }
type windowClosedPacket struct {
	Provider       string                `json:"provider"`
	Ordinal        int                   `json:"ordinal"`
	Pass           int                   `json:"pass"`
	Arm            string                `json:"arm"`
	Turn           int                   `json:"turn"`
	Attempt        int                   `json:"attempt"`
	CaseKey        string                `json:"case_key"`
	WindowID       string                `json:"window_id"`
	LedgerSequence int                   `json:"ledger_sequence"`
	Request        windowRequestIdentity `json:"ledger_pending_request"`
	RequestSHA256  string                `json:"request_sha256"`
	ResponseSHA256 string                `json:"response_sha256"`
	RequestBytes   int                   `json:"request_bytes"`
	ResponseBytes  int                   `json:"response_bytes"`
	RequestFile    string                `json:"request_file"`
	ResponseFile   string                `json:"response_file"`
	DescriptorFile string                `json:"descriptor_file"`
	Synthetic      bool                  `json:"synthetic"`
}
type windowClosedCapture struct {
	mu            sync.Mutex
	dir           string
	directoryInfo os.FileInfo
	source        ConversationSuite
	profiles      PairedProfiles
	cases         map[string]ConversationCase
	readArgs      map[string]map[string]any
	planNames     map[string]string
	seen          map[string]bool
	files         map[string]string
	packets       []windowClosedPacket
	ledgers       map[*windowAccounting]bool
	stopped       bool
}

func newWindowClosedProviders(data windowClosedProviderData) (*windowClosedModelTransport, *windowClosedJEVTransport, error) {
	fail := func() (*windowClosedModelTransport, *windowClosedJEVTransport, error) {
		return nil, nil, fmt.Errorf("closed provider construction rejected")
	}
	source, e := LoadConversations(data.DefinitionsDir)
	if e != nil {
		return fail()
	}
	primary, _, e := fullCustomerSelection(source)
	if e != nil {
		return fail()
	}
	profiles, e := LoadPairedProfiles(data.ProfilesDir)
	if e != nil {
		return fail()
	}
	args, e := loadFullLocalReadParameters(data.DefinitionsDir, source.DefinitionSHA256)
	if e != nil {
		return fail()
	}
	canonical := func(path string) (string, error) {
		if path == "" {
			return "", fmt.Errorf("missing path")
		}
		abs, e := filepath.Abs(path)
		if e != nil {
			return "", e
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(abs))
		if e != nil {
			return "", e
		}
		canonical := filepath.Join(parent, filepath.Base(abs))
		if _, err := os.Lstat(canonical); err == nil {
			return filepath.EvalSymlinks(canonical)
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return canonical, nil
	}
	root, e := canonical(data.CampaignRoot)
	if e != nil {
		return fail()
	}
	dir, e := canonical(data.CaptureDir)
	if e != nil {
		return fail()
	}
	within := func(a, b string) bool {
		rel, e := filepath.Rel(a, b)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if within(root, dir) || within(dir, root) {
		return fail()
	}
	if e = os.Mkdir(dir, 0700); e != nil {
		return fail()
	}
	if e = os.Chmod(dir, 0700); e != nil {
		return fail()
	}
	directoryInfo, e := os.Lstat(dir)
	if e != nil {
		return fail()
	}
	parent, e := os.Open(filepath.Dir(dir))
	if e != nil {
		return fail()
	}
	syncErr := parent.Sync()
	closeErr := parent.Close()
	if syncErr != nil || closeErr != nil {
		return fail()
	}
	c := &windowClosedCapture{dir: dir, directoryInfo: directoryInfo, source: source, profiles: profiles, cases: map[string]ConversationCase{}, readArgs: args, planNames: map[string]string{}, seen: map[string]bool{}, files: map[string]string{}, ledgers: map[*windowAccounting]bool{}}
	for _, item := range primary.Cases {
		c.cases[item.Key()] = item
	}
	for ref, plan := range profiles.archive.Catalogue.Plans {
		c.planNames[ref] = plan.Name
	}
	return &windowClosedModelTransport{capture: c}, &windowClosedJEVTransport{capture: c}, nil
}
func (m *windowClosedModelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if m == nil || m.capture == nil {
		return nil, fmt.Errorf("missing closed model")
	}
	return m.capture.roundTrip(r, "model")
}
func (j *windowClosedJEVTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if j == nil || j.capture == nil {
		return nil, fmt.Errorf("missing closed JEV")
	}
	return j.capture.roundTrip(r, "jev")
}
func (c *windowClosedCapture) stopLocked() error {
	c.stopped = true
	for l := range c.ledgers {
		_ = windowTransportStop(l, "protocol_failure")
	}
	return fmt.Errorf("closed provider capture stopped")
}
func (c *windowClosedCapture) roundTrip(r *http.Request, provider string) (*http.Response, error) {
	if r == nil || r.Body == nil {
		return nil, fmt.Errorf("missing closed provider request")
	}
	defer r.Body.Close()
	frame, e := windowOwnedContextFrame(r.Context())
	if e != nil {
		return nil, fmt.Errorf("closed provider requires live owned phase")
	}
	phase := r.Context().Value(windowOwnedPhaseKey{}).(*windowOwnedPhase)
	d := phase.driver
	p := d.permit
	// No driver pointer is retained by the source-aware substitute.
	p.mu.Lock()
	if p.guardWindowOwnedLocked(d) != nil || d.phase != phase {
		p.mu.Unlock()
		return nil, fmt.Errorf("closed provider phase changed")
	}
	ledger := d.ledger
	slot := d.slot
	arm := d.arm
	turn := d.nextTurn + 1
	attempt := phase.model
	if provider == "jev" {
		attempt = phase.jev
	}
	profileSHA := d.environment.profiles.SHA256
	scopeDefinition := p.campaign.prepared.scope.DefinitionSHA256
	actualCampaignRoot := p.campaign.root
	p.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ledgers[ledger] = true
	fail := func() (*http.Response, error) { return nil, c.stopLocked() }
	rel, relErr := filepath.Rel(actualCampaignRoot, c.dir)
	inverse, inverseErr := filepath.Rel(c.dir, actualCampaignRoot)
	if relErr != nil || inverseErr != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) || inverse != ".." && !strings.HasPrefix(inverse, ".."+string(filepath.Separator)) {
		return fail()
	}
	if c.stopped || r.Context().Err() != nil || profileSHA != c.profiles.SHA256 || scopeDefinition != c.source.DefinitionSHA256 || !reflect.DeepEqual(c.cases[frame.Key()], frame) {
		return fail()
	}
	state := ledger.Snapshot()
	if state.Pending == nil || state.Closed || state.Halt != "" {
		return fail()
	}
	identity := state.Pending.Request
	bounds, e := ledger.contract.provider(provider)
	if e != nil {
		return fail()
	}
	if identity.Provider != provider || identity.CaseKey != frame.Key() || identity.CaseKey != slot.CaseKey || identity.Pass != slot.Pass || identity.Arm != arm || identity.Turn != turn || identity.Attempt != attempt || attempt < 1 {
		return fail()
	}
	if r.URL == nil || r.RequestURI != "" || len(r.TransferEncoding) != 0 || (r.Host != "" && r.Host != r.URL.Host) {
		return fail()
	}
	max := bounds.MaxRequestBytes
	if max < 1 || max > 1<<20 {
		return fail()
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	closeErr := r.Body.Close()
	if e != nil || closeErr != nil || len(raw) > max {
		return fail()
	}
	if validateWindowPOST(provider, r.Method, r.URL.String(), raw, bounds, identity) != nil {
		return fail()
	}
	key := ledger.contract.ClaimSHA256 + "/" + strconv.Itoa(state.Pending.Sequence)
	if c.seen[key] {
		return fail()
	}
	c.seen[key] = true
	index := len(c.packets) + 1
	packet := windowClosedPacket{Provider: provider, Ordinal: slot.Ordinal, Pass: identity.Pass, Arm: identity.Arm, Turn: identity.Turn, Attempt: identity.Attempt, CaseKey: identity.CaseKey, WindowID: ledger.contract.WindowID, LedgerSequence: state.Pending.Sequence, Request: identity, RequestSHA256: conversationHash(raw), RequestBytes: len(raw), RequestFile: fmt.Sprintf("packet-%06d-request.json", index), ResponseFile: fmt.Sprintf("packet-%06d-response.json", index), DescriptorFile: fmt.Sprintf("packet-%06d.json", index), Synthetic: true}
	// Original native provider bytes are durable BEFORE any inner model rewrite.
	if e = c.writeFreshLocked(packet.RequestFile, raw); e != nil {
		return fail()
	}
	var body []byte
	if provider == "model" {
		body, e = c.modelResponse(r, raw, frame, bounds)
	} else {
		body, e = c.jevResponse(raw, bounds)
	}
	if e != nil || len(body) > windowMaxResponseBytes {
		return fail()
	}
	if _, e = parseWindowResponseUsage(provider, body, bounds); e != nil {
		return fail()
	}
	packet.ResponseSHA256 = conversationHash(body)
	packet.ResponseBytes = len(body)
	if e = c.writeFreshLocked(packet.ResponseFile, body); e != nil {
		return fail()
	}
	descriptor, e := json.Marshal(packet)
	if e != nil {
		return fail()
	}
	if e = c.writeFreshLocked(packet.DescriptorFile, descriptor); e != nil {
		return fail()
	}
	c.packets = append(c.packets, packet)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
}

// Root handles anchor each operation to the original directory inode and prevent
// a substituted directory or symlink from redirecting capture writes.
func (c *windowClosedCapture) openRootLocked() (*os.Root, error) {
	info, e := os.Lstat(c.dir)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 || c.directoryInfo == nil || !os.SameFile(c.directoryInfo, info) {
		return nil, fmt.Errorf("capture directory changed")
	}
	root, e := os.OpenRoot(c.dir)
	if e != nil {
		return nil, e
	}
	actual, e := root.Stat(".")
	if e != nil || !os.SameFile(c.directoryInfo, actual) {
		root.Close()
		return nil, fmt.Errorf("capture directory identity changed")
	}
	return root, nil
}
func (c *windowClosedCapture) writeFreshLocked(name string, raw []byte) error {
	root, e := c.openRootLocked()
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	if e = f.Chmod(0600); e == nil {
		var n int
		n, e = f.Write(raw)
		if e == nil && n != len(raw) {
			e = io.ErrShortWrite
		}
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	dir, e := root.Open(".")
	if e != nil {
		return e
	}
	e = dir.Sync()
	closeErr = dir.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	c.files[name] = conversationHash(raw)
	return nil
}
func (c *windowClosedCapture) modelResponse(r *http.Request, raw []byte, frame ConversationCase, bounds windowProviderLimits) ([]byte, error) {
	obj, e := windowJSONObject(raw)
	if e != nil {
		return nil, e
	}
	obj["model"] = "local-full-customer"
	// UseNumber in windowJSONObject preserves all unmodified JSON numbers.
	inner, e := json.Marshal(obj)
	if e != nil {
		return nil, e
	}
	cloned := r.Clone(r.Context())
	cloned.Header = make(http.Header)
	cloned.URL, _ = url.Parse("http://127.0.0.1/v1/messages")
	cloned.Host = ""
	cloned.Body = io.NopCloser(bytes.NewReader(inner))
	cloned.GetBody = nil
	cloned.ContentLength = int64(len(inner))
	oracle := &fullLocalModel{c: frame, readArgs: c.readArgs[frame.SourceID], planNames: c.planNames}
	response, e := oracle.RoundTrip(cloned)
	if e != nil || response == nil || response.Body == nil {
		return nil, fmt.Errorf("closed model oracle failed")
	}
	defer response.Body.Close()
	fake, e := readWindowResponse(response.Body)
	if e != nil {
		return nil, e
	}
	parsed, e := windowJSONObject(fake)
	if e != nil {
		return nil, e
	}
	parsed["model"] = "deepseek-flash"
	parsed["usage"] = windowClosedSyntheticUsage(bounds, true)
	return json.Marshal(parsed)
}
func windowClosedSyntheticUsage(bounds windowProviderLimits, model bool) map[string]int64 {
	input := int64(100)
	if input > bounds.MaxInput {
		input = bounds.MaxInput
	}
	output := int64(8)
	if output > bounds.MaxOutput {
		output = bounds.MaxOutput
	}
	usage := map[string]int64{"input_tokens": input, "output_tokens": output}
	if model {
		usage["cache_read_input_tokens"] = 0
		usage["cache_creation_input_tokens"] = 0
	}
	return usage
}
func (c *windowClosedCapture) jevResponse(raw []byte, bounds windowProviderLimits) ([]byte, error) {
	obj, e := windowJSONObject(raw)
	if e != nil {
		return nil, e
	}
	questions, ok := obj["questions"].(map[string]any)
	if !ok || len(questions) == 0 {
		return nil, fmt.Errorf("missing closed JEV questions")
	}
	answers := map[string]any{}
	for name, value := range questions {
		q, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid closed question")
		}
		switch q["type"] {
		case "noul":
			answers[name] = map[string]any{"type": "noul", "noul": .99}
		case "choice":
			criteria, ok := q["criteria"].(map[string]any)
			if !ok || len(criteria) == 0 {
				return nil, fmt.Errorf("missing closed choice criteria")
			}
			keys := make([]string, 0, len(criteria))
			for k := range criteria {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			choice := keys[0]
			confidence := .5
			if _, ok := criteria["get_balance"]; ok {
				choice = "get_balance"
			}
			if _, ok := criteria["complex"]; ok {
				choice = "complex"
				confidence = .99
			}
			probabilities := map[string]float64{}
			for _, k := range keys {
				if len(keys) > 1 {
					probabilities[k] = (1 - confidence) / float64(len(keys)-1)
				} else {
					probabilities[k] = 1
				}
			}
			if len(keys) > 1 {
				probabilities[choice] = confidence
			}
			answers[name] = map[string]any{"type": "choice", "choice": choice, "confidence": confidence, "probabilities": probabilities}
		case "score":
			criteria, ok := q["criteria"].([]any)
			if !ok || len(criteria) == 0 {
				return nil, fmt.Errorf("missing closed score criteria")
			}
			legend := map[string]any{}
			probabilities := map[string]float64{}
			for i, v := range criteria {
				key := strconv.Itoa(i)
				legend[key] = v
				probabilities[key] = 0
			}
			score := 0.0
			probabilities["0"] = 1
			if len(criteria) > 1 {
				score = .1
				probabilities["0"] = .9
				probabilities["1"] = .1
			}
			answers[name] = map[string]any{"type": "score", "score": score, "confidence": .99, "legend": legend, "probabilities": probabilities}
		default:
			return nil, fmt.Errorf("unsupported closed JEV question")
		}
	}
	return json.Marshal(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": windowClosedSyntheticUsage(bounds, false)})
}

// Final verification reads only exact files owned in this live object. Disk
// contents can never reconstruct providers, permissions, ledger or paid claims.
func verifyWindowClosedPackets(model *windowClosedModelTransport, judge *windowClosedJEVTransport) ([]windowClosedPacket, error) {
	if model == nil || judge == nil || model.capture == nil || model.capture != judge.capture {
		return nil, fmt.Errorf("closed provider ownership mismatch")
	}
	c := model.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	fail := func() ([]windowClosedPacket, error) { return nil, c.stopLocked() }
	if c.stopped {
		return fail()
	}
	root, e := c.openRootLocked()
	if e != nil {
		return fail()
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		return fail()
	}
	entries, e := dir.ReadDir(-1)
	closeErr := dir.Close()
	if e != nil || closeErr != nil || len(entries) != len(c.files) {
		return fail()
	}
	for _, entry := range entries {
		expected, ok := c.files[entry.Name()]
		if !ok {
			return fail()
		}
		info, e := root.Lstat(entry.Name())
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > windowMaxResponseBytes {
			return fail()
		}
		file, e := root.Open(entry.Name())
		if e != nil {
			return fail()
		}
		raw, e := io.ReadAll(io.LimitReader(file, windowMaxResponseBytes+1))
		closeErr := file.Close()
		if e != nil || closeErr != nil || len(raw) > windowMaxResponseBytes || conversationHash(raw) != expected {
			return fail()
		}
	}
	out, e := cloneWindowOwned(c.packets)
	if e != nil {
		return fail()
	}
	return out, nil
}
