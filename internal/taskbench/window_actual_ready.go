package taskbench

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// Injected only into an independently reviewed committed-archive build. Empty
// defaults deliberately make ordinary builds incapable of actual readiness.
var fullWindowActualManifestSHA, fullWindowActualArchiveCommit, fullWindowActualArchiveTree string

// Options select a read-only manifest. They cannot replace build trust anchors,
// historical claims, provider transports, observation identity or frozen caps.
type FullWindowActualReadyOptions struct{ ManifestPath string }
type FullWindowActualReadySummary struct {
	ManifestSHA256     string `json:"manifest_sha256"`
	ScopeSHA256        string `json:"scope_sha256"`
	ExecutionSHA256    string `json:"execution_sha256"`
	Pairs              int    `json:"pairs"`
	Arms               int    `json:"arms"`
	DefinedTurns       int    `json:"defined_turns"`
	BoundariesExcluded int    `json:"boundaries_excluded"`
}

// FullWindowActualReady is an in-process read-only attestation, not execution
// authorization. JSON cannot populate its private snapshot or mint a token.
type FullWindowActualReady struct {
	manifestPath   string
	manifestRaw    []byte
	attestationRaw []byte
	manifest       windowActualManifest
	prepared       windowCampaignPrepared
	baseline       fullWindowBaseline
	stopped        windowStoppedBaseline
	stoppedV2      windowStoppedV2Baseline
	stoppedV3      windowStoppedV3Baseline
	stoppedV4      windowStoppedV4Baseline
	stoppedV5      windowStoppedV5Baseline
	stoppedV6      windowStoppedV6Baseline
	approval130    windowActualFile
	stoppedV7      windowStoppedV7Baseline
	stoppedV8      windowStoppedV8Baseline
	v8Authority    *windowV8Authority
	source         ConversationSuite
	profiles       PairedProfiles
}
type windowActualFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type windowActualGoEvidence struct {
	Result       windowActualFile `json:"result"`
	Command      []string         `json:"command"`
	SourceSHA256 string           `json:"source_sha256"`
	Packages     []string         `json:"packages"`
}
type windowActualManifest struct {
	HelperCatalog             *windowActualFile                `json:"helper_catalog,omitempty"`
	PrivateSettingsSHA256     string                           `json:"private_settings_sha256"`
	BuildAttestationPublicKey string                           `json:"build_attestation_public_key"`
	Schema                    string                           `json:"schema"`
	EvidenceKind              string                           `json:"evidence_kind"`
	ArchiveCommit             string                           `json:"archive_commit"`
	ArchiveTree               string                           `json:"archive_tree"`
	SourceRoot                string                           `json:"source_root"`
	Sources                   map[string]string                `json:"sources"`
	DefinitionsDir            string                           `json:"definitions_dir"`
	ProfilesDir               string                           `json:"profiles_dir"`
	ClaimsDir                 string                           `json:"claims_dir"`
	HistoricalClaims          map[string]string                `json:"historical_claims"`
	BaselineEvidence          *windowActualFile                `json:"baseline_evidence,omitempty"`
	SessionRoot               string                           `json:"session_root"`
	CampaignRoot              string                           `json:"campaign_root"`
	KnowledgeModel            string                           `json:"knowledge_model"`
	KnowledgeJEV              string                           `json:"knowledge_jev"`
	Scope                     fullWindowScope                  `json:"scope"`
	ScopeSHA256               string                           `json:"scope_sha256"`
	ExecutionSHA256           string                           `json:"execution_sha256"`
	Limits                    windowCampaignLimits             `json:"provider_limits"`
	Prices                    windowActualFile                 `json:"prices"`
	Envelope                  windowActualFile                 `json:"envelope"`
	FullGo                    windowActualGoEvidence           `json:"full_go"`
	Observer                  observability.EvaluationIdentity `json:"observer"`
}

// Both evidence files bind exact integer bounds used by existing accounting.
// Their outer SHA is reviewed independently; self-declared booleans alone are
// never accepted as tariff or maximum-envelope provenance.
type windowActualProviderEvidence struct {
	Schema        string               `json:"schema"`
	EvidenceKind  string               `json:"evidence_kind"`
	Limits        windowCampaignLimits `json:"provider_limits"`
	ModelEndpoint string               `json:"model_endpoint"`
	JEVEndpoint   string               `json:"jev_endpoint"`
	SourceSHA256  string               `json:"source_sha256"`
	Basis         []windowActualFile   `json:"basis"`
}

func CheckFullWindowActualReady(o FullWindowActualReadyOptions) (FullWindowActualReadySummary, *FullWindowActualReady, error) {
	return checkFullWindowActualReady(o, false)
}
func checkFullWindowActualReady(o FullWindowActualReadyOptions, v8 bool) (FullWindowActualReadySummary, *FullWindowActualReady, error) {
	epoch := 0
	if v8 {
		epoch = 8
	}
	return checkFullWindowActualReadyEpoch(o, epoch)
}
func checkFullWindowActualReadyEpoch(o FullWindowActualReadyOptions, epoch int) (FullWindowActualReadySummary, *FullWindowActualReady, error) {
	v8 := epoch == 8
	v9 := epoch == 9
	manifestSHA, archiveCommit, archiveTree := fullWindowActualManifestSHA, fullWindowActualArchiveCommit, fullWindowActualArchiveTree
	if v8 {
		manifestSHA, archiveCommit, archiveTree = fullWindowV8ManifestSHA, fullWindowV8ArchiveCommit, fullWindowV8ArchiveTree
	}
	if v9 {
		manifestSHA, archiveCommit, archiveTree = fullWindowV9ManifestSHA, fullWindowV9ArchiveCommit, fullWindowV9ArchiveTree
	}
	var summary FullWindowActualReadySummary
	reject := func() (FullWindowActualReadySummary, *FullWindowActualReady, error) {
		return summary, nil, fmt.Errorf("actual readiness rejected")
	}
	if epoch != 0 && epoch != 8 && epoch != 9 {
		return reject()
	}
	if !comparisonDigest(manifestSHA) || !windowActualRevision(archiveCommit) || !windowActualRevision(archiveTree) {
		return reject()
	}
	raw, err := readWindowActualFile(o.ManifestPath, manifestSHA, 8<<20)
	if err != nil {
		return reject()
	}
	var m windowActualManifest
	if decodeWindowActualJSON(raw, &m) != nil || validateWindowActualPrivateSettingsBinding(&m) != nil || !(windowActualSupportedEpoch(m) && epoch == 0 || v8 && m.Schema == "full-window-actual-ready.v8" && m.Scope.Schema == "full-window-scope.v8" || v9 && m.Schema == "full-window-actual-ready.v9" && m.Scope.Schema == "full-window-scope.v9") || m.EvidenceKind != "actual" || m.ArchiveCommit != archiveCommit || m.ArchiveTree != archiveTree {
		return reject()
	}
	attestationRaw, err := readWindowActualBytes(o.ManifestPath+".binary-attestation.json", "", 65536)
	if err != nil {
		return reject()
	}
	executable, err := os.Executable()
	if err != nil {
		return reject()
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil || validateWindowActualBinaryAttestation(attestationRaw, m, manifestSHA, executable) != nil {
		return reject()
	}
	if validateWindowActualRoots(m.SessionRoot, m.CampaignRoot) != nil || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun {
		return reject()
	}
	customerRevision := "50e3c8dab14b4b6cc25322fd340a274e6970200b"
	if err := validateWindowActualHelperCatalog(m, o.ManifestPath); err != nil {
		return reject()
	}
	if v9 {
		customerRevision = m.ArchiveCommit
	}

	if m.Scope.Bindings.CustomerRevision != customerRevision || m.Scope.Bindings.ExecutorRevision != m.ArchiveCommit || m.Scope.Bindings.PolicySHA256 != windowOwnedQualityPolicySHA() {
		return reject()
	}
	packages, err := validateWindowActualSource(m)
	if err != nil {
		return reject()
	}
	sourceRaw, _ := json.Marshal(m.Sources)
	sourceSHA := conversationHash(sourceRaw)
	if !reflect.DeepEqual(m.FullGo.Command, windowActualGoCommand(m.Schema)) || m.FullGo.SourceSHA256 != sourceSHA || !reflect.DeepEqual(m.FullGo.Packages, packages) {
		return reject()
	}
	inventory, err := windowActualPackageInventory(m.SourceRoot, m.Sources)
	if err != nil {
		return reject()
	}
	results, err := readWindowActualFile(m.FullGo.Result.Path, m.FullGo.Result.SHA256, 64<<20)
	if err != nil || validateWindowActualGoPackages(results, inventory) != nil {
		return reject()
	}
	if m.Prices.SHA256 != m.Scope.Bindings.PriceEvidenceSHA256 || m.Envelope.SHA256 != m.Scope.Bindings.RequestEnvelopeSHA256 {
		return reject()
	}
	for i, f := range []windowActualFile{m.Prices, m.Envelope} {
		evidence, err := readWindowActualFile(f.Path, f.SHA256, 8<<20)
		if err != nil {
			return reject()
		}
		var e windowActualProviderEvidence
		schema := []string{"full-window-actual-prices.v1", "full-window-actual-envelope.v1"}[i]
		if decodeWindowActualJSON(evidence, &e) != nil || validateWindowActualProviderEvidence(e, schema, m.Limits, sourceSHA) != nil {
			return reject()
		}
	}
	id := m.Observer
	exportTimeoutMS := windowObserverTimeoutForScope(m.Scope.Schema)
	if !id.Enabled || !id.CaptureContent || id.SampleRate != 1 || id.Environment != "evaluation" || id.Release != m.ArchiveCommit || id.ExportTimeoutMS != exportTimeoutMS || id.QueueSize < 1 || !comparisonDigest(id.EndpointSHA256) || !comparisonDigest(id.ProjectSHA256) {
		return reject()
	}
	// Source/profile paths are part of the reviewed tree, never arbitrary aliases.
	if validateWindowActualInputPaths(m) != nil {
		return reject()
	}
	source, err := LoadConversations(m.DefinitionsDir)
	if err != nil {
		return reject()
	}
	profiles, err := LoadPairedProfiles(m.ProfilesDir)
	if err != nil {
		return reject()
	}
	prepare := prepareWindowCampaign
	if v8 {
		prepare = prepareWindowCampaignV8Technical
	}
	if v9 {
		prepare = prepareWindowCampaignV9Technical
	}
	prepared, err := prepare(m.CampaignRoot, m.CampaignRoot, m.Scope, source, profiles, m.Scope.Bindings, m.ScopeSHA256, m.ExecutionSHA256, m.Limits)
	if err != nil {
		return reject()
	}
	_, totals, err := validateFullWindowScope(m.Scope, source, profiles, m.Scope.Bindings)
	if err != nil || totals.Pairs != 3660 || totals.Arms != 7320 || totals.DefinedUserTurns != 11772 || totals.Boundaries != 1851 {
		return reject()
	}
	candidate := &FullWindowActualReady{manifestPath: o.ManifestPath, manifestRaw: append([]byte(nil), raw...), attestationRaw: append([]byte(nil), attestationRaw...), manifest: m, prepared: prepared, source: source, profiles: profiles}
	if v9 {
		proof, e := loadWindowActualStoppedBaselineV9(m)
		if e != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV8, candidate.prepared.stoppedHistory = windowV9CandidateBaseline(proof.base), proof, proof
	} else if v8 {
		proof, e := loadWindowActualStoppedBaselineV8(m)
		if e != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV7, candidate.prepared.stoppedHistory = windowV8CandidateBaseline(proof.base), proof, proof
	} else if m.Schema == "full-window-actual-ready.v7" {
		proof, approval, err := loadWindowActualStoppedBaselineV7(m)
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV6, candidate.approval130, candidate.prepared.stoppedHistory = proof.base, proof, approval, windowV7CurrentHistory{stopped: proof, approval: approval}
	} else if m.Schema == "full-window-actual-ready.v6" {
		proof, approval, err := loadWindowActualStoppedBaselineV6(m)
		if err != nil {
			return reject()
		}
		history := windowCurrent130History{stopped: proof, approval: approval}
		base, _, _, err := history.revalidateWindowStoppedHistory()
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV5, candidate.approval130, candidate.prepared.stoppedHistory = base, proof, approval, history
	} else if m.Schema == "full-window-actual-ready.v5" {
		proof, err := loadWindowActualStoppedBaselineV5(m)
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV4, candidate.prepared.stoppedHistory = proof.base, proof, proof
	} else if m.Schema == "full-window-actual-ready.v4" {
		proof, err := loadWindowActualStoppedBaselineV4(m)
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV3, candidate.prepared.stoppedHistory = proof.base, proof, proof
	} else if m.Schema == "full-window-actual-ready.v3" {
		proof, err := loadWindowActualStoppedBaselineV3(m)
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stoppedV2, candidate.prepared.stoppedHistory = proof.base, proof, proof
	} else {
		proof, err := loadWindowActualStoppedBaseline(m)
		if err != nil {
			return reject()
		}
		candidate.baseline, candidate.stopped, candidate.prepared.stoppedHistory = proof.base, proof, proof
	}
	if validatePreparedWindowCampaign(candidate.prepared, candidate.baseline) != nil {
		return reject()
	}
	summary = FullWindowActualReadySummary{manifestSHA, m.ScopeSHA256, m.ExecutionSHA256, 3660, 7320, 11772, 1851}
	return summary, candidate, nil
}

// Future session startup must recheck before any credential load or claim write.
func (r *FullWindowActualReady) revalidate() error {
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v9" {
		return r.revalidateV9()
	}
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v8" {
		return r.revalidateV8()
	}
	if r == nil || len(r.manifestRaw) == 0 || r.manifestPath == "" {
		return fmt.Errorf("actual ready token absent")
	}
	_, fresh, err := CheckFullWindowActualReady(FullWindowActualReadyOptions{r.manifestPath})
	if err != nil {
		return err
	}
	if !bytes.Equal(r.manifestRaw, fresh.manifestRaw) || !bytes.Equal(r.attestationRaw, fresh.attestationRaw) || !reflect.DeepEqual(r.manifest, fresh.manifest) || !reflect.DeepEqual(r.prepared, fresh.prepared) || !reflect.DeepEqual(r.baseline, fresh.baseline) || !reflect.DeepEqual(r.stopped, fresh.stopped) || !reflect.DeepEqual(r.stoppedV2, fresh.stoppedV2) || !reflect.DeepEqual(r.stoppedV3, fresh.stoppedV3) || !reflect.DeepEqual(r.stoppedV4, fresh.stoppedV4) || !reflect.DeepEqual(r.stoppedV5, fresh.stoppedV5) || r.approval130 != fresh.approval130 {
		return fmt.Errorf("actual ready snapshot changed")
	}
	return nil
}
func windowActualRevision(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func decodeWindowActualJSON(raw []byte, out any) error {
	if err := validateWindowJSONShape(raw, true); err != nil {
		return err
	}

	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func validateWindowActualRoots(session, campaign string) error {
	parent, err := os.Lstat(filepath.Dir(session))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return fmt.Errorf("private existing parent required")
	}
	if session == campaign || filepath.Dir(session) != filepath.Dir(campaign) {
		return fmt.Errorf("new separate sibling roots required")
	}
	for _, p := range []string{session, campaign} {
		if canonicalRootWindowCampaign(p) != nil {
			return fmt.Errorf("canonical root required")
		}
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			return fmt.Errorf("new absent root required")
		}
	}
	return nil
}
func readWindowActualFile(path, sha string, max int64) ([]byte, error) {
	if !comparisonDigest(sha) {
		return nil, fmt.Errorf("anchored file digest required")
	}
	return readWindowActualBytes(path, sha, max)
}
func readWindowActualBytes(path, sha string, max int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || (sha != "" && !comparisonDigest(sha)) || max < 1 {
		return nil, fmt.Errorf("bounded anchored absolute file required")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return nil, fmt.Errorf("aliased evidence path")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > max {
		return nil, fmt.Errorf("invalid evidence file")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("evidence replaced")
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	after, e := os.Lstat(path)
	final, fe := f.Stat()
	closeErr := f.Close()
	if err != nil || e != nil || fe != nil || closeErr != nil || len(raw) > int(max) || int64(len(raw)) != opened.Size() || final.Size() != opened.Size() || !os.SameFile(opened, after) || (sha != "" && conversationHash(raw) != sha) {
		return nil, fmt.Errorf("evidence bytes changed")
	}
	return raw, nil
}
func validateWindowActualProviderEvidence(e windowActualProviderEvidence, schema string, limits windowCampaignLimits, sourceSHA string) error {
	model, _, _ := windowProviderEndpoint("model")
	jev, _, _ := windowProviderEndpoint("jev")
	if e.Schema != schema || e.EvidenceKind != "actual" || e.Limits != limits || e.ModelEndpoint != model || e.JEVEndpoint != jev || e.SourceSHA256 != sourceSHA || len(e.Basis) == 0 {
		return fmt.Errorf("provider evidence mismatch")
	}
	for _, f := range e.Basis {
		if _, err := readWindowActualFile(f.Path, f.SHA256, 64<<20); err != nil {
			return err
		}
	}
	return nil
}

func validateWindowActualGoJSON(raw []byte, packages []string) error {
	expected := map[string]bool{}
	for _, p := range packages {
		if _, exists := expected[p]; exists {
			return fmt.Errorf("duplicate package")
		}
		expected[p] = true
	}
	return validateWindowActualGoPackages(raw, expected)
}
func validateWindowActualGoPackages(raw []byte, packages map[string]bool) error {
	if len(packages) == 0 {
		return fmt.Errorf("Go packages missing")
	}
	states := map[string]int{}
	noTestOutput := map[string]bool{}
	for p := range packages {
		if p == "" {
			return fmt.Errorf("invalid package")
		}
		states[p] = 1
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if _, err := windowJSONObject(line); err != nil {
			return err
		}
		var e struct{ Action, Package, Test, Output string }
		if json.Unmarshal(line, &e) != nil || states[e.Package] == 0 {
			return fmt.Errorf("foreign Go result")
		}
		if e.Action == "fail" || e.Action == "build-fail" {
			return fmt.Errorf("failed Go result")
		}
		if e.Test != "" {
			if e.Action != "run" && e.Action != "output" && e.Action != "pause" && e.Action != "cont" && e.Action != "pass" && e.Action != "skip" {
				return fmt.Errorf("unknown Go test event")
			}
			if !packages[e.Package] || states[e.Package] != 2 {
				return fmt.Errorf("unexpected Go test event")
			}
			continue
		}
		switch e.Action {
		case "start":
			if states[e.Package] != 1 {
				return fmt.Errorf("duplicate Go package")
			}
			states[e.Package] = 2
		case "output":
			if states[e.Package] != 2 {
				return fmt.Errorf("Go output outside live package")
			}
			if strings.TrimSpace(e.Output) == "?   \t"+e.Package+"\t[no test files]" || strings.TrimSpace(e.Output) == "?\t"+e.Package+"\t[no test files]" {
				noTestOutput[e.Package] = true
			}
		case "pass":
			if states[e.Package] != 2 || !packages[e.Package] {
				return fmt.Errorf("Go pass without applicable tests")
			}
			states[e.Package] = 3
		case "skip":
			if states[e.Package] != 2 || packages[e.Package] || !noTestOutput[e.Package] {
				return fmt.Errorf("invalid no-test package skip")
			}
			states[e.Package] = 3
		default:
			return fmt.Errorf("unsupported Go package result")
		}
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	for _, state := range states {
		if state != 3 {
			return fmt.Errorf("incomplete Go result")
		}
	}
	return nil
}

func validateWindowActualSource(m windowActualManifest) ([]string, error) {
	if !filepath.IsAbs(m.SourceRoot) || filepath.Clean(m.SourceRoot) != m.SourceRoot {
		return nil, fmt.Errorf("canonical source root required")
	}
	root, err := filepath.EvalSymlinks(m.SourceRoot)
	if err != nil || root != m.SourceRoot {
		return nil, fmt.Errorf("aliased source root")
	}
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != m.ArchiveCommit {
		return nil, fmt.Errorf("source revision mismatch")
	}
	tree, err := git("rev-parse", "HEAD^{tree}")
	if err != nil || strings.TrimSpace(string(tree)) != m.ArchiveTree {
		return nil, fmt.Errorf("source tree mismatch")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || validateWindowActualBuildPins(info, m, windowActualManifestAnchor(m)) != nil {
		return nil, fmt.Errorf("actual archive build identity unavailable")
	}
	// Archive pins are the executing build's provenance, not caller assertions.
	_, archiveCommit, archiveTree := windowActualBuildAnchors(m)
	if m.ArchiveCommit != archiveCommit || m.ArchiveTree != archiveTree {
		return nil, fmt.Errorf("archive provenance mismatch")
	}
	entries, err := git("ls-tree", "-r", "-l", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	blobs := map[string]windowActualGitBlob{}
	for _, entry := range strings.Split(string(entries), "\x00") {
		if entry == "" {
			continue
		}
		metadata, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 4 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !windowActualRevision(fields[2]) {
			return nil, fmt.Errorf("tracked regular blob required")
		}
		size, e := strconv.ParseInt(fields[3], 10, 64)
		if e != nil || size < 0 || size > windowActualSourceMaxBytes {
			return nil, fmt.Errorf("tracked source exceeds reviewed 16MiB bound")
		}
		if _, exists := blobs[path]; exists {
			return nil, fmt.Errorf("duplicate source tree path")
		}
		blobs[path] = windowActualGitBlob{fields[2], size}
	}
	if len(blobs) != len(m.Sources) || len(blobs) < 3 {
		return nil, fmt.Errorf("incomplete full source map")
	}
	for p := range blobs {
		if !comparisonDigest(m.Sources[p]) {
			return nil, fmt.Errorf("unbound tracked source")
		}
	}
	if err = validateWindowActualSourceFiles(root, m.Sources, blobs); err != nil {
		return nil, err
	}
	expected := m.Sources

	inventory, err := windowActualPackageInventory(root, expected)
	if err != nil {
		return nil, err
	}
	packages := make([]string, 0, len(inventory))
	for p := range inventory {
		packages = append(packages, p)
	}
	sort.Strings(packages)

	for _, path := range []string{"eval/task-definitions", "eval/runtime-profiles"} {
		diff, e := git("diff", "--name-only", "HEAD", "--", path)
		if e != nil || len(diff) != 0 {
			return nil, fmt.Errorf("frozen input modified")
		}
		extra, e := git("ls-files", "--others", "-z", "--", path)
		if e != nil || len(extra) != 0 {
			return nil, fmt.Errorf("extra frozen input")
		}
	}
	return packages, nil
}

func validateWindowActualInputPaths(m windowActualManifest) error {
	if m.DefinitionsDir != filepath.Join(m.SourceRoot, "eval/task-definitions/2026-10-12-full-dev-v1") || m.ProfilesDir != filepath.Join(m.SourceRoot, "eval/runtime-profiles/2026-10-13-six-profiles-v1") {
		return fmt.Errorf("frozen versioned input paths required")
	}
	return nil
}
func validateWindowActualBuildPins(info *debug.BuildInfo, m windowActualManifest, sha string) error {
	if info == nil || info.Main.Path != "github.com/frostloom/ai-gateway" {
		return fmt.Errorf("wrong build module")
	}
	if info.Main.Replace != nil {
		return fmt.Errorf("main module replacement prohibited")
	}
	for _, dep := range info.Deps {
		if dep == nil || dep.Replace != nil {
			return fmt.Errorf("module replacement prohibited")
		}
	}
	vcsPresent := false
	revision, modified := "", ""
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs.") {
			vcsPresent = true
		}
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		}
		if setting.Key == "vcs.modified" {
			modified = setting.Value
		}
	}
	if vcsPresent && (revision != m.ArchiveCommit || modified != "false") {
		return fmt.Errorf("modified or foreign VCS build")
	}
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	expected := map[string]string{prefix + "fullWindowActualManifestSHA": sha, prefix + "fullWindowActualArchiveCommit": m.ArchiveCommit, prefix + "fullWindowActualArchiveTree": m.ArchiveTree}
	if m.Schema == "full-window-actual-ready.v9" {
		expected = map[string]string{prefix + "fullWindowV9ManifestSHA": sha, prefix + "fullWindowV9ArchiveCommit": m.ArchiveCommit, prefix + "fullWindowV9ArchiveTree": m.ArchiveTree}
	} else if m.Schema == "full-window-actual-ready.v8" {
		expected = map[string]string{prefix + "fullWindowV8ManifestSHA": sha, prefix + "fullWindowV8ArchiveCommit": m.ArchiveCommit, prefix + "fullWindowV8ArchiveTree": m.ArchiveTree}
	}
	found := map[string]bool{}
	for _, setting := range info.Settings {
		if setting.Key != "-ldflags" {
			continue
		}
		fields := strings.Fields(setting.Value)
		for i := 0; i < len(fields); i++ {
			v := ""
			if fields[i] == "-X" {
				i++
				if i == len(fields) {
					return fmt.Errorf("missing -X value")
				}
				v = fields[i]
			} else if strings.HasPrefix(fields[i], "-X=") {
				v = strings.TrimPrefix(fields[i], "-X=")
			}
			if v == "" {
				continue
			}
			key, value, ok := strings.Cut(v, "=")
			want, pinned := expected[key]
			if pinned {
				if !ok || value != want || found[key] {
					return fmt.Errorf("build pin provenance mismatch")
				}
				found[key] = true
			}
		}
	}
	if len(found) != 3 {
		return fmt.Errorf("build pins absent from compiler settings")
	}
	return nil
}

// ImportDir only reads source and applies the executing platform's build
// constraints. It neither resolves modules nor compiles or executes packages.
func windowActualPackageInventory(root string, sources map[string]string) (map[string]bool, error) {
	dirs := map[string]bool{}
	for p := range sources {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		d := filepath.Dir(p)
		excluded := false
		for _, part := range strings.Split(filepath.ToSlash(d), "/") {
			if part == "testdata" || part == "vendor" || strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".") && part != "." {
				excluded = true
			}
		}
		if !excluded {
			dirs[d] = true
		}
	}
	inventory := map[string]bool{}
	for d := range dirs {
		pkg, err := build.Default.ImportDir(filepath.Join(root, d), 0)
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("source package cannot be identified: %w", err)
		}
		for _, files := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles} {
			for _, name := range files {
				p := filepath.ToSlash(filepath.Join(d, name))
				if _, ok := sources[p]; !ok {
					return nil, fmt.Errorf("applicable source outside frozen map")
				}
			}
		}
		name := "github.com/frostloom/ai-gateway"
		if d != "." {
			name += "/" + filepath.ToSlash(d)
		}
		inventory[name] = len(pkg.TestGoFiles)+len(pkg.XTestGoFiles) > 0
	}
	if len(inventory) == 0 {
		return nil, fmt.Errorf("no applicable packages")
	}
	return inventory, nil
}

type windowActualBinaryBinding struct {
	Schema              string `json:"schema"`
	ArchiveCommit       string `json:"archive_commit"`
	ArchiveTree         string `json:"archive_tree"`
	ReadyManifestSHA256 string `json:"ready_manifest_sha256"`
	BinarySHA256        string `json:"binary_sha256"`
}
type windowActualBinaryAttestation struct {
	windowActualBinaryBinding
	Signature string `json:"signature"`
}

// Largest currently tracked file is 13,546,241 bytes. Source-only 16MiB
// accommodates those three >8MiB frozen JSON inputs, without enlarging evidence.
const windowActualSourceMaxBytes int64 = 16 << 20

type windowActualGitBlob struct {
	sha1 string
	size int64
}

func validateWindowActualSourceFiles(root string, sources map[string]string, tree ...map[string]windowActualGitBlob) error {
	for p, sha := range sources {
		if p == "" || filepath.IsAbs(p) || filepath.ToSlash(filepath.Clean(p)) != p || p == ".." || strings.HasPrefix(p, "../") || p == ".git" || strings.HasPrefix(p, ".git/") {
			return fmt.Errorf("invalid frozen source path")
		}
		raw, err := readWindowActualFile(filepath.Join(root, filepath.FromSlash(p)), sha, windowActualSourceMaxBytes)
		if err != nil {
			return err
		}
		if len(tree) != 0 {
			blob, ok := tree[0][p]
			if !ok || int64(len(raw)) != blob.size {
				return fmt.Errorf("source tree length mismatch")
			}
			h := sha1.New()
			fmt.Fprintf(h, "blob %d%c", len(raw), byte(0))
			h.Write(raw)
			if hex.EncodeToString(h.Sum(nil)) != blob.sha1 {
				return fmt.Errorf("source differs from committed blob")
			}
		}
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(root, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlink prohibited")
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if _, ok := sources[filepath.ToSlash(rel)]; !ok {
			return fmt.Errorf("unbound source file")
		}
		return nil
	})
}
func validateWindowActualBinaryAttestation(raw []byte, m windowActualManifest, manifestSHA, executable string) error {
	return validateWindowActualBinaryAttestationWithPass(nil, raw, m, manifestSHA, executable)
}

// Only historical constructors pass a private pool. Current/live attestation
// callers retain their original fresh executable reader through the nil path.
func validateWindowActualBinaryAttestationWithPass(p *historyReadPass, raw []byte, m windowActualManifest, manifestSHA, executable string) error {
	var a windowActualBinaryAttestation
	if decodeWindowActualJSON(raw, &a) != nil || a.Schema != "full-window-actual-binary-attestation.v1" || a.ArchiveCommit != m.ArchiveCommit || a.ArchiveTree != m.ArchiveTree || a.ReadyManifestSHA256 != manifestSHA || !comparisonDigest(a.BinarySHA256) {
		return fmt.Errorf("binary attestation binding rejected")
	}
	publicKey, err := hex.DecodeString(m.BuildAttestationPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || len(m.BuildAttestationPublicKey) != 64 {
		return fmt.Errorf("invalid frozen build public key")
	}
	signature, err := hex.DecodeString(a.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid build signature")
	}
	canonical, err := json.Marshal(a.windowActualBinaryBinding)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, signature) {
		return fmt.Errorf("build signature rejected")
	}
	if p != nil {
		_, err = p.Stream(historyReadExecutableStream, executable, a.BinarySHA256, 1<<30)
		return err
	}
	return verifyWindowActualExecutable(executable, a.BinarySHA256)
}
func verifyWindowActualExecutable(path, sha string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("absolute executable required")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return fmt.Errorf("aliased executable")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > 1<<30 {
		return fmt.Errorf("invalid executable file")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return fmt.Errorf("executable replaced")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (1<<30)+1))
	final, fe := f.Stat()
	after, ae := os.Lstat(path)
	closeErr := f.Close()
	if err != nil || fe != nil || ae != nil || closeErr != nil || n != before.Size() || final.Size() != before.Size() || !os.SameFile(opened, after) || hex.EncodeToString(h.Sum(nil)) != sha {
		return fmt.Errorf("executing binary digest mismatch")
	}
	return nil
}

// Live sessions can verify the immutable signature without the startup-only
// requirement that their newly owned output roots remain absent.
func (r *FullWindowActualReady) verifyBinaryAttestation() error {
	if r == nil || len(r.attestationRaw) == 0 || len(r.manifestRaw) == 0 || conversationHash(r.manifestRaw) != windowActualManifestAnchor(r.manifest) {
		return fmt.Errorf("signed actual ready snapshot absent")
	}
	raw, err := readWindowActualBytes(r.manifestPath+".binary-attestation.json", "", 65536)
	if err != nil || !bytes.Equal(raw, r.attestationRaw) {
		return fmt.Errorf("actual binary attestation changed")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	return validateWindowActualBinaryAttestation(raw, r.manifest, windowActualManifestAnchor(r.manifest), executable)
}

func validateWindowActualPrivateSettingsBinding(m *windowActualManifest) error {
	if m == nil || !comparisonDigest(m.PrivateSettingsSHA256) {
		return fmt.Errorf("private settings digest required")
	}
	return nil
}
